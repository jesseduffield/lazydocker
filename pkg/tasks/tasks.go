package tasks

import (
	"sync"

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
	t.Log.Warn("new task")

	t.waitingMutex.Lock()
	defer t.waitingMutex.Unlock()

	t.Log.Warn("locked mutex")

	if t.currentTask != nil {
		t.Log.Warn("about to ask current task to stop")
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
		t.Log.Warn("running new task")
		f(stop)
		notifyStopped <- struct{}{}
	}()

	return nil
}

func (t *Task) Stop() {
	t.stop <- struct{}{}
	<-t.notifyStopped
	t.Log.Warn("task successfully stopped")
	return
}
