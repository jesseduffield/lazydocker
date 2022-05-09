// Package throttle provides functionality to limit the frequency with which code is called
//
// Throttling is of the Trigger() method and depends on the parameters passed (period, trailing).
//
// The period parameter defines how often the throttled code can run.  A period of one second means
// that the throttled code will run at most once per second.
//
// The trailing parameter defines what hapens if Trigger() is called after the throttled code has been
// started, but before the period is finished.  If trailing is false then these triggers are ignored.
// If trailing is true then the throttled code is executed one more time at the beginning of the next period.
//
// Example with period = time.Second and trailing = false:
//
//		Whole seconds after first trigger...|0|0|0|0|1|1|1|1|
//		Trigger() gets called...............|X| |X| | |X| | |
//		Throttled code gets called..........|X| | | | |X| | |
//
// Note that the second trigger had no effect.  The third Trigger() caused immediate execution of the
// throttled code.
//
// Example with period = time.Second and trailing = true:
//
//		Whole seconds after first trigger...|0|0|0|0|1|1|1|1|
//		Trigger() gets called...............|X| |X| | |X| | |
//		Throttled code gets called..........|X| | | |X| | | |
//
// Note that the second Trigger() causes the throttled code to get called once the first period is over.
// The third Trigger() will do the same.
package throttle

import (
	"sync"
	"time"
)

// ThrottleDriver is an interface for requesting execution of the throttled resource
// and for stopping the throttler.
type ThrottleDriver interface {
	// Trigger() requests execution of the throttled resource.
	Trigger()

	// Stop() stops the throttler.
	Stop()
}

// Throttle extends ThrottleDriver with Next(), which is used by the client to throttle its code.
type Throttle interface {
	ThrottleDriver

	// Next() returns true at most once per `period`.  If false is returned the throttler has been stoped.
	Next() bool
}

// NewThrottle returns a new Throttle.  If trailing is true then a multiple Trigger() calls in one
// period will cause a delayed Trigger() to be called in the next period.
func NewThrottle(period time.Duration, trailing bool) Throttle {
	return newThrottler(period, trailing)
}

// ThottleFunc executes f at most once every period.  Stop() must eventually be called
// on the return value to prevent a leaked go proc.
func ThrottleFunc(period time.Duration, trailing bool, f func()) ThrottleDriver {
	throttler := newThrottler(period, trailing)
	go func() {
		for throttler.Next() {
			f()
		}
	}()
	return throttler
}

type throttler struct {
	cond     *sync.Cond
	period   time.Duration
	trailing bool
	last     time.Time
	waiting  bool
	stop     bool
}

func newThrottler(period time.Duration, trailing bool) *throttler {
	return &throttler{
		period:   period,
		trailing: trailing,
		cond:     sync.NewCond(&sync.Mutex{}),
	}
}

// Trigger signals an attempt to execute the throttled code.
// If Trigger is called twice within the same period, Next() will be called once for that period
// (and once for the next period if trailing is true).
func (t *throttler) Trigger() {
	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	if !t.waiting && !t.stop {

		delta := time.Now().Sub(t.last)

		if delta > t.period {
			t.waiting = true
			t.cond.Broadcast()
		} else if t.trailing {
			t.waiting = true
			time.AfterFunc(t.period-delta, t.cond.Broadcast)
		}
	}
}

// Next() returns true at most once per period.  While it returns true, the throttle is running.
// When it returns false the throttle has been stopped.
func (t *throttler) Next() bool {
	t.cond.L.Lock()
	defer t.cond.L.Unlock()
	for !t.waiting && !t.stop {
		t.cond.Wait()
	}
	if !t.stop {
		t.waiting = false
		t.last = time.Now()
	}
	return !t.stop
}

// Stop the throttle
func (t *throttler) Stop() {
	t.cond.L.Lock()
	defer t.cond.L.Unlock()
	t.stop = true
	t.cond.Broadcast()
}
