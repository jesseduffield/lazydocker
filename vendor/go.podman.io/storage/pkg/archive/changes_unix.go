//go:build !windows

package archive

import (
	"os"
	"syscall"

	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/system"
	"golang.org/x/sys/unix"
)

func statDifferent(oldStat *system.StatT, oldInfo *FileInfo, newStat *system.StatT, newInfo *FileInfo) bool {
	// Don't look at size for dirs, its not a good measure of change
	oldUID, oldGID := oldStat.UID(), oldStat.GID()
	uid, gid := newStat.UID(), newStat.GID()
	if cuid, cgid, err := newInfo.idMappings.ToContainer(idtools.IDPair{UID: int(uid), GID: int(gid)}); err == nil {
		uid = uint32(cuid)
		gid = uint32(cgid)
		if oldInfo != nil {
			if oldcuid, oldcgid, err := oldInfo.idMappings.ToContainer(idtools.IDPair{UID: int(oldUID), GID: int(oldGID)}); err == nil {
				oldUID = uint32(oldcuid)
				oldGID = uint32(oldcgid)
			}
		}
	}
	ownerChanged := uid != oldUID || gid != oldGID
	if oldStat.Mode() != newStat.Mode() ||
		ownerChanged ||
		oldStat.Rdev() != newStat.Rdev() ||
		oldStat.Flags() != newStat.Flags() ||
		!sameFsTimeSpec(oldStat.Mtim(), newStat.Mtim()) ||
		// Don't look at size for dirs, its not a good measure of change
		((oldStat.Mode()&unix.S_IFDIR != unix.S_IFDIR) && (oldStat.Size() != newStat.Size())) {
		return true
	}
	return false
}

func (info *FileInfo) isDir() bool {
	return info.parent == nil || info.stat.Mode()&unix.S_IFDIR != 0
}

func getIno(fi os.FileInfo) uint64 {
	return fi.Sys().(*syscall.Stat_t).Ino
}

func hasHardlinks(fi os.FileInfo) bool {
	return fi.Sys().(*syscall.Stat_t).Nlink > 1
}
