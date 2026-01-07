package mpb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/vbauerster/mpb/v8/cwriter"
	"github.com/vbauerster/mpb/v8/decor"
)

const defaultRefreshRate = 150 * time.Millisecond
const defaultHmQueueLength = 128

// ErrDone represents use after `(*Progress).Wait()` error.
var ErrDone = fmt.Errorf("%T instance can't be reused after %[1]T.Wait()", (*Progress)(nil))

// Progress represents a container that renders one or more progress bars.
type Progress struct {
	uwg          *sync.WaitGroup
	pwg, bwg     sync.WaitGroup
	operateState chan func(*pState)
	interceptIO  chan func(io.Writer)
	done         <-chan struct{}
	cancel       func()
}

// pState holds bars in its priorityQueue, it gets passed to (*Progress).serve monitor goroutine.
type pState struct {
	ctx         context.Context
	hm          heapManager
	iterDrop    chan struct{}
	renderReq   chan time.Time
	idCount     int
	popPriority int

	// following are provided/overrode by user
	hmQueueLen       int
	reqWidth         int
	refreshRate      time.Duration
	popCompleted     bool
	autoRefresh      bool
	delayRC          <-chan struct{}
	manualRC         <-chan interface{}
	shutdownNotifier chan<- interface{}
	queueBars        map[*Bar]*Bar
	output           io.Writer
	debugOut         io.Writer
	uwg              *sync.WaitGroup
}

// New creates new Progress container instance. It's not possible to
// reuse instance after `(*Progress).Wait` method has been called.
func New(options ...ContainerOption) *Progress {
	return NewWithContext(context.Background(), options...)
}

// NewWithContext creates new Progress container instance with provided
// context. It's not possible to reuse instance after `(*Progress).Wait`
// method has been called.
func NewWithContext(ctx context.Context, options ...ContainerOption) *Progress {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &pState{
		ctx:         ctx,
		hmQueueLen:  defaultHmQueueLength,
		iterDrop:    make(chan struct{}),
		renderReq:   make(chan time.Time),
		popPriority: math.MinInt32,
		refreshRate: defaultRefreshRate,
		queueBars:   make(map[*Bar]*Bar),
		output:      os.Stdout,
		debugOut:    io.Discard,
	}

	for _, opt := range options {
		if opt != nil {
			opt(s)
		}
	}

	s.hm = make(heapManager, s.hmQueueLen)

	p := &Progress{
		uwg:          s.uwg,
		operateState: make(chan func(*pState)),
		interceptIO:  make(chan func(io.Writer)),
		cancel:       cancel,
	}

	cw := cwriter.New(s.output)
	if s.manualRC != nil {
		done := make(chan struct{})
		p.done = done
		s.autoRefresh = false
		go s.manualRefreshListener(done)
	} else if cw.IsTerminal() || s.autoRefresh {
		done := make(chan struct{})
		p.done = done
		s.autoRefresh = true
		go s.autoRefreshListener(done)
	} else {
		p.done = ctx.Done()
		s.autoRefresh = false
	}

	p.pwg.Add(1)
	go p.serve(s, cw)
	go s.hm.run()
	return p
}

// AddBar creates a bar with default bar filler.
func (p *Progress) AddBar(total int64, options ...BarOption) *Bar {
	return p.New(total, BarStyle(), options...)
}

// AddSpinner creates a bar with default spinner filler.
func (p *Progress) AddSpinner(total int64, options ...BarOption) *Bar {
	return p.New(total, SpinnerStyle(), options...)
}

// New creates a bar by calling `Build` method on provided `BarFillerBuilder`.
func (p *Progress) New(total int64, builder BarFillerBuilder, options ...BarOption) *Bar {
	if builder == nil {
		return p.MustAdd(total, nil, options...)
	}
	return p.MustAdd(total, builder.Build(), options...)
}

// MustAdd creates a bar which renders itself by provided BarFiller.
// If `total <= 0` triggering complete event by increment methods is
// disabled. Panics if called after `(*Progress).Wait()`.
func (p *Progress) MustAdd(total int64, filler BarFiller, options ...BarOption) *Bar {
	bar, err := p.Add(total, filler, options...)
	if err != nil {
		panic(err)
	}
	return bar
}

// Add creates a bar which renders itself by provided BarFiller.
// If `total <= 0` triggering complete event by increment methods
// is disabled. If called after `(*Progress).Wait()` then
// `(nil, ErrDone)` is returned.
func (p *Progress) Add(total int64, filler BarFiller, options ...BarOption) (*Bar, error) {
	if filler == nil {
		filler = NopStyle().Build()
	} else if f, ok := filler.(BarFillerFunc); ok && f == nil {
		filler = NopStyle().Build()
	}
	ch := make(chan *Bar)
	select {
	case p.operateState <- func(ps *pState) {
		bs := ps.makeBarState(total, filler, options...)
		bar := newBar(ps.ctx, p, bs)
		if bs.waitBar != nil {
			ps.queueBars[bs.waitBar] = bar
		} else {
			ps.hm.push(bar, true)
		}
		ps.idCount++
		ch <- bar
	}:
		return <-ch, nil
	case <-p.done:
		return nil, ErrDone
	}
}

func (p *Progress) traverseBars(cb func(b *Bar) bool) {
	drop, iter := make(chan struct{}), make(chan *Bar)
	select {
	case p.operateState <- func(s *pState) { s.hm.iter(drop, iter, nil) }:
		for b := range iter {
			if !cb(b) {
				close(drop)
				break
			}
		}
	case <-p.done:
	}
}

// UpdateBarPriority either immediately or lazy.
// With lazy flag order is updated after the next refresh cycle.
// If you don't care about laziness just use `(*Bar).SetPriority(int)`.
func (p *Progress) UpdateBarPriority(b *Bar, priority int, lazy bool) {
	if b == nil {
		return
	}
	select {
	case p.operateState <- func(s *pState) { s.hm.fix(b, priority, lazy) }:
	case <-p.done:
	}
}

// Write is implementation of io.Writer.
// Writing to `*Progress` will print lines above a running bar.
// Writes aren't flushed immediately, but at next refresh cycle.
// If called after `(*Progress).Wait()` then `(0, ErrDone)` is returned.
func (p *Progress) Write(b []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result)
	select {
	case p.interceptIO <- func(w io.Writer) {
		n, err := w.Write(b)
		ch <- result{n, err}
	}:
		res := <-ch
		return res.n, res.err
	case <-p.done:
		return 0, ErrDone
	}
}

// Wait waits for all bars to complete and finally shutdowns container. After
// this method has been called, there is no way to reuse `*Progress` instance.
func (p *Progress) Wait() {
	p.bwg.Wait()
	p.Shutdown()
	// wait for user wg, if any
	if p.uwg != nil {
		p.uwg.Wait()
	}
}

// Shutdown cancels any running bar immediately and then shutdowns `*Progress`
// instance. Normally this method shouldn't be called unless you know what you
// are doing. Proper way to shutdown is to call `(*Progress).Wait()` instead.
func (p *Progress) Shutdown() {
	p.cancel()
	p.pwg.Wait()
}

func (p *Progress) serve(s *pState, cw *cwriter.Writer) {
	defer p.pwg.Done()
	var err error
	var w *cwriter.Writer
	renderReq := s.renderReq
	operateState := p.operateState
	interceptIO := p.interceptIO

	if s.delayRC != nil {
		w = cwriter.New(io.Discard)
	} else {
		w, cw = cw, nil
	}

	for {
		select {
		case <-s.delayRC:
			w, cw = cw, nil
			s.delayRC = nil
		case op := <-operateState:
			op(s)
		case fn := <-interceptIO:
			fn(w)
		case <-renderReq:
			err = s.render(w)
			if err != nil {
				// (*pState).(autoRefreshListener|manualRefreshListener) may block
				// if not launching following short lived goroutine
				go func() {
					for {
						select {
						case <-s.renderReq:
						case <-p.done:
							return
						}
					}
				}()
				p.cancel() // cancel all bars
				renderReq = nil
				operateState = nil
				interceptIO = nil
			}
		case <-p.done:
			if err != nil {
				_, _ = fmt.Fprintln(s.debugOut, err.Error())
			} else if s.autoRefresh {
				update := make(chan bool)
				for i := 0; i == 0 || <-update; i++ {
					if err := s.render(w); err != nil {
						_, _ = fmt.Fprintln(s.debugOut, err.Error())
						break
					}
					s.hm.state(update)
				}
			}
			s.hm.end(s.shutdownNotifier)
			return
		}
	}
}

func (s *pState) autoRefreshListener(done chan struct{}) {
	ticker := time.NewTicker(s.refreshRate)
	defer ticker.Stop()
	for {
		select {
		case t := <-ticker.C:
			s.renderReq <- t
		case <-s.ctx.Done():
			close(done)
			return
		}
	}
}

func (s *pState) manualRefreshListener(done chan struct{}) {
	for {
		select {
		case x := <-s.manualRC:
			if t, ok := x.(time.Time); ok {
				s.renderReq <- t
			} else {
				s.renderReq <- time.Now()
			}
		case <-s.ctx.Done():
			close(done)
			return
		}
	}
}

func (s *pState) render(cw *cwriter.Writer) (err error) {
	iter, iterPop := make(chan *Bar), make(chan *Bar)
	s.hm.sync(s.iterDrop)
	s.hm.iter(s.iterDrop, iter, iterPop)

	var width, height int
	if cw.IsTerminal() {
		width, height, err = cw.GetTermSize()
		if err != nil {
			close(s.iterDrop)
			return err
		}
	} else {
		if s.reqWidth > 0 {
			width = s.reqWidth
		} else {
			width = 80
		}
		height = width
	}

	var barCount int
	for b := range iter {
		barCount++
		go b.render(width)
	}

	return s.flush(cw, height, barCount, iterPop)
}

func (s *pState) flush(cw *cwriter.Writer, height, barCount int, iter <-chan *Bar) error {
	var total, popCount int
	rows := make([][]io.Reader, 0, barCount)

	for b := range iter {
		frame := <-b.frameCh
		if frame.err != nil {
			close(s.iterDrop)
			b.cancel()
			return frame.err // b.frameCh is buffered it's ok to return here
		}
		var discarded int
		for i := len(frame.rows) - 1; i >= 0; i-- {
			if total < height {
				total++
			} else {
				_, _ = io.Copy(io.Discard, frame.rows[i]) // Found IsInBounds
				discarded++
			}
		}
		rows = append(rows, frame.rows)

		switch frame.shutdown {
		case 1:
			b.cancel()
			if qb, ok := s.queueBars[b]; ok {
				delete(s.queueBars, b)
				qb.priority = b.priority
				s.hm.push(qb, true)
			} else if s.popCompleted && !frame.noPop {
				b.priority = s.popPriority
				s.popPriority++
				s.hm.push(b, false)
			} else if !frame.rmOnComplete {
				s.hm.push(b, false)
			}
		case 2:
			if s.popCompleted && !frame.noPop {
				popCount += len(frame.rows) - discarded
				continue
			}
			fallthrough
		default:
			s.hm.push(b, false)
		}
	}

	for i := len(rows) - 1; i >= 0; i-- {
		for _, r := range rows[i] {
			_, err := cw.ReadFrom(r)
			if err != nil {
				return err
			}
		}
	}

	return cw.Flush(total - popCount)
}

func (s pState) makeBarState(total int64, filler BarFiller, options ...BarOption) *bState {
	bs := &bState{
		id:          s.idCount,
		priority:    s.idCount,
		reqWidth:    s.reqWidth,
		total:       total,
		filler:      filler,
		renderReq:   s.renderReq,
		autoRefresh: s.autoRefresh,
		extender: func(_ decor.Statistics, rows ...io.Reader) ([]io.Reader, error) {
			return rows, nil
		},
	}

	if total > 0 {
		bs.triggerComplete = true
	}

	for _, opt := range options {
		if opt != nil {
			opt(bs)
		}
	}

	for _, group := range bs.decorGroups {
		for _, d := range group {
			if d, ok := unwrap(d).(decor.EwmaDecorator); ok {
				bs.ewmaDecorators = append(bs.ewmaDecorators, d)
			}
		}
	}

	bs.buffers[0] = bytes.NewBuffer(make([]byte, 0, 128)) // prepend
	bs.buffers[1] = bytes.NewBuffer(make([]byte, 0, 128)) // append
	bs.buffers[2] = bytes.NewBuffer(make([]byte, 0, 256)) // filler

	return bs
}
