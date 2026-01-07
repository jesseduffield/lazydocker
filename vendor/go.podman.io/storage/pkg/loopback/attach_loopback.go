//go:build linux

package loopback

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// Loopback related errors
var (
	ErrAttachLoopbackDevice   = errors.New("loopback attach failed")
	ErrGetLoopbackBackingFile = errors.New("unable to get loopback backing file")
	ErrSetCapacity            = errors.New("unable set loopback capacity")
)

func stringToLoopName(src string) [LoNameSize]uint8 {
	var dst [LoNameSize]uint8
	copy(dst[:], src[:])
	return dst
}

func getNextFreeLoopbackIndex() (int, error) {
	f, err := os.OpenFile("/dev/loop-control", os.O_RDONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	index, err := ioctlLoopCtlGetFree(f.Fd())
	if index < 0 {
		index = 0
	}
	return index, err
}

func openNextAvailableLoopback(sparseName string, sparseFile *os.File) (*os.File, error) {
	// Read information about the loopback file.
	var st syscall.Stat_t
	if err := syscall.Fstat(int(sparseFile.Fd()), &st); err != nil {
		logrus.Errorf("Reading information about loopback file %s: %v", sparseName, err)
		return nil, ErrAttachLoopbackDevice
	}

	// upper bound to avoid infinite loop
	remaining := 1000

	// Start looking for a free /dev/loop
	for {
		if remaining == 0 {
			logrus.Errorf("No free loopback devices available")
			return nil, ErrAttachLoopbackDevice
		}
		remaining--

		index, err := getNextFreeLoopbackIndex()
		if err != nil {
			logrus.Debugf("Error retrieving the next available loopback: %s", err)
			return nil, err
		}

		target := fmt.Sprintf("/dev/loop%d", index)

		// OpenFile adds O_CLOEXEC
		loopFile, err := os.OpenFile(target, os.O_RDWR, 0o644)
		if err != nil {
			// The kernel returns ENXIO when opening a device that is in the "deleting" or "rundown" state, so
			// just treat ENXIO as if the device does not exist.
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, unix.ENXIO) {
				// Another process could have taken the loopback device in the meantime.  So repeat
				// the process with the next loopback device.
				continue
			}
			logrus.Errorf("Opening loopback device: %s", err)
			return nil, ErrAttachLoopbackDevice
		}

		fi, err := loopFile.Stat()
		if err != nil {
			loopFile.Close()
			logrus.Errorf("Stat loopback device: %s", err)
			return nil, ErrAttachLoopbackDevice
		}
		if fi.Mode()&os.ModeDevice != os.ModeDevice {
			loopFile.Close()
			logrus.Errorf("Loopback device %s is not a block device.", target)
			continue
		}

		// Try to attach to the loop file
		if err := ioctlLoopSetFd(loopFile.Fd(), sparseFile.Fd()); err != nil {
			loopFile.Close()

			// If the error is EBUSY, then try the next loopback
			if err == syscall.EBUSY {
				continue
			}

			logrus.Errorf("Cannot set up loopback device %s: %s", target, err)
			return nil, ErrAttachLoopbackDevice
		}

		// Check if the loopback driver and underlying filesystem agree on the loopback file's
		// device and inode numbers.
		dev, ino, err := getLoopbackBackingFile(loopFile)
		if err != nil {
			logrus.Errorf("Getting loopback backing file: %s", err)
			return nil, ErrGetLoopbackBackingFile
		}
		if dev != uint64(st.Dev) || ino != st.Ino { //nolint:unconvert
			logrus.Errorf("Loopback device and filesystem disagree on device/inode for %q: %#x(%d):%#x(%d) vs %#x(%d):%#x(%d)", sparseName, dev, dev, ino, ino, st.Dev, st.Dev, st.Ino, st.Ino)
		}
		return loopFile, nil
	}
}

// AttachLoopDevice attaches the given sparse file to the next
// available loopback device. It returns an opened *os.File.
func AttachLoopDevice(sparseName string) (loop *os.File, err error) {
	return attachLoopDevice(sparseName, false)
}

// AttachLoopDeviceRO attaches the given sparse file opened read-only to
// the next available loopback device. It returns an opened *os.File.
func AttachLoopDeviceRO(sparseName string) (loop *os.File, err error) {
	return attachLoopDevice(sparseName, true)
}

func attachLoopDevice(sparseName string, readonly bool) (loop *os.File, err error) {
	var sparseFile *os.File

	// OpenFile adds O_CLOEXEC
	if readonly {
		sparseFile, err = os.OpenFile(sparseName, os.O_RDONLY, 0o644)
	} else {
		sparseFile, err = os.OpenFile(sparseName, os.O_RDWR, 0o644)
	}
	if err != nil {
		logrus.Errorf("Opening sparse file: %v", err)
		return nil, ErrAttachLoopbackDevice
	}
	defer sparseFile.Close()

	loopFile, err := openNextAvailableLoopback(sparseName, sparseFile)
	if err != nil {
		return nil, err
	}

	// Set the status of the loopback device
	loopInfo := &loopInfo64{
		loFileName: stringToLoopName(loopFile.Name()),
		loOffset:   0,
		loFlags:    LoFlagsAutoClear,
	}

	if err := ioctlLoopSetStatus64(loopFile.Fd(), loopInfo); err != nil {
		logrus.Errorf("Cannot set up loopback device info: %s", err)

		// If the call failed, then free the loopback device
		if err := ioctlLoopClrFd(loopFile.Fd()); err != nil {
			logrus.Error("While cleaning up the loopback device")
		}
		loopFile.Close()
		return nil, ErrAttachLoopbackDevice
	}

	return loopFile, nil
}
