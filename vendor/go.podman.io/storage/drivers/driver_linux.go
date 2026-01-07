//go:build linux

package graphdriver

import (
	"path/filepath"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/mount"
	"golang.org/x/sys/unix"
)

const (
	// FsMagicAufs filesystem id for Aufs
	FsMagicAufs = FsMagic(0x61756673)
	// FsMagicBtrfs filesystem id for Btrfs
	FsMagicBtrfs = FsMagic(0x9123683E)
	// FsMagicCramfs filesystem id for Cramfs
	FsMagicCramfs = FsMagic(0x28cd3d45)
	// FsMagicEcryptfs filesystem id for eCryptfs
	FsMagicEcryptfs = FsMagic(0xf15f)
	// FsMagicExtfs filesystem id for Extfs
	FsMagicExtfs = FsMagic(0x0000EF53)
	// FsMagicF2fs filesystem id for F2fs
	FsMagicF2fs = FsMagic(0xF2F52010)
	// FsMagicGPFS filesystem id for GPFS
	FsMagicGPFS = FsMagic(0x47504653)
	// FsMagicJffs2Fs filesystem if for Jffs2Fs
	FsMagicJffs2Fs = FsMagic(0x000072b6)
	// FsMagicJfs filesystem id for Jfs
	FsMagicJfs = FsMagic(0x3153464a)
	// FsMagicNfsFs filesystem id for NfsFs
	FsMagicNfsFs = FsMagic(0x00006969)
	// FsMagicRAMFs filesystem id for RamFs
	FsMagicRAMFs = FsMagic(0x858458f6)
	// FsMagicReiserFs filesystem id for ReiserFs
	FsMagicReiserFs = FsMagic(0x52654973)
	// FsMagicSmbFs filesystem id for SmbFs
	FsMagicSmbFs = FsMagic(0x0000517B)
	// FsMagicSquashFs filesystem id for SquashFs
	FsMagicSquashFs = FsMagic(0x73717368)
	// FsMagicTmpFs filesystem id for TmpFs
	FsMagicTmpFs = FsMagic(0x01021994)
	// FsMagicVxFS filesystem id for VxFs
	FsMagicVxFS = FsMagic(0xa501fcf5)
	// FsMagicXfs filesystem id for Xfs
	FsMagicXfs = FsMagic(0x58465342)
	// FsMagicZfs filesystem id for Zfs
	FsMagicZfs = FsMagic(0x2fc12fc1)
	// FsMagicOverlay filesystem id for overlay
	FsMagicOverlay = FsMagic(0x794C7630)
	// FsMagicFUSE filesystem id for FUSE
	FsMagicFUSE = FsMagic(0x65735546)
	// FsMagicAcfs filesystem id for Acfs
	FsMagicAcfs = FsMagic(0x61636673)
	// FsMagicAfs filesystem id for Afs
	FsMagicAfs = FsMagic(0x5346414f)
	// FsMagicCephFs filesystem id for Ceph
	FsMagicCephFs = FsMagic(0x00C36400)
	// FsMagicCIFS filesystem id for CIFS
	FsMagicCIFS = FsMagic(0xFF534D42)
	// FsMagicEROFS filesystem id for EROFS
	FsMagicEROFS = FsMagic(0xE0F5E1E2)
	// FsMagicFHGFS filesystem id for FHGFS
	FsMagicFHGFSFs = FsMagic(0x19830326)
	// FsMagicIBRIX filesystem id for IBRIX
	FsMagicIBRIX = FsMagic(0x013111A8)
	// FsMagicKAFS filesystem id for KAFS
	FsMagicKAFS = FsMagic(0x6B414653)
	// FsMagicLUSTRE filesystem id for LUSTRE
	FsMagicLUSTRE = FsMagic(0x0BD00BD0)
	// FsMagicNCP filesystem id for NCP
	FsMagicNCP = FsMagic(0x564C)
	// FsMagicNFSD filesystem id for NFSD
	FsMagicNFSD = FsMagic(0x6E667364)
	// FsMagicOCFS2 filesystem id for OCFS2
	FsMagicOCFS2 = FsMagic(0x7461636F)
	// FsMagicPANFS filesystem id for PANFS
	FsMagicPANFS = FsMagic(0xAAD7AAEA)
	// FsMagicPRLFS filesystem id for PRLFS
	FsMagicPRLFS = FsMagic(0x7C7C6673)
	// FsMagicSMB2 filesystem id for SMB2
	FsMagicSMB2 = FsMagic(0xFE534D42)
	// FsMagicSNFS filesystem id for SNFS
	FsMagicSNFS = FsMagic(0xBEEFDEAD)
	// FsMagicVBOXSF filesystem id for VBOXSF
	FsMagicVBOXSF = FsMagic(0x786F4256)
	// FsMagicVXFS filesystem id for VXFS
	FsMagicVXFS = FsMagic(0xA501FCF5)
)

var (
	// Slice of drivers that should be used in an order
	Priority = []string{
		"overlay",
		"btrfs",
		"zfs",
		"vfs",
	}

	// FsNames maps filesystem id to name of the filesystem.
	FsNames = map[FsMagic]string{
		FsMagicAufs:        "aufs",
		FsMagicBtrfs:       "btrfs",
		FsMagicCramfs:      "cramfs",
		FsMagicEcryptfs:    "ecryptfs",
		FsMagicEROFS:       "erofs",
		FsMagicExtfs:       "extfs",
		FsMagicF2fs:        "f2fs",
		FsMagicGPFS:        "gpfs",
		FsMagicJffs2Fs:     "jffs2",
		FsMagicJfs:         "jfs",
		FsMagicNfsFs:       "nfs",
		FsMagicOverlay:     "overlayfs",
		FsMagicRAMFs:       "ramfs",
		FsMagicReiserFs:    "reiserfs",
		FsMagicSmbFs:       "smb",
		FsMagicSquashFs:    "squashfs",
		FsMagicTmpFs:       "tmpfs",
		FsMagicUnsupported: "unsupported",
		FsMagicVxFS:        "vxfs",
		FsMagicXfs:         "xfs",
		FsMagicZfs:         "zfs",
	}
)

// GetFSMagic returns the filesystem id given the path.
func GetFSMagic(rootpath string) (FsMagic, error) {
	var buf unix.Statfs_t
	path := filepath.Dir(rootpath)
	if err := unix.Statfs(path, &buf); err != nil {
		return 0, err
	}

	if _, ok := FsNames[FsMagic(buf.Type)]; !ok {
		logrus.Debugf("Unknown filesystem type %#x reported for %s", buf.Type, path)
	}
	return FsMagic(buf.Type), nil
}

// NewFsChecker returns a checker configured for the provided FsMagic
func NewFsChecker(t FsMagic) Checker {
	return &fsChecker{
		t: t,
	}
}

type fsChecker struct {
	t FsMagic
}

func (c *fsChecker) IsMounted(path string) bool {
	m, _ := Mounted(c.t, path)
	return m
}

// NewDefaultChecker returns a check that parses /proc/mountinfo to check
// if the specified path is mounted.
func NewDefaultChecker() Checker {
	return &defaultChecker{}
}

type defaultChecker struct{}

func (c *defaultChecker) IsMounted(path string) bool {
	m, _ := mount.Mounted(path)
	return m
}

// isMountPoint checks that the given path is a mount point
func isMountPoint(mountPath string) (bool, error) {
	// it is already the root
	if mountPath == "/" {
		return true, nil
	}

	var s1, s2 unix.Stat_t
	if err := unix.Stat(mountPath, &s1); err != nil {
		return true, err
	}
	if err := unix.Stat(filepath.Dir(mountPath), &s2); err != nil {
		return true, err
	}
	return s1.Dev != s2.Dev, nil
}

// Mounted checks if the given path is mounted as the fs type
func Mounted(fsType FsMagic, mountPath string) (bool, error) {
	var buf unix.Statfs_t

	if err := unix.Statfs(mountPath, &buf); err != nil {
		return false, err
	}
	if FsMagic(buf.Type) != fsType {
		return false, nil
	}
	return isMountPoint(mountPath)
}
