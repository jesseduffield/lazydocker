//go:build linux

package loopback

import (
	"golang.org/x/sys/unix"
)

type loopInfo64 struct {
	loDevice         uint64 /* ioctl r/o */
	loInode          uint64 /* ioctl r/o */
	loRdevice        uint64 /* ioctl r/o */
	loOffset         uint64
	loSizelimit      uint64 /* bytes, 0 == max available */
	loNumber         uint32 /* ioctl r/o */
	loEncryptType    uint32
	loEncryptKeySize uint32 /* ioctl w/o */
	loFlags          uint32 /* ioctl r/o */
	loFileName       [LoNameSize]uint8
	loCryptName      [LoNameSize]uint8
	loEncryptKey     [LoKeySize]uint8 /* ioctl w/o */
	loInit           [2]uint64
}

// IOCTL consts
const (
	LoopSetFd       = unix.LOOP_SET_FD
	LoopCtlGetFree  = unix.LOOP_CTL_GET_FREE
	LoopGetStatus64 = unix.LOOP_GET_STATUS64
	LoopSetStatus64 = unix.LOOP_SET_STATUS64
	LoopClrFd       = unix.LOOP_CLR_FD
	LoopSetCapacity = unix.LOOP_SET_CAPACITY
)

// LOOP consts.
const (
	LoFlagsAutoClear = unix.LO_FLAGS_AUTOCLEAR
	LoFlagsReadOnly  = unix.LO_FLAGS_READ_ONLY
	LoFlagsPartScan  = unix.LO_FLAGS_PARTSCAN
	LoKeySize        = unix.LO_KEY_SIZE
	LoNameSize       = unix.LO_NAME_SIZE
)
