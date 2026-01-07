//go:build !remote

package libpod

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
)

// GetPodPidInformation returns process-related data of all processes in
// the pod.  The output data can be controlled via the `descriptors`
// argument which expects format descriptors and supports all AIXformat
// descriptors of ps (1) plus some additional ones to for instance inspect the
// set of effective capabilities.  Each element in the returned string slice
// is a tab-separated string.
//
// For more details, please refer to github.com/containers/psgo.
func (p *Pod) GetPodPidInformation(descriptors []string) ([]string, error) {
	// Default to 'ps -ef' compatible descriptors
	if len(strings.Join(descriptors, "")) == 0 {
		descriptors = []string{"user", "pid", "ppid", "pcpu", "etime", "tty", "time", "args"}
	}

	jailNames := make([]string, 0)
	ctrsInPod, err := p.AllContainers()
	if err != nil {
		return nil, err
	}
	for _, c := range ctrsInPod {
		c.lock.Lock()
		err := c.syncContainer()
		c.lock.Unlock()
		if err != nil {
			return nil, err
		}

		if c.state.State == define.ContainerStateRunning {
			jailName, err := c.jailName()
			if err != nil {
				return nil, fmt.Errorf("getting jail name: %w", err)
			}
			jailNames = append(jailNames, jailName)
		}
	}

	// Also support comma-separated input.
	psDescriptors := []string{}
	for _, d := range descriptors {
		for _, s := range strings.Split(d, ",") {
			if s != "" {
				psDescriptors = append(psDescriptors, s)
			}
		}
	}

	// For consistency with pod_top_linux.go, only allow descriptor names
	for _, d := range psDescriptors {
		if _, ok := isDescriptor[d]; !ok {
			return nil, fmt.Errorf("unknown descriptor: %s", d)
		}
	}

	args := []string{
		"-J",
		strings.Join(jailNames, ","),
		"-ao",
		strings.Join(psDescriptors, ","),
	}

	output, err := execPS(args)
	if err != nil {
		return nil, fmt.Errorf("executing ps(1): %w", err)
	}

	return output, nil
}
