//go:build linux && !exclude_disk_quota && cgo

//
// projectquota.go - implements XFS project quota controls
// for setting quota limits on a newly created directory.
// It currently supports the legacy XFS specific ioctls.
//
// TODO: use generic quota control ioctl FS_IOC_FS{GET,SET}XATTR
//       for both xfs/ext4 for kernel version >= v4.5
//

package quota

/*
#include <stdlib.h>
#include <dirent.h>
#include <linux/fs.h>
#include <linux/quota.h>
#include <linux/dqblk_xfs.h>

#ifndef FS_XFLAG_PROJINHERIT
struct fsxattr {
	__u32		fsx_xflags;
	__u32		fsx_extsize;
	__u32		fsx_nextents;
	__u32		fsx_projid;
	unsigned char	fsx_pad[12];
};
#define FS_XFLAG_PROJINHERIT	0x00000200
#endif
#ifndef FS_IOC_FSGETXATTR
#define FS_IOC_FSGETXATTR		_IOR ('X', 31, struct fsxattr)
#endif
#ifndef FS_IOC_FSSETXATTR
#define FS_IOC_FSSETXATTR		_IOW ('X', 32, struct fsxattr)
#endif

#ifndef PRJQUOTA
#define PRJQUOTA	2
#endif
#ifndef FS_PROJ_QUOTA
#define FS_PROJ_QUOTA	2
#endif
#ifndef Q_XSETPQLIM
#define Q_XSETPQLIM QCMD(Q_XSETQLIM, PRJQUOTA)
#endif
#ifndef Q_XGETPQUOTA
#define Q_XGETPQUOTA QCMD(Q_XGETQUOTA, PRJQUOTA)
#endif
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/directory"
	"golang.org/x/sys/unix"
)

const projectIDsAllocatedPerQuotaHome = 10000

// Quota limit params - currently we only control blocks hard limit and inodes
type Quota struct {
	Size   uint64
	Inodes uint64
}

// Control - Context to be used by storage driver (e.g. overlay)
// who wants to apply project quotas to container dirs
type Control struct {
	backingFsBlockDev string
	nextProjectID     uint32
	quotas            *sync.Map
	basePath          string
}

// Attempt to generate a unigue projectid.  Multiple directories
// per file system can have quota and they need a group of unique
// ids. This function attempts to allocate at least projectIDsAllocatedPerQuotaHome(10000)
// unique projectids, based on the inode of the basepath.
func generateUniqueProjectID(path string) (uint32, error) {
	fileinfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := fileinfo.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("not a syscall.Stat_t %s", path)
	}
	projectID := projectIDsAllocatedPerQuotaHome + (stat.Ino*projectIDsAllocatedPerQuotaHome)%(math.MaxUint32-projectIDsAllocatedPerQuotaHome)
	return uint32(projectID), nil
}

// NewControl - initialize project quota support.
// Test to make sure that quota can be set on a test dir and find
// the first project id to be used for the next container create.
//
// Returns nil (and error) if project quota is not supported.
//
// First get the project id of the basePath directory.
// This test will fail if the backing fs is not xfs.
//
// xfs_quota tool can be used to assign a project id to the driver home directory, e.g.:
//    echo 100000:/var/lib/containers/storage/overlay >> /etc/projects
//    echo 200000:/var/lib/containers/storage/volumes >> /etc/projects
//    echo storage:100000 >> /etc/projid
//    echo volumes:200000 >> /etc/projid
//    xfs_quota -x -c 'project -s storage volumes' /<xfs mount point>
//
// In the example above, the storage directory project id will be used as a
// "start offset" and all containers will be assigned larger project ids
// (e.g. >= 100000). Then the volumes directory project id will be used as a
// "start offset" and all volumes will be assigned larger project ids
// (e.g. >= 200000).
// This is a way to prevent xfs_quota management from conflicting with
// containers/storage.

// Then try to create a test directory with the next project id and set a quota
// on it. If that works, continue to scan existing containers to map allocated
// project ids.
func NewControl(basePath string) (*Control, error) {
	//
	// Get project id of parent dir as minimal id to be used by driver
	//
	minProjectID, err := getProjectID(basePath)
	if err != nil {
		return nil, err
	}
	if minProjectID == 0 {
		// Indicates the storage was never initialized
		// Generate a unique range of Projectids for this basepath
		minProjectID, err = generateUniqueProjectID(basePath)
		if err != nil {
			return nil, err
		}

	}
	//
	// create backing filesystem device node
	//
	backingFsBlockDev, err := makeBackingFsDev(basePath)
	if err != nil {
		return nil, err
	}

	//
	// Test if filesystem supports project quotas by trying to set
	// a quota on the first available project id
	//
	quota := Quota{
		Size:   0,
		Inodes: 0,
	}

	q := Control{
		backingFsBlockDev: backingFsBlockDev,
		nextProjectID:     minProjectID + 1,
		quotas:            &sync.Map{},
		basePath:          basePath,
	}

	if err := q.setProjectQuota(minProjectID, quota); err != nil {
		return nil, err
	}

	// Clear inherit flag from top-level directory if necessary.
	if err := stripProjectInherit(basePath); err != nil {
		return nil, err
	}

	//
	// get first project id to be used for next container
	//
	err = q.findNextProjectID()
	if err != nil {
		return nil, err
	}

	logrus.Debugf("NewControl(%s): nextProjectID = %d", basePath, q.nextProjectID)
	return &q, nil
}

// SetQuota - assign a unique project id to directory and set the quota limits
// for that project id.
// targetPath must exist, must be a directory, and must be empty.
func (q *Control) SetQuota(targetPath string, quota Quota) error {
	var projectID uint32
	value, ok := q.quotas.Load(targetPath)
	if ok {
		projectID, ok = value.(uint32)
	}
	if !ok {
		projectID = q.nextProjectID

		// The directory we are setting an ID on must be empty, as
		// the ID will not be propagated to pre-existing subdirectories.
		dents, err := os.ReadDir(targetPath)
		if err != nil {
			return fmt.Errorf("reading directory %s: %w", targetPath, err)
		}
		if len(dents) > 0 {
			return fmt.Errorf("can only set project ID on empty directories, %s is not empty", targetPath)
		}

		//
		// assign project id to new container directory
		//
		err = setProjectID(targetPath, projectID)
		if err != nil {
			return err
		}

		q.quotas.Store(targetPath, projectID)
		q.nextProjectID++
	}

	//
	// set the quota limit for the container's project id
	//
	logrus.Debugf("SetQuota path=%s, size=%d, inodes=%d, projectID=%d", targetPath, quota.Size, quota.Inodes, projectID)
	return q.setProjectQuota(projectID, quota)
}

// ClearQuota removes the map entry in the quotas map for targetPath.
// It does so to prevent the map leaking entries as directories are deleted.
func (q *Control) ClearQuota(targetPath string) {
	q.quotas.Delete(targetPath)
}

// setProjectQuota - set the quota for project id on xfs block device
func (q *Control) setProjectQuota(projectID uint32, quota Quota) error {
	var d C.fs_disk_quota_t
	d.d_version = C.FS_DQUOT_VERSION
	d.d_id = C.__u32(projectID)
	d.d_flags = C.FS_PROJ_QUOTA

	if quota.Size > 0 {
		d.d_fieldmask = d.d_fieldmask | C.FS_DQ_BHARD | C.FS_DQ_BSOFT
		d.d_blk_hardlimit = C.__u64(quota.Size / 512)
		d.d_blk_softlimit = d.d_blk_hardlimit
	}
	if quota.Inodes > 0 {
		d.d_fieldmask = d.d_fieldmask | C.FS_DQ_IHARD | C.FS_DQ_ISOFT
		d.d_ino_hardlimit = C.__u64(quota.Inodes)
		d.d_ino_softlimit = d.d_ino_hardlimit
	}

	cs := C.CString(q.backingFsBlockDev)
	defer C.free(unsafe.Pointer(cs))

	runQuotactl := func() syscall.Errno {
		_, _, errno := unix.Syscall6(unix.SYS_QUOTACTL, C.Q_XSETPQLIM,
			uintptr(unsafe.Pointer(cs)), uintptr(d.d_id),
			uintptr(unsafe.Pointer(&d)), 0, 0)
		return errno
	}

	errno := runQuotactl()

	// If the backingFsBlockDev does not exist any more then try to recreate it.
	if errors.Is(errno, unix.ENOENT) {
		if _, err := makeBackingFsDev(q.basePath); err != nil {
			return fmt.Errorf(
				"failed to recreate missing backingFsBlockDev %s for projid %d: %w",
				q.backingFsBlockDev, projectID, err,
			)
		}

		if errno := runQuotactl(); errno != 0 {
			return fmt.Errorf("failed to set quota limit for projid %d on %s after backingFsBlockDev recreation: %w",
				projectID, q.backingFsBlockDev, errno)
		}

	} else if errno != 0 {
		return fmt.Errorf("failed to set quota limit for projid %d on %s: %w",
			projectID, q.backingFsBlockDev, errno)
	}

	return nil
}

// GetQuota - get the quota limits of a directory that was configured with SetQuota
func (q *Control) GetQuota(targetPath string, quota *Quota) error {
	d, err := q.fsDiskQuotaFromPath(targetPath)
	if err != nil {
		return err
	}
	quota.Size = uint64(d.d_blk_hardlimit) * 512
	quota.Inodes = uint64(d.d_ino_hardlimit)
	return nil
}

// GetDiskUsage - get the current disk usage of a directory that was configured with SetQuota
func (q *Control) GetDiskUsage(targetPath string, usage *directory.DiskUsage) error {
	d, err := q.fsDiskQuotaFromPath(targetPath)
	if err != nil {
		return err
	}
	usage.Size = int64(d.d_bcount) * 512
	usage.InodeCount = int64(d.d_icount)

	return nil
}

func (q *Control) fsDiskQuotaFromPath(targetPath string) (C.fs_disk_quota_t, error) {
	var d C.fs_disk_quota_t
	var projectID uint32
	value, ok := q.quotas.Load(targetPath)
	if ok {
		projectID, ok = value.(uint32)
	}
	if !ok {
		return d, fmt.Errorf("quota not found for path : %s", targetPath)
	}

	//
	// get the quota limit for the container's project id
	//
	cs := C.CString(q.backingFsBlockDev)
	defer C.free(unsafe.Pointer(cs))

	_, _, errno := unix.Syscall6(unix.SYS_QUOTACTL, C.Q_XGETPQUOTA,
		uintptr(unsafe.Pointer(cs)), uintptr(C.__u32(projectID)),
		uintptr(unsafe.Pointer(&d)), 0, 0)
	if errno != 0 {
		return d, fmt.Errorf("failed to get quota limit for projid %d on %s: %w",
			projectID, q.backingFsBlockDev, errno)
	}

	return d, nil
}

// getProjectID - get the project id of path on xfs
func getProjectID(targetPath string) (uint32, error) {
	dir, err := openDir(targetPath)
	if err != nil {
		return 0, err
	}
	defer closeDir(dir)

	var fsx C.struct_fsxattr
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.FS_IOC_FSGETXATTR,
		uintptr(unsafe.Pointer(&fsx)))
	if errno != 0 {
		return 0, fmt.Errorf("failed to get projid for %s: %w", targetPath, errno)
	}

	return uint32(fsx.fsx_projid), nil
}

// setProjectID - set the project id of path on xfs
func setProjectID(targetPath string, projectID uint32) error {
	dir, err := openDir(targetPath)
	if err != nil {
		return err
	}
	defer closeDir(dir)

	logrus.Debugf("Setting quota project ID %d on %s", projectID, targetPath)

	var fsx C.struct_fsxattr
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.FS_IOC_FSGETXATTR,
		uintptr(unsafe.Pointer(&fsx)))
	if errno != 0 {
		return fmt.Errorf("failed to get projid for %s: %w", targetPath, errno)
	}
	fsx.fsx_projid = C.__u32(projectID)
	fsx.fsx_xflags |= C.FS_XFLAG_PROJINHERIT
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.FS_IOC_FSSETXATTR,
		uintptr(unsafe.Pointer(&fsx)))
	if errno != 0 {
		return fmt.Errorf("failed to set projid for %s: %w", targetPath, errno)
	}

	return nil
}

// stripProjectInherit strips the project inherit flag from a directory.
// Used on the top-level directory to ensure project IDs are only inherited for
// files in directories we set quotas on - not the directories we want to set
// the quotas on, as that would make everything use the same project ID.
func stripProjectInherit(targetPath string) error {
	dir, err := openDir(targetPath)
	if err != nil {
		return err
	}
	defer closeDir(dir)

	var fsx C.struct_fsxattr
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.FS_IOC_FSGETXATTR,
		uintptr(unsafe.Pointer(&fsx)))
	if errno != 0 {
		return fmt.Errorf("failed to get xfs attrs for %s: %w", targetPath, errno)
	}
	if fsx.fsx_xflags&C.FS_XFLAG_PROJINHERIT != 0 {
		// Flag is set, need to clear it.
		logrus.Debugf("Clearing PROJINHERIT flag from directory %s", targetPath)
		fsx.fsx_xflags = fsx.fsx_xflags &^ C.FS_XFLAG_PROJINHERIT
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.FS_IOC_FSSETXATTR,
			uintptr(unsafe.Pointer(&fsx)))
		if errno != 0 {
			return fmt.Errorf("failed to clear PROJINHERIT for %s: %w", targetPath, errno)
		}
	}
	return nil
}

// findNextProjectID - find the next project id to be used for containers
// by scanning driver home directory to find used project ids
func (q *Control) findNextProjectID() error {
	files, err := os.ReadDir(q.basePath)
	if err != nil {
		return fmt.Errorf("read directory failed : %s", q.basePath)
	}
	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		path := filepath.Join(q.basePath, file.Name())
		projid, err := getProjectID(path)
		if err != nil {
			return err
		}
		if projid > 0 {
			q.quotas.Store(path, projid)
		}
		if q.nextProjectID <= projid {
			q.nextProjectID = projid + 1
		}
	}

	return nil
}

func free(p *C.char) {
	C.free(unsafe.Pointer(p))
}

func openDir(path string) (*C.DIR, error) {
	Cpath := C.CString(path)
	defer free(Cpath)

	dir, errno := C.opendir(Cpath)
	if dir == nil {
		return nil, fmt.Errorf("can't open dir %v: %w", Cpath, errno)
	}
	return dir, nil
}

func closeDir(dir *C.DIR) {
	if dir != nil {
		C.closedir(dir)
	}
}

func getDirFd(dir *C.DIR) uintptr {
	return uintptr(C.dirfd(dir))
}

// Get the backing block device of the driver home directory
// and create a block device node under the home directory
// to be used by quotactl commands
func makeBackingFsDev(home string) (string, error) {
	var stat unix.Stat_t
	if err := unix.Stat(home, &stat); err != nil {
		return "", err
	}

	backingFsBlockDev := path.Join(home, BackingFsBlockDeviceLink)
	backingFsBlockDevTmp := backingFsBlockDev + ".tmp"
	// Re-create just in case someone copied the home directory over to a new device
	if err := unix.Mknod(backingFsBlockDevTmp, unix.S_IFBLK|0o600, int(stat.Dev)); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("failed to mknod %s: %w", backingFsBlockDevTmp, err)
		}
		// On EEXIST, try again after unlinking any potential leftover.
		_ = unix.Unlink(backingFsBlockDevTmp)
		if err := unix.Mknod(backingFsBlockDevTmp, unix.S_IFBLK|0o600, int(stat.Dev)); err != nil {
			return "", fmt.Errorf("failed to mknod %s: %w", backingFsBlockDevTmp, err)
		}
	}
	if err := unix.Rename(backingFsBlockDevTmp, backingFsBlockDev); err != nil {
		return "", fmt.Errorf("failed to rename %s to %s: %w", backingFsBlockDevTmp, backingFsBlockDev, err)
	}

	return backingFsBlockDev, nil
}
