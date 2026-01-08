package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
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

// Restart restarts the pod
func (p *Pod) Restart() error {
	p.Log.Warn(fmt.Sprintf("restarting pod %s", p.Name))
	ctx := context.Background()
	return p.Runtime.RestartPod(ctx, p.ID, nil)
}

// Start starts the pod
func (p *Pod) Start() error {
	p.Log.Warn(fmt.Sprintf("starting pod %s", p.Name))
	ctx := context.Background()
	return p.Runtime.StartPod(ctx, p.ID)
}

// Stop stops the pod
func (p *Pod) Stop() error {
	p.Log.Warn(fmt.Sprintf("stopping pod %s", p.Name))
	ctx := context.Background()
	return p.Runtime.StopPod(ctx, p.ID, nil)
}

// Pause pauses the pod
func (p *Pod) Pause() error {
	p.Log.Warn(fmt.Sprintf("pausing pod %s", p.Name))
	ctx := context.Background()
	return p.Runtime.PausePod(ctx, p.ID)
}

// Unpause unpauses the pod
func (p *Pod) Unpause() error {
	p.Log.Warn(fmt.Sprintf("unpausing pod %s", p.Name))
	ctx := context.Background()
	return p.Runtime.UnpausePod(ctx, p.ID)
}

// Remove removes the pod
func (p *Pod) Remove(force bool) error {
	p.Log.Warn(fmt.Sprintf("removing pod %s", p.Name))
	ctx := context.Background()
	if err := p.Runtime.RemovePod(ctx, p.ID, force); err != nil {
		if strings.Contains(err.Error(), "pod is running") ||
			strings.Contains(err.Error(), "stop the pod before attempting removal") {
			return ComplexError{
				Code:    MustStopContainer,
				Message: err.Error(),
				frame:   xerrors.Caller(1),
			}
		}
		return err
	}
	return nil
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
