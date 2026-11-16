package gui

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestCoalescingRunnerCoalescesConcurrentRuns is a regression test for
// https://github.com/jesseduffield/lazydocker/issues/849: every Docker event
// triggers a refresh, and with no guard against overlapping refreshes the
// refreshes pile up without bound (thousands of in-flight /images/json
// requests, OOM-ing dockerd). While one run is in flight, further Run calls
// must be coalesced into a single follow-up run instead of starting
// parallel runs.
func TestCoalescingRunnerCoalescesConcurrentRuns(t *testing.T) {
	runner := &coalescingRunner{}

	var runs atomic.Int32
	started := make(chan struct{})
	var startOnce sync.Once
	release := make(chan struct{})

	fn := func() {
		runs.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
	}

	// Start the first run in the background; it blocks until released.
	runner.Run(fn)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first run to start")
	}

	// Fire a burst of concurrent runs while the first one is still in flight.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner.Run(fn)
		}()
	}
	wg.Wait()

	// Only the first run has executed so far: the burst must not have
	// started any parallel runs.
	assert.Equal(t, int32(1), runs.Load())

	// Release the first run. The burst must collapse into exactly one
	// follow-up run, not one run per call.
	close(release)

	waitFor(t, func() bool { return runs.Load() == 2 },
		"the burst to be coalesced into exactly one follow-up run")
	assert.Equal(t, int32(2), runs.Load())

	// A later run, once nothing is in flight, still executes.
	runner.Run(func() { runs.Add(1) })
	waitFor(t, func() bool { return runs.Load() == 3 }, "a later run to execute")
}

// TestCoalescingRunnerRunsSequentially covers the non-overlapping case: back
// to back runs each execute exactly once.
func TestCoalescingRunnerRunsSequentially(t *testing.T) {
	runner := &coalescingRunner{}

	var runs atomic.Int32
	runner.Run(func() { runs.Add(1) })
	waitFor(t, func() bool { return runs.Load() == 1 }, "the first run to execute")

	runner.Run(func() { runs.Add(1) })
	waitFor(t, func() bool { return runs.Load() == 2 }, "the second run to execute")
}
