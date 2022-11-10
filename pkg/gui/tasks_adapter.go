package gui

import (
	"context"
	"time"

	"github.com/jesseduffield/lazydocker/pkg/tasks"
)

func (gui *Gui) QueueTask(f func(ctx context.Context)) error {
	return gui.taskManager.NewTask(f)
}

type RenderStringTaskOpts struct {
	Autoscroll    bool
	Wrap          bool
	GetStrContent func() string
}

type TaskOpts struct {
	Autoscroll bool
	Wrap       bool
	Func       func(ctx context.Context)
}

type TickerTaskOpts struct {
	Duration   time.Duration
	Before     func(ctx context.Context)
	Func       func(ctx context.Context, notifyStopped chan struct{})
	Autoscroll bool
	Wrap       bool
}

func (gui *Gui) NewRenderStringTask(opts RenderStringTaskOpts) tasks.TaskFunc {
	taskOpts := TaskOpts{
		Autoscroll: opts.Autoscroll,
		Wrap:       opts.Wrap,
		Func: func(ctx context.Context) {
			gui.RenderStringMain(opts.GetStrContent())
		},
	}

	return gui.NewTask(taskOpts)
}

// assumes it's cheap to obtain the content (otherwise we would pass a function that returns the content)
func (gui *Gui) NewSimpleRenderStringTask(getContent func() string) tasks.TaskFunc {
	return gui.NewRenderStringTask(RenderStringTaskOpts{
		GetStrContent: getContent,
		Autoscroll:    false,
		Wrap:          gui.Config.UserConfig.Gui.WrapMainPanel,
	})
}

func (gui *Gui) NewTask(opts TaskOpts) tasks.TaskFunc {
	return func(ctx context.Context) {
		mainView := gui.Views.Main
		mainView.Autoscroll = opts.Autoscroll
		mainView.Wrap = opts.Wrap

		opts.Func(ctx)
	}
}

// NewTickerTask is a convenience function for making a new task that repeats some action once per e.g. second
// the before function gets called after the lock is obtained, but before the ticker starts.
// if you handle a message on the stop channel in f() you need to send a message on the notifyStopped channel because returning is not sufficient. Here, unlike in a regular task, simply returning means we're now going to wait till the next tick to run again.
func (gui *Gui) NewTickerTask(opts TickerTaskOpts) tasks.TaskFunc {
	notifyStopped := make(chan struct{}, 10)

	task := func(ctx context.Context) {
		if opts.Before != nil {
			opts.Before(ctx)
		}
		tickChan := time.NewTicker(opts.Duration)
		defer tickChan.Stop()
		// calling f first so that we're not waiting for the first tick
		opts.Func(ctx, notifyStopped)
		for {
			select {
			case <-notifyStopped:
				gui.Log.Info("exiting ticker task due to notifyStopped channel")
				return
			case <-ctx.Done():
				gui.Log.Info("exiting ticker task due to stopped channel")
				return
			case <-tickChan.C:
				gui.Log.Info("running ticker task again")
				opts.Func(ctx, notifyStopped)
			}
		}
	}

	taskOpts := TaskOpts{
		Autoscroll: opts.Autoscroll,
		Wrap:       opts.Wrap,
		Func:       task,
	}

	return gui.NewTask(taskOpts)
}
