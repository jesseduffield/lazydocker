//go:build freebsd

package system

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// Flag values from <sys/stat.h>
const (
	/*
	 * Definitions of flags stored in file flags word.
	 *
	 * Super-user and owner changeable flags.
	 */
	UF_SETTABLE  uint32 = 0x0000ffff /* mask of owner changeable flags */
	UF_NODUMP    uint32 = 0x00000001 /* do not dump file */
	UF_IMMUTABLE uint32 = 0x00000002 /* file may not be changed */
	UF_APPEND    uint32 = 0x00000004 /* writes to file may only append */
	UF_OPAQUE    uint32 = 0x00000008 /* directory is opaque wrt. union */
	UF_NOUNLINK  uint32 = 0x00000010 /* file may not be removed or renamed */

	UF_SYSTEM   uint32 = 0x00000080 /* Windows system file bit */
	UF_SPARSE   uint32 = 0x00000100 /* sparse file */
	UF_OFFLINE  uint32 = 0x00000200 /* file is offline */
	UF_REPARSE  uint32 = 0x00000400 /* Windows reparse point file bit */
	UF_ARCHIVE  uint32 = 0x00000800 /* file needs to be archived */
	UF_READONLY uint32 = 0x00001000 /* Windows readonly file bit */
	/* This is the same as the MacOS X definition of UF_HIDDEN. */
	UF_HIDDEN uint32 = 0x00008000 /* file is hidden */

	/*
	 * Super-user changeable flags.
	 */
	SF_SETTABLE  uint32 = 0xffff0000 /* mask of superuser changeable flags */
	SF_ARCHIVED  uint32 = 0x00010000 /* file is archived */
	SF_IMMUTABLE uint32 = 0x00020000 /* file may not be changed */
	SF_APPEND    uint32 = 0x00040000 /* writes to file may only append */
	SF_NOUNLINK  uint32 = 0x00100000 /* file may not be removed or renamed */
	SF_SNAPSHOT  uint32 = 0x00200000 /* snapshot inode */
)

func Lchflags(path string, flags uint32) error {
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}
	_, _, e1 := unix.Syscall(unix.SYS_LCHFLAGS, uintptr(unsafe.Pointer(p)), uintptr(flags), 0)
	if e1 != 0 {
		return e1
	}
	return nil
}
