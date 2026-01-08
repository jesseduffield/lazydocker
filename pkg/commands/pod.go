package commands

import (
	"time"

	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
)

// Pod represents a Podman pod with its containers.
type Pod struct {
	ID              string
	Name            string
	Summary         PodSummary
	Containers      []*Container // Non-infra containers in this pod
	OSCommand       *OSCommand
	Log             *logrus.Entry
	Runtime         ContainerRuntime
	StatHistory     []*RecordedPodStats
	MonitoringStats bool
	StatsMutex      deadlock.Mutex
}

// RecordedPodStats contains the pod stats we've received from Podman.
type RecordedPodStats struct {
	Stats      PodStatsEntry
	RecordedAt time.Time
}

// State returns the pod state.
func (p *Pod) State() string {
	return p.Summary.Status
}

// HasContainers returns true if the pod has non-infra containers.
func (p *Pod) HasContainers() bool {
	return len(p.Containers) > 0
}

// GetRunningContainerCount returns the number of running containers in the pod.
func (p *Pod) GetRunningContainerCount() int {
	count := 0
	for _, c := range p.Containers {
		if c.Summary.State == "running" {
			count++
		}
	}
	return count
}

// appendStats adds a new stats entry to the pod's history.
func (p *Pod) appendStats(stats *RecordedPodStats, maxDuration time.Duration) {
	p.StatsMutex.Lock()
	defer p.StatsMutex.Unlock()

	p.StatHistory = append(p.StatHistory, stats)
	p.eraseOldHistory(maxDuration)
}

// eraseOldHistory removes any history before the user-specified max duration.
func (p *Pod) eraseOldHistory(maxDuration time.Duration) {
	if maxDuration == 0 {
		return
	}

	for i, stat := range p.StatHistory {
		if time.Since(stat.RecordedAt) < maxDuration {
			p.StatHistory = p.StatHistory[i:]
			return
		}
	}

	// All entries are older than maxDuration, clear the history
	p.StatHistory = nil
}

// GetLastStats returns the most recent stats entry.
func (p *Pod) GetLastStats() (*RecordedPodStats, bool) {
	p.StatsMutex.Lock()
	defer p.StatsMutex.Unlock()
	history := p.StatHistory
	if len(history) == 0 {
		return nil, false
	}
	return history[len(history)-1], true
}
