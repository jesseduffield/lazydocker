package gui

import (
	"sync"
	"time"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

type appStatus struct {
	name       string
	statusType string
	duration   int
}

type statusManager struct {
	statuses []appStatus
	lock     *sync.Mutex
}

func (m *statusManager) removeStatus(name string) {
	newStatuses := []appStatus{}

	m.lock.Lock()
	defer m.lock.Unlock()

	for _, status := range m.statuses {
		if status.name != name {
			newStatuses = append(newStatuses, status)
		}
	}
	m.statuses = newStatuses
}

func (m *statusManager) addWaitingStatus(name string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.removeStatus(name)
	newStatus := appStatus{
		name: name,
		//TODO: add a different enum for information statuses
		statusType: "waiting",
		duration:   0,
	}
	m.statuses = append([]appStatus{newStatus}, m.statuses...)
}

func (m *statusManager) getStatusString() string {
	m.lock.Lock()
	defer m.lock.Unlock()

	if len(m.statuses) == 0 {
		return ""
	}
	topStatus := m.statuses[0]
	if topStatus.statusType == "waiting" {
		return topStatus.name + " " + utils.Loader()
	}
	return topStatus.name
}

// WithStaticWaitingStatus shows a waiting status for a specific duration
func (gui *Gui) WithStaticWaitingStatus(name string, duration time.Duration) error {
	return gui.WithWaitingStatus(name, func() error { time.Sleep(duration); return nil })
}

// WithWaitingStatus wraps a function and shows a waiting status while the function is still executing
func (gui *Gui) WithWaitingStatus(name string, f func() error) error {
	go func() {
		gui.statusManager.addWaitingStatus(name)

		defer func() {
			gui.statusManager.removeStatus(name)
		}()

		go func() {
			ticker := time.NewTicker(time.Millisecond * 50)
			defer ticker.Stop()
			for range ticker.C {
				appStatus := gui.statusManager.getStatusString()
				if appStatus == "" {
					return
				}
				if err := gui.renderString(gui.g, "appStatus", appStatus); err != nil {
					gui.Log.Warn(err)
				}
			}
		}()

		if err := f(); err != nil {
			gui.g.Update(func(g *gocui.Gui) error {
				return gui.createErrorPanel(err.Error())
			})
		}
	}()

	return nil
}
