package zfs

import (
	"fmt"

	"github.com/sirupsen/logrus"
	graphdriver "go.podman.io/storage/drivers"
	"golang.org/x/sys/unix"
)

func checkRootdirFs(rootDir string) error {
	fsMagic, err := graphdriver.GetFSMagic(rootDir)
	if err != nil {
		return err
	}
	backingFS := "unknown"
	if fsName, ok := graphdriver.FsNames[fsMagic]; ok {
		backingFS = fsName
	}

	if fsMagic != graphdriver.FsMagicZfs {
		logrus.WithField("root", rootDir).WithField("backingFS", backingFS).WithField("storage-driver", "zfs").Error("No zfs dataset found for root")
		return fmt.Errorf("no zfs dataset found for rootdir '%s': %w", rootDir, graphdriver.ErrPrerequisites)
	}

	return nil
}

func getMountpoint(id string) string {
	return id
}

func detachUnmount(mountpoint string) error {
	return unix.Unmount(mountpoint, unix.MNT_DETACH)
}
