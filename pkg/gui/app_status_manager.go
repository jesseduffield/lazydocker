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

func (m *statusManager) removeStatus(name string) {
	newStatuses := []appStatus{}
	for _, status := range m.statuses {
		if status.name != name {
			newStatuses = append(newStatuses, status)
		}
	}
	m.statuses = newStatuses
}

func (m *statusManager) addWaitingStatus(name string) {
	m.removeStatus(name)
	newStatus := appStatus{
		name:       name,
		statusType: "waiting",
		duration:   0,
	}
	m.statuses = append([]appStatus{newStatus}, m.statuses...)
}

// addMessage adds a transient message status that will be removed after the given duration.
func (m *statusManager) addMessage(name string, duration time.Duration) {
	m.removeStatus(name)
	newStatus := appStatus{
		name:       name,
		statusType: "message",
		duration:   int(duration / time.Millisecond),
	}
	m.statuses = append([]appStatus{newStatus}, m.statuses...)

	// schedule removal after duration
	go func() {
		time.Sleep(duration)
		m.removeStatus(name)
	}()
}

func (m *statusManager) getStatusString() string {
	if len(m.statuses) == 0 {
		return ""
	}
	topStatus := m.statuses[0]
	if topStatus.statusType == "waiting" {
		return topStatus.name + " " + utils.Loader()
	}
	return topStatus.name
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

// AddMessage shows a transient message in the appStatus area for the given duration.
func (gui *Gui) AddMessage(name string, duration time.Duration) {
	if gui.statusManager == nil {
		return
	}
	gui.statusManager.addMessage(name, duration)
	// ensure the UI is updated immediately
	go func() {
		// repeatedly render until the message expires so layout picks it up
		ticker := time.NewTicker(time.Millisecond * 50)
		defer ticker.Stop()
		start := time.Now()
		for range ticker.C {
			if time.Since(start) > duration+time.Millisecond*100 {
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
	}()
}
