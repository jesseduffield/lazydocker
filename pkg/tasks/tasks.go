package tasks

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type TaskManager struct {
	waitingTasks []*Task
	currentTask  *Task
	waitingMutex sync.Mutex
	Log          *logrus.Entry
}

type Task struct {
	stop          chan struct{}
	notifyStopped chan struct{}
	Log           *logrus.Entry
}

func NewTaskManager(log *logrus.Entry) *TaskManager {
	return &TaskManager{Log: log}
}

func (t *TaskManager) NewTask(f func(stop chan struct{})) error {
	t.waitingMutex.Lock()
	defer t.waitingMutex.Unlock()

	if t.currentTask != nil {
		t.currentTask.Stop()
	}

	stop := make(chan struct{}, 1) // we don't want to block on this in case the task already returned
	notifyStopped := make(chan struct{})

	t.currentTask = &Task{
		stop:          stop,
		notifyStopped: notifyStopped,
		Log:           t.Log,
	}

	go func() {
		f(stop)
		close(notifyStopped)
	}()

	return nil
}

func (t *Task) Stop() {
	close(t.stop)
	<-t.notifyStopped
	return
}

// NewTickerTask is a convenience function for making a new task that repeats some action once per e.g. second
// the before function gets called after the lock is obtained, but before the ticker starts.
func (t *TaskManager) NewTickerTask(duration time.Duration, before func(stop chan struct{}), f func(stop, notifyStopped chan struct{})) error {
	notifyStopped := make(chan struct{})

	return t.NewTask(func(stop chan struct{}) {
		if before != nil {
			before(stop)
		}
		tickChan := time.NewTicker(duration)
		// calling f first so that we're not waiting for the first tick
		f(stop, notifyStopped)
		for {
			select {
			case <-notifyStopped:
				return
			case <-stop:
				return
			case <-tickChan.C:
				f(stop, notifyStopped)
			}
		}
	})
}
