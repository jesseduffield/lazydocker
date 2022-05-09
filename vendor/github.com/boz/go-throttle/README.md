# go-throttle [![GoDoc](https://godoc.org/github.com/boz/go-throttle?status.svg)](https://godoc.org/github.com/boz/go-throttle) [![Build Status](https://travis-ci.org/boz/go-throttle.svg?branch=master)](https://travis-ci.org/boz/go-throttle)

Package `throttle` provides functionality to limit the frequency with which code is called

Throttling is of the `Trigger()` method and depends on the parameters passed (`period`, `trailing`).

The `period` parameter defines how often the throttled code can run.  A period of one second means
that the throttled code will run at most once per second.

The `trailing` parameter defines what hapens if `Trigger()` is called after the throttled code has been
started, but before the period is finished.  If `trailing` is false then these triggers are ignored.
If `trailing` is true then the throttled code is executed one more time at the beginning of the next period.

Example with `period = time.Second` and `trailing = false`:

    Whole seconds after first trigger...|0|0|0|0|1|1|1|1|
    Trigger() gets called...............|X| |X| | |X| | |
    Throttled code gets called..........|X| | | | |X| | |

Note that the second `Trigger()` had no effect.  The third `Trigger()` caused immediate execution of the
throttled code.

Example with `period = time.Second` and `trailing = true`:

    Whole seconds after first trigger...|0|0|0|0|1|1|1|1|
    Trigger() gets called...............|X| |X| | |X| | |
    Throttled code gets called..........|X| | | |X| | | |

Note that the second `Trigger()` causes the throttled code to get called once the first period is over.
The third `Trigger()` will do the same.

## Usage

Throttling execution of a function:

```go
throttle := throttle.ThrottleFunc(period, false, func() {
  fmt.Println("fun, throttled.")
})

go func() {
  for i := 0; i < 5; i++ {
    throttle.Trigger()
    time.Sleep(period / 6)
  }
}()

time.Sleep(2 * period)
throttle.Stop()

// Output: fun, throttled.
```

Throttling arbitrary code:

```go
package cache

import (
	"time"
	"github.com/boz/go-throttle"
)

type CacheRebuilder struct {
	throttle throttle.Throttle
}

// Create a cache rebuilder which will rebuild the cache at most once every 5 minutes, regardless
// of how often a rebuild is requested.
func NewRebuilder() *CacheRebuilder {
	cr := &CacheRebuilder{NewThrottle(5*time.Minute, true)}

	go func() {
		for cr.throttle.Next() {
			cr.doRebuild()
		}
	}()

	return cr
}

func (cr *CacheRebuilder) Stop() {
	cr.throttle.Stop()
}

func (cr *CacheRebuilder) Rebuild() {
	cr.throttle.Trigger()
}

func (cr *CacheRebuilder) doRebuild() {
	// actually rebuild the cache.
}
```
