package archive

import (
	"os"

	"go.podman.io/storage/pkg/system"
)

func statDifferent(oldStat *system.StatT, oldInfo *FileInfo, newStat *system.StatT, newInfo *FileInfo) bool {
	// Don't look at size for dirs, its not a good measure of change
	if oldStat.Mtim() != newStat.Mtim() ||
		oldStat.Mode() != newStat.Mode() ||
		oldStat.Size() != newStat.Size() && !oldStat.Mode().IsDir() {
		return true
	}
	return false
}

func (info *FileInfo) isDir() bool {
	return info.parent == nil || info.stat.Mode().IsDir()
}

func getIno(fi os.FileInfo) uint64 {
	return 0
}

func hasHardlinks(fi os.FileInfo) bool {
	return false
}
