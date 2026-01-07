//go:build !remote

package libpod

import (
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/google/shlex"
	"github.com/sirupsen/logrus"
)

var isDescriptor = map[string]bool{}

func init() {
	allDescriptors, err := util.GetContainerPidInformationDescriptors()
	if err != nil {
		// Should never happen
		logrus.Debugf("failed call to util.GetContainerPidInformationDescriptors()")
		return
	}
	for _, d := range allDescriptors {
		isDescriptor[d] = true
	}
}

// Top gathers statistics about the running processes in a container. It returns a
// []string for output
func (c *Container) Top(descriptors []string) ([]string, error) {
	conStat, err := c.State()
	if err != nil {
		return nil, fmt.Errorf("unable to look up state for %s: %w", c.ID(), err)
	}
	if conStat != define.ContainerStateRunning {
		return nil, errors.New("top can only be used on running containers")
	}

	// Default to 'ps -ef' compatible descriptors
	if len(strings.Join(descriptors, "")) == 0 {
		descriptors = []string{"user", "pid", "ppid", "pcpu", "etime", "tty", "time", "args"}
	}

	// If everything in descriptors is a supported AIX format
	// descriptor, we use 'ps -ao <descriptors>', otherwise we pass
	// everything straight through to ps.
	supportedDescriptors := true
	for _, d := range descriptors {
		if _, ok := isDescriptor[d]; !ok {
			supportedDescriptors = false
			break
		}
	}
	if supportedDescriptors {
		descriptors = []string{"-o", strings.Join(descriptors, ",")}
	}

	// Note that the descriptors to ps(1) must be shlexed (see #12452).
	psDescriptors := []string{}
	for _, d := range descriptors {
		shSplit, err := shlex.Split(d)
		if err != nil {
			return nil, fmt.Errorf("parsing ps args: %w", err)
		}
		for _, s := range shSplit {
			if s != "" {
				psDescriptors = append(psDescriptors, s)
			}
		}
	}

	jailName, err := c.jailName()
	if err != nil {
		return nil, fmt.Errorf("getting jail name: %w", err)
	}

	args := []string{
		"-J",
		jailName,
	}
	args = append(args, psDescriptors...)

	output, err := execPS(args)
	if err != nil {
		return nil, fmt.Errorf("executing ps(1): %w", err)
	}

	return output, nil
}

func execPS(args []string) ([]string, error) {
	cmd := exec.Command("ps", args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	stdout := []string{}
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			stdout = append(stdout, scanner.Text())
		}
		wg.Done()
	}()
	stderr := []string{}
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			stderr = append(stderr, scanner.Text())
		}
		wg.Done()
	}()

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	wg.Wait()
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ps(1) command failed: %w, output: %s", err, strings.Join(stderr, " "))
	}

	return stdout, nil
}
