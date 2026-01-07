//go:build linux

// Package for handling processes and PIDs.
package pidhandle

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

type pidfdHandle struct {
	pidfd        int
	normalHandle pidHandle
}

// Store the "unix." methods in variables so we can mock them
// in the unit-tests and test out different return value.
var (
	pidfdOpen       = unix.PidfdOpen
	newFileHandle   = unix.NewFileHandle
	openByHandleAt  = unix.OpenByHandleAt
	nameToHandleAt  = unix.NameToHandleAt
	pidfdSendSignal = unix.PidfdSendSignal
)

// The pidData prefix used when the pidfd and name_to_handle is supported
// when creating the PIDHandle to uniquely identify the process.
const nameToHandlePrefix = "name-to-handle:"

// Creates new PIDHandle for a given process pid.
//
// Note that there still can be a race condition if the process terminates
// *before* the PIDHandle is created. It is a caller's responsibility
// to ensure that this either cannot happen or accept this risk.
func NewPIDHandle(pid int) (PIDHandle, error) {
	// Use the pidfd to obtain the file-descriptor pointing to the process.
	pidData := ""
	pidfd, err := pidfdOpen(pid, 0)
	if err != nil {
		switch err {
		case unix.ENOSYS:
			// Do not fail if PidFdOpen is not supported, we will
			// fallback to process start-time later.

		case unix.ESRCH:
			// The process does not exist, so any future call of Kill
			// or IsAlive should return unix.ESRCH, even if the pid is
			// recycled in the future. Let's note it in the pidData.
			pidData = noSuchProcessID

		case unix.EINVAL:
			// The PidfdOpen returns EINVAL if pid is invalid or if it refers
			// to a thread and not to process. This is not a valid PID for
			// PIDHandle and it most likely means the pid has been recycled
			// (or there is a programming error). We therefore store
			// noSuchProcessID into pidData to return unix.ESRCH in
			// the future Kill or IsAlive calls.
			pidData = noSuchProcessID

		default:
			return nil, fmt.Errorf("pidfdOpen failed: %w", err)
		}
	}

	h := pidfdHandle{
		pidfd:        pidfd,
		normalHandle: pidHandle{pid: pid, pidData: pidData},
	}

	pidData, err = h.String()
	if err != nil {
		return nil, err
	}
	h.normalHandle.pidData = pidData
	return &h, nil
}

// Creates new PIDHandle for a given process pid using the pidData
// originally obtained from PIDHandle.String().
func NewPIDHandleFromString(pid int, pidData string) (PIDHandle, error) {
	h := pidfdHandle{
		pidfd:        -1,
		normalHandle: pidHandle{pid: pid, pidData: pidData},
	}

	// Open the pidfd encoded in pidData.
	data, found := strings.CutPrefix(pidData, nameToHandlePrefix)
	if found {
		// Split the data.
		parts := strings.SplitN(data, " ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format, expected 2 parts")
		}

		// Parse fhType.
		fhTypeInt, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, err
		}
		fhType := int32(fhTypeInt)

		// Decode hex string to bytes.
		bytes, err := hex.DecodeString(parts[1])
		if err != nil {
			return nil, err
		}

		// Create FileHandle and open it.
		fh := newFileHandle(fhType, bytes)
		fd, err := pidfdOpen(os.Getpid(), 0)
		if err != nil {
			return nil, err
		}
		defer unix.Close(fd)
		pidfd, err := openByHandleAt(fd, fh, 0)
		if err != nil {
			if err == unix.ESTALE {
				h.normalHandle.pidData = noSuchProcessID
				return &h, nil
			}
			return nil, fmt.Errorf("openByHandleAt failed: %w", err)
		}
		h.pidfd = pidfd
		return &h, nil
	}

	return &h, nil
}

// Returns the PID associated with this PIDHandle.
func (h *pidfdHandle) PID() int {
	return h.normalHandle.PID()
}

// Close releases the pidfd resource.
func (h *pidfdHandle) Close() error {
	if h.pidfd != 0 {
		err := unix.Close(h.pidfd)
		if err != nil {
			return fmt.Errorf("failed to close pidfd: %w", err)
		}
		h.pidfd = 0
	}
	return h.normalHandle.Close()
}

// Sends the signal to process.
func (h *pidfdHandle) Kill(signal unix.Signal) error {
	if h.pidfd > -1 {
		return pidfdSendSignal(h.pidfd, signal, nil, 0)
	}

	return h.normalHandle.Kill(signal)
}

// Returns true in case the process is still alive.
func (h *pidfdHandle) IsAlive() (bool, error) {
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
func (h *pidfdHandle) String() (string, error) {
	if len(h.normalHandle.pidData) != 0 {
		return h.normalHandle.pidData, nil
	}

	// Serialize the pidfd to string if possible.
	if h.pidfd > -1 {
		fh, _, err := nameToHandleAt(h.pidfd, "", unix.AT_EMPTY_PATH)
		if err != nil {
			// Do not fail if NameToHandleAt is not supported, we will
			// fallback to process start-time later.
			if err == unix.ENOTSUP {
				logrus.Debugf("NameToHandleAt(%d) failed: %v", h.pidfd, err)
			} else {
				return "", err
			}
		} else {
			hexStr := hex.EncodeToString(fh.Bytes())
			return nameToHandlePrefix + strconv.Itoa(int(fh.Type())) + " " + hexStr, nil
		}
	}

	// Fallback to default String().
	return h.normalHandle.String()
}
