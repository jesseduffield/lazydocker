package gui

import "sync"

// coalescingRunner runs a function at most once at a time. A Run call made
// while a run is in flight does not start a parallel run: it is coalesced
// into a single follow-up run once the current run finishes, no matter how
// many calls arrived in the meantime.
//
// This is what keeps the event-driven refresh from piling up parallel
// refreshes against the daemon: Docker events can arrive far faster than a
// refresh completes, and a rate limit alone cannot stop the in-flight
// requests from growing without bound.
// See https://github.com/jesseduffield/lazydocker/issues/849
type coalescingRunner struct {
	mu      sync.Mutex
	running bool
	pending bool
}

// Run executes fn, coalescing with a run already in flight if there is one.
func (r *coalescingRunner) Run(fn func()) {
	r.mu.Lock()
	if r.running {
		r.pending = true
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	go func() {
		for {
			fn()

			r.mu.Lock()
			if !r.pending {
				r.running = false
				r.mu.Unlock()
				return
			}
			r.pending = false
			r.mu.Unlock()
		}
	}()
}
