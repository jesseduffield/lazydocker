package gui

import (
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
}

const (
	TickIntervalMs = 50
)

func (m *statusManager) removeStatus(name string) {
	newStatuses := []appStatus{}
	for _, status := range m.statuses {
		if status.name != name {
			newStatuses = append(newStatuses, status)
		}
	}
	m.statuses = newStatuses
}

func (m *statusManager) addStatus(name string, statusType string, duration int) {
	m.removeStatus(name)
	newStatus := appStatus{
		name:       name,
		statusType: statusType,
		duration:   duration,
	}
	m.statuses = append([]appStatus{newStatus}, m.statuses...)
}

func (m *statusManager) getStatusString() string {
	if len(m.statuses) == 0 {
		return ""
	}

	topStatus := m.statuses[0]
	if topStatus.statusType == "waiting" {
		return topStatus.name + " " + utils.Loader()
	} else if topStatus.statusType == "info" {
		return topStatus.name
	}

	return topStatus.name
}

// WithWaitingStatus wraps a function and shows a waiting status while the function is still executing
func (gui *Gui) WithWaitingStatus(name string, f func() error) error {
	go func() {
		go gui.Notify(name, "waiting", 0)()

		defer func() {
			gui.statusManager.removeStatus(name)
		}()

		if err := f(); err != nil {
			gui.g.Update(func(g *gocui.Gui) error {
				return gui.createErrorPanel(err.Error())
			})
		}
	}()

	return nil
}

// Notify sends static notification to the user.
// duration of 0 will disable the self-cleaning of the notification
func (gui *Gui) Notify(name string, statusType string, duration int) func() {
	return func() {
		gui.statusManager.addStatus(name, statusType, duration)

		defer func() {
			gui.statusManager.removeStatus(name)
		}()

		ticker := time.NewTicker(time.Millisecond * TickIntervalMs)
		tickCount := 0
		endTick := duration * 1000 / TickIntervalMs

		defer ticker.Stop()
		for range ticker.C {
			tickCount++
			// If no duration, don't terminate early
			if duration > 0 && tickCount >= endTick {
				return
			}

			appStatus := gui.statusManager.getStatusString()
			if appStatus == "" {
				return
			}

			if err := gui.renderString(gui.g, "appStatus", appStatus); err != nil {
				gui.Log.Warn(err)
			}
		}
	}
}
