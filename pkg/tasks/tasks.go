package tasks

import (
	"fmt"
	"sync"
	"time"

	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
)

type TaskManager struct {
	waitingTask       *Task
	currentTask       *Task
	waitingMutex      sync.Mutex
	taskIDMutex       sync.Mutex
	Log               *logrus.Entry
	Tr                *i18n.TranslationSet
	waitingTaskAlerts chan struct{}
	newTaskId         int
}

type Task struct {
	stop          chan struct{}
	stopped       bool
	stopMutex     sync.Mutex
	notifyStopped chan struct{}
	Log           *logrus.Entry
	f             func(chan struct{})
}

func NewTaskManager(log *logrus.Entry, translationSet *i18n.TranslationSet) *TaskManager {
	return &TaskManager{Log: log, Tr: translationSet}
}

// Close closes the task manager, killing whatever task may currently be running
func (t *TaskManager) Close() {
	if t.currentTask == nil {
		return
	}

	c := make(chan struct{}, 1)

	go func() {
		t.currentTask.Stop()
		c <- struct{}{}
	}()

	select {
	case <-c:
		return
	case <-time.After(3 * time.Second):
		fmt.Println(t.Tr.CannotKillChildError)
	}
}

func (t *TaskManager) NewTask(f func(stop chan struct{})) error {
	go func() {
		t.taskIDMutex.Lock()
		t.newTaskId++
		taskID := t.newTaskId
		t.taskIDMutex.Unlock()

		t.waitingMutex.Lock()
		defer t.waitingMutex.Unlock()
		if taskID < t.newTaskId {
			return
		}

		stop := make(chan struct{}, 1) // we don't want to block on this in case the task already returned
		notifyStopped := make(chan struct{})

		if t.currentTask != nil {
			t.Log.Info("asking task to stop")
			t.currentTask.Stop()
			t.Log.Info("task stopped")
		}

		t.currentTask = &Task{
			stop:          stop,
			notifyStopped: notifyStopped,
			Log:           t.Log,
			f:             f,
		}

		go func() {
			f(stop)
			t.Log.Info("returned from function, closing notifyStopped")
			close(notifyStopped)
		}()
	}()

	return nil
}

func (t *Task) Stop() {
	t.stopMutex.Lock()
	defer t.stopMutex.Unlock()
	if t.stopped {
		return
	}
	close(t.stop)
	t.Log.Info("closed stop channel, waiting for notifyStopped message")
	<-t.notifyStopped
	t.Log.Info("received notifystopped message")
	t.stopped = true
	return
}

// NewTickerTask is a convenience function for making a new task that repeats some action once per e.g. second
// the before function gets called after the lock is obtained, but before the ticker starts.
// if you handle a message on the stop channel in f() you need to send a message on the notifyStopped channel because returning is not sufficient. Here, unlike in a regular task, simply returning means we're now going to wait till the next tick to run again.
func (t *TaskManager) NewTickerTask(duration time.Duration, before func(stop chan struct{}), f func(stop, notifyStopped chan struct{})) error {
	notifyStopped := make(chan struct{}, 10)

	return t.NewTask(func(stop chan struct{}) {
		if before != nil {
			before(stop)
		}
		tickChan := time.NewTicker(duration)
		defer tickChan.Stop()
		// calling f first so that we're not waiting for the first tick
		f(stop, notifyStopped)
		for {
			select {
			case <-notifyStopped:
				t.Log.Info("exiting ticker task due to notifyStopped channel")
				return
			case <-stop:
				t.Log.Info("exiting ticker task due to stopped cahnnel")
				return
			case <-tickChan.C:
				t.Log.Info("running ticker task again")
				f(stop, notifyStopped)
			}
		}
	})
}
