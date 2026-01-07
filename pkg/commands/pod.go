package commands

import (
	"github.com/sirupsen/logrus"
)

// Pod represents a Podman pod with its containers.
type Pod struct {
	ID         string
	Name       string
	Summary    PodSummary
	Containers []*Container // Non-infra containers in this pod
	OSCommand  *OSCommand
	Log        *logrus.Entry
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
