//go:build !windows

// Package for handling processes and PIDs.
package pidhandle

import (
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// PIDHandle defines an interface for working with operating system processes
// in a reliable way. OS-specific implementations include additional logic
// to try to ensure that operations (e.g., sending signals) are performed
// on the exact same process that was originally referenced when the PIDHandle
// was created via NewPIDHandle or NewPIDHandleFromString.
//
// This prevents accidental interaction with a different process in scenarios
// where the original process has exited and its PID has been reused by
// the system for an unrelated process.
type PIDHandle interface {
	// Returns the PID associated with this PIDHandle.
	PID() int
	// Releases the PIDHandle resources.
	Close() error
	// Sends the signal to process.
	Kill(signal unix.Signal) error
	// Returns true in case the process is still alive.
	IsAlive() (bool, error)
	// Returns a serialized representation of the PIDHandle.
	// This string can be passed to NewPIDHandleFromString to recreate
	// a PIDHandle that reliably refers to the same process as the original.
	String() (string, error)
}

// The pidData value used when no process with this PID exists when creating
// the PIDHandle.
const noSuchProcessID = "no-proc"

// The pidData prefix used when only the process start time (creation time)
// is supported when creating the PIDHandle to uniquely identify the process.
const startTimePrefix = "start-time:"

type pidHandle struct {
	pid     int
	pidData string
}

// Returns the PID.
func (h *pidHandle) PID() int {
	return h.pid
}

// Close releases the PIDHandle resource.
func (h *pidHandle) Close() error {
	// No resources for the default PIDHandle implementation.
	return nil
}

// Sends the signal to process.
func (h *pidHandle) Kill(signal unix.Signal) error {
	if h.pidData == noSuchProcessID {
		// The process did not exist when we created the PIDHandle, so return
		// ESRCH error.
		return unix.ESRCH
	}

	// Get the start-time of the process and check if it is the same as
	// the one we store in pidData. If it is not, we know that the PID
	// has been recycled and return ESRCH error.
	startTime, found := strings.CutPrefix(h.pidData, startTimePrefix)
	if found {
		p, err := process.NewProcess(int32(h.pid))
		if err != nil {
			if err == process.ErrorProcessNotRunning {
				return unix.ESRCH
			}
			return err
		}

		ctime, err := p.CreateTime()
		if err != nil {
			return err
		}

		if strconv.FormatInt(ctime, 10) != startTime {
			return unix.ESRCH
		}
	}

	return unix.Kill(h.pid, signal)
}

// Returns true in case the process is still alive.
func (h *pidHandle) IsAlive() (bool, error) {
	err := h.Kill(0)
	if err != nil {
		if err == unix.ESRCH {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Returns a serialized representation of the PIDHandle.
// This string can be passed to NewPIDHandleFromString to recreate
// a PIDHandle that reliably refers to the same process as the original.
func (h *pidHandle) String() (string, error) {
	if len(h.pidData) != 0 {
		return h.pidData, nil
	}

	// Get the start-time of the process and return it as string.
	p, err := process.NewProcess(int32(h.pid))
	if err != nil {
		if err == process.ErrorProcessNotRunning {
			return noSuchProcessID, nil
		}
		return "", err
	}

	ctime, err := p.CreateTime()
	if err != nil {
		// The process existed, but we cannot get its start-time. There is
		// either an issue with getting it, or the process terminated in the
		// mean-time. We have no way to find out what actually happened, so
		// in this case, we just fallback to an empty string. This will mean
		// that Kill or IsAlive might kill wrong process in rare situation
		// when CreateTime() failed for different reason than the process
		// terminated...
		logrus.Debugf("Getting CreateTime for process (%d) failed: %v", h.pid, err)
		return "", nil
	}

	return startTimePrefix + strconv.FormatInt(ctime, 10), nil
}
