package mpb

import (
	"io"
	"sync"
	"time"
)

// ContainerOption is a func option to alter default behavior of a bar
// container. Container term refers to a Progress struct which can
// hold one or more Bars.
type ContainerOption func(*pState)

// WithWaitGroup provides means to have a single joint point. If
// *sync.WaitGroup is provided, you can safely call just p.Wait()
// without calling Wait() on provided *sync.WaitGroup. Makes sense
// when there are more than one bar to render.
func WithWaitGroup(wg *sync.WaitGroup) ContainerOption {
	return func(s *pState) {
		s.uwg = wg
	}
}

// WithWidth sets container width. If not set it defaults to terminal
// width. A bar added to the container will inherit its width, unless
// overridden by `func BarWidth(int) BarOption`.
func WithWidth(width int) ContainerOption {
	return func(s *pState) {
		s.reqWidth = width
	}
}

// WithQueueLen sets buffer size of heap manager channel. Ideally it must be
// kept at MAX value, where MAX is number of bars to be rendered at the same
// time. If len < MAX then backpressure to the scheduler will be increased as
// MAX-len extra goroutines will be launched at each render cycle.
// Default queue len is 128.
func WithQueueLen(len int) ContainerOption {
	return func(s *pState) {
		s.hmQueueLen = len
	}
}

// WithRefreshRate overrides default 150ms refresh rate.
func WithRefreshRate(d time.Duration) ContainerOption {
	return func(s *pState) {
		s.refreshRate = d
	}
}

// WithManualRefresh disables internal auto refresh time.Ticker.
// Refresh will occur upon receive value from provided ch.
func WithManualRefresh(ch <-chan interface{}) ContainerOption {
	return func(s *pState) {
		s.manualRC = ch
	}
}

// WithRenderDelay delays rendering. By default rendering starts as
// soon as bar is added, with this option it's possible to delay
// rendering process by keeping provided chan unclosed. In other words
// rendering will start as soon as provided chan is closed.
func WithRenderDelay(ch <-chan struct{}) ContainerOption {
	return func(s *pState) {
		s.delayRC = ch
	}
}

// WithShutdownNotifier value of type `[]*mpb.Bar` will be send into provided
// channel upon container shutdown.
func WithShutdownNotifier(ch chan<- interface{}) ContainerOption {
	return func(s *pState) {
		s.shutdownNotifier = ch
	}
}

// WithOutput overrides default os.Stdout output. If underlying io.Writer
// is not a terminal then auto refresh is disabled unless WithAutoRefresh
// option is set.
func WithOutput(w io.Writer) ContainerOption {
	if w == nil {
		w = io.Discard
	}
	return func(s *pState) {
		s.output = w
	}
}

// WithDebugOutput sets debug output.
func WithDebugOutput(w io.Writer) ContainerOption {
	if w == nil {
		w = io.Discard
	}
	return func(s *pState) {
		s.debugOut = w
	}
}

// WithAutoRefresh force auto refresh regardless of what output is set to.
// Applicable only if not WithManualRefresh set.
func WithAutoRefresh() ContainerOption {
	return func(s *pState) {
		s.autoRefresh = true
	}
}

// PopCompletedMode pop completed bars out of progress container.
// In this mode completed bars get moved to the top and stop
// participating in rendering cycle.
func PopCompletedMode() ContainerOption {
	return func(s *pState) {
		s.popCompleted = true
	}
}

// ContainerOptional will return provided option only when cond is true.
func ContainerOptional(option ContainerOption, cond bool) ContainerOption {
	if cond {
		return option
	}
	return nil
}

// ContainerOptOn will return provided option only when predicate evaluates to true.
func ContainerOptOn(option ContainerOption, predicate func() bool) ContainerOption {
	if predicate() {
		return option
	}
	return nil
}

// ContainerFuncOptional will call option and return its value only when cond is true.
func ContainerFuncOptional(option func() ContainerOption, cond bool) ContainerOption {
	if cond {
		return option()
	}
	return nil
}

// ContainerFuncOptOn will call option and return its value only when predicate evaluates to true.
func ContainerFuncOptOn(option func() ContainerOption, predicate func() bool) ContainerOption {
	if predicate() {
		return option()
	}
	return nil
}
