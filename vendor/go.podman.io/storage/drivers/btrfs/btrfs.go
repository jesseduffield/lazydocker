//go:build linux && cgo

package btrfs

/*
#include <stdlib.h>
#include <dirent.h>

// keep struct field name compatible with btrfs-progs < 6.1.
#define max_referenced max_rfer
#include <btrfs/ioctl.h>
#include <btrfs/ctree.h>

static void set_name_btrfs_ioctl_vol_args_v2(struct btrfs_ioctl_vol_args_v2* btrfs_struct, const char* value) {
    snprintf(btrfs_struct->name, BTRFS_SUBVOL_NAME_MAX, "%s", value);
}
*/
import "C"

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/docker/go-units"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	graphdriver "go.podman.io/storage/drivers"
	"go.podman.io/storage/internal/tempdir"
	"go.podman.io/storage/pkg/directory"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/parsers"
	"go.podman.io/storage/pkg/system"
	"golang.org/x/sys/unix"
)

const defaultPerms = os.FileMode(0o555)

func init() {
	graphdriver.MustRegister("btrfs", Init)
}

type btrfsOptions struct {
	minSpace uint64
	size     uint64
}

// Init returns a new BTRFS driver.
// An error is returned if BTRFS is not supported.
func Init(home string, options graphdriver.Options) (graphdriver.Driver, error) {
	fsMagic, err := graphdriver.GetFSMagic(home)
	if err != nil {
		return nil, err
	}

	if fsMagic != graphdriver.FsMagicBtrfs {
		return nil, fmt.Errorf("%q is not on a btrfs filesystem: %w", home, graphdriver.ErrPrerequisites)
	}

	if err := os.MkdirAll(filepath.Join(home, "subvolumes"), 0o700); err != nil {
		return nil, err
	}

	if err := mount.MakePrivate(home); err != nil {
		return nil, err
	}

	opt, userDiskQuota, err := parseOptions(options.DriverOptions)
	if err != nil {
		return nil, err
	}

	driver := &Driver{
		home:    home,
		options: opt,
	}

	if userDiskQuota {
		if err := driver.enableQuota(); err != nil {
			return nil, err
		}
	}

	return graphdriver.NewNaiveDiffDriver(driver, graphdriver.NewNaiveLayerIDMapUpdater(driver)), nil
}

func parseOptions(opt []string) (btrfsOptions, bool, error) {
	var options btrfsOptions
	userDiskQuota := false
	for _, option := range opt {
		key, val, err := parsers.ParseKeyValueOpt(option)
		if err != nil {
			return options, userDiskQuota, err
		}
		key = strings.ToLower(key)
		switch key {
		case "btrfs.min_space":
			minSpace, err := units.RAMInBytes(val)
			if err != nil {
				return options, userDiskQuota, err
			}
			userDiskQuota = true
			options.minSpace = uint64(minSpace)
		case "btrfs.mountopt":
			return options, userDiskQuota, fmt.Errorf("btrfs driver does not support mount options")
		default:
			return options, userDiskQuota, fmt.Errorf("unknown option %s (%q)", key, option)
		}
	}
	return options, userDiskQuota, nil
}

// Driver contains information about the filesystem mounted.
type Driver struct {
	// root of the file system
	home         string
	options      btrfsOptions
	quotaEnabled bool
	once         sync.Once
}

// String prints the name of the driver (btrfs).
func (d *Driver) String() string {
	return "btrfs"
}

// Status returns current driver information in a two dimensional string array.
// Output contains "Build Version" and "Library Version" of the btrfs libraries used.
// Version information can be used to check compatibility with your kernel.
func (d *Driver) Status() [][2]string {
	status := [][2]string{}
	if bv := btrfsBuildVersion(); bv != "-" {
		status = append(status, [2]string{"Build Version", bv})
	}
	if lv := btrfsLibVersion(); lv != -1 {
		status = append(status, [2]string{"Library Version", fmt.Sprintf("%d", lv)})
	}
	return status
}

// Metadata returns empty metadata for this driver.
func (d *Driver) Metadata(id string) (map[string]string, error) {
	return nil, nil //nolint: nilnil
}

// Cleanup unmounts the home directory.
func (d *Driver) Cleanup() error {
	return mount.Unmount(d.home)
}

func free(p *C.char) {
	C.free(unsafe.Pointer(p))
}

func openDir(path string) (*C.DIR, error) {
	Cpath := C.CString(path)
	defer free(Cpath)

	dir := C.opendir(Cpath)
	if dir == nil {
		return nil, fmt.Errorf("can't open dir %s", path)
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

func subvolCreate(path, name string) error {
	dir, err := openDir(path)
	if err != nil {
		return err
	}
	defer closeDir(dir)

	var args C.struct_btrfs_ioctl_vol_args
	for i, c := range []byte(name) {
		args.name[i] = C.char(c)
	}

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_SUBVOL_CREATE,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return fmt.Errorf("failed to create btrfs subvolume: %w", errno)
	}
	return nil
}

func subvolSnapshot(src, dest, name string) error {
	srcDir, err := openDir(src)
	if err != nil {
		return err
	}
	defer closeDir(srcDir)

	destDir, err := openDir(dest)
	if err != nil {
		return err
	}
	defer closeDir(destDir)

	var args C.struct_btrfs_ioctl_vol_args_v2
	args.fd = C.__s64(getDirFd(srcDir))

	cs := C.CString(name)
	C.set_name_btrfs_ioctl_vol_args_v2(&args, cs)
	C.free(unsafe.Pointer(cs))

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(destDir), C.BTRFS_IOC_SNAP_CREATE_V2,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return fmt.Errorf("failed to create btrfs snapshot: %w", errno)
	}
	return nil
}

func isSubvolume(p string) (bool, error) {
	var bufStat unix.Stat_t
	if err := unix.Lstat(p, &bufStat); err != nil {
		return false, err
	}

	// return true if it is a btrfs subvolume
	return bufStat.Ino == C.BTRFS_FIRST_FREE_OBJECTID, nil
}

func subvolDelete(dirpath, name string, quotaEnabled bool) error {
	dir, err := openDir(dirpath)
	if err != nil {
		return err
	}
	defer closeDir(dir)
	fullPath := path.Join(dirpath, name)

	var args C.struct_btrfs_ioctl_vol_args

	// walk the btrfs subvolumes
	walkSubvolumes := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p != fullPath {
				// missing most likely because the path was a subvolume that got removed in the previous iteration
				// since it's gone anyway, we don't care
				return nil
			}
			return fmt.Errorf("walking subvolumes: %w", err)
		}
		// we want to check children only so skip itself
		// it will be removed after the filepath walk anyways
		if d.IsDir() && p != fullPath {
			sv, err := isSubvolume(p)
			if err != nil {
				return fmt.Errorf("failed to test if %s is a btrfs subvolume: %w", p, err)
			}
			if sv {
				if err := subvolDelete(path.Dir(p), d.Name(), quotaEnabled); err != nil {
					return fmt.Errorf("failed to destroy btrfs child subvolume (%s) of parent (%s): %w", p, dirpath, err)
				}
			}
		}
		return nil
	}
	if err := filepath.WalkDir(path.Join(dirpath, name), walkSubvolumes); err != nil {
		return fmt.Errorf("recursively walking subvolumes for %s failed: %w", dirpath, err)
	}

	if quotaEnabled {
		if qgroupid, err := subvolLookupQgroup(fullPath); err == nil {
			var args C.struct_btrfs_ioctl_qgroup_create_args
			args.qgroupid = C.__u64(qgroupid)

			_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_QGROUP_CREATE,
				uintptr(unsafe.Pointer(&args)))
			if errno != 0 {
				logrus.Errorf("Failed to delete btrfs qgroup %v for %s: %v", qgroupid, fullPath, errno.Error())
			}
		} else {
			logrus.Errorf("Failed to lookup btrfs qgroup for %s: %v", fullPath, err.Error())
		}
	}

	// all subvolumes have been removed
	// now remove the one originally passed in
	for i, c := range []byte(name) {
		args.name[i] = C.char(c)
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_SNAP_DESTROY,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return fmt.Errorf("failed to destroy btrfs snapshot %s for %s: %w", dirpath, name, errno)
	}
	return nil
}

func (d *Driver) updateQuotaStatus() {
	d.once.Do(func() {
		if !d.quotaEnabled {
			// In case quotaEnabled is not set, check qgroup and update quotaEnabled as needed
			if err := qgroupStatus(d.home); err != nil {
				// quota is still not enabled
				return
			}
			d.quotaEnabled = true
		}
	})
}

func (d *Driver) enableQuota() error {
	d.updateQuotaStatus()

	if d.quotaEnabled {
		return nil
	}

	dir, err := openDir(d.home)
	if err != nil {
		return err
	}
	defer closeDir(dir)

	var args C.struct_btrfs_ioctl_quota_ctl_args
	args.cmd = C.BTRFS_QUOTA_CTL_ENABLE
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_QUOTA_CTL,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return fmt.Errorf("failed to enable btrfs quota for %s: %w", dir, errno)
	}

	d.quotaEnabled = true

	return nil
}

func (d *Driver) subvolRescanQuota() error {
	d.updateQuotaStatus()

	if !d.quotaEnabled {
		return nil
	}

	dir, err := openDir(d.home)
	if err != nil {
		return err
	}
	defer closeDir(dir)

	var args C.struct_btrfs_ioctl_quota_rescan_args
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_QUOTA_RESCAN_WAIT,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return fmt.Errorf("failed to rescan btrfs quota for %s: %w", dir, errno)
	}

	return nil
}

func subvolLimitQgroup(path string, size uint64) error {
	dir, err := openDir(path)
	if err != nil {
		return err
	}
	defer closeDir(dir)

	var args C.struct_btrfs_ioctl_qgroup_limit_args
	args.lim.max_rfer = C.__u64(size)
	args.lim.flags = C.BTRFS_QGROUP_LIMIT_MAX_RFER
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_QGROUP_LIMIT,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return fmt.Errorf("failed to limit qgroup for %s: %w", dir, errno)
	}

	return nil
}

// qgroupStatus performs a BTRFS_IOC_TREE_SEARCH on the root path
// with search key of BTRFS_QGROUP_STATUS_KEY.
// In case qgroup is enabled, the returned key type will match BTRFS_QGROUP_STATUS_KEY.
// For more details please see https://github.com/kdave/btrfs-progs/blob/v4.9/qgroup.c#L1035
func qgroupStatus(path string) error {
	dir, err := openDir(path)
	if err != nil {
		return err
	}
	defer closeDir(dir)

	var args C.struct_btrfs_ioctl_search_args
	args.key.tree_id = C.BTRFS_QUOTA_TREE_OBJECTID
	args.key.min_type = C.BTRFS_QGROUP_STATUS_KEY
	args.key.max_type = C.BTRFS_QGROUP_STATUS_KEY
	args.key.max_objectid = C.__u64(math.MaxUint64)
	args.key.max_offset = C.__u64(math.MaxUint64)
	args.key.max_transid = C.__u64(math.MaxUint64)
	args.key.nr_items = 4096

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_TREE_SEARCH,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return fmt.Errorf("failed to search qgroup for %s: %w", path, errno)
	}
	sh := (*C.struct_btrfs_ioctl_search_header)(unsafe.Pointer(&args.buf))
	if sh._type != C.BTRFS_QGROUP_STATUS_KEY {
		return fmt.Errorf("invalid qgroup search header type for %s: %v", path, sh._type)
	}
	return nil
}

func subvolLookupQgroup(path string) (uint64, error) {
	dir, err := openDir(path)
	if err != nil {
		return 0, err
	}
	defer closeDir(dir)

	var args C.struct_btrfs_ioctl_ino_lookup_args
	args.objectid = C.BTRFS_FIRST_FREE_OBJECTID

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, getDirFd(dir), C.BTRFS_IOC_INO_LOOKUP,
		uintptr(unsafe.Pointer(&args)))
	if errno != 0 {
		return 0, fmt.Errorf("failed to lookup qgroup for %s: %w", dir, errno)
	}
	if args.treeid == 0 {
		return 0, fmt.Errorf("invalid qgroup id for %s: 0", dir)
	}

	return uint64(args.treeid), nil
}

func (d *Driver) subvolumesDir() string {
	return path.Join(d.home, "subvolumes")
}

func (d *Driver) subvolumesDirID(id string) string {
	return path.Join(d.subvolumesDir(), id)
}

func (d *Driver) quotasDir() string {
	return path.Join(d.home, "quotas")
}

func (d *Driver) quotasDirID(id string) string {
	return path.Join(d.quotasDir(), id)
}

// CreateFromTemplate creates a layer with the same contents and parent as another layer.
func (d *Driver) CreateFromTemplate(id, template string, templateIDMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, opts *graphdriver.CreateOpts, readWrite bool) error {
	return d.Create(id, template, opts)
}

// CreateReadWrite creates a layer that is writable for use as a container
// file system.
func (d *Driver) CreateReadWrite(id, parent string, opts *graphdriver.CreateOpts) error {
	return d.Create(id, parent, opts)
}

// Create the filesystem with given id.
func (d *Driver) Create(id, parent string, opts *graphdriver.CreateOpts) error {
	quotas := d.quotasDir()
	subvolumes := d.subvolumesDir()
	if err := os.MkdirAll(subvolumes, 0o700); err != nil {
		return err
	}
	if parent == "" {
		if err := subvolCreate(subvolumes, id); err != nil {
			return err
		}
		if err := os.Chmod(path.Join(subvolumes, id), defaultPerms); err != nil {
			return err
		}
	} else {
		parentDir := d.subvolumesDirID(parent)
		st, err := os.Stat(parentDir)
		if err != nil {
			return err
		}
		if !st.IsDir() {
			return fmt.Errorf("%s: not a directory", parentDir)
		}
		if err := subvolSnapshot(parentDir, subvolumes, id); err != nil {
			return err
		}
	}

	var storageOpt map[string]string
	if opts != nil {
		storageOpt = opts.StorageOpt
	}

	if _, ok := storageOpt["size"]; ok {
		driver := &Driver{}
		if err := d.parseStorageOpt(storageOpt, driver); err != nil {
			return err
		}

		if err := d.setStorageSize(path.Join(subvolumes, id), driver); err != nil {
			return err
		}
		if err := os.MkdirAll(quotas, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path.Join(quotas, id), []byte(fmt.Sprint(driver.options.size)), 0o644); err != nil {
			return err
		}
	}

	mountLabel := ""
	if opts != nil {
		mountLabel = opts.MountLabel
	}

	return label.Relabel(path.Join(subvolumes, id), mountLabel, false)
}

// Parse btrfs storage options
func (d *Driver) parseStorageOpt(storageOpt map[string]string, driver *Driver) error {
	// Read size to change the subvolume disk quota per container
	for key, val := range storageOpt {
		key := strings.ToLower(key)
		switch key {
		case "size":
			size, err := units.RAMInBytes(val)
			if err != nil {
				return err
			}
			driver.options.size = uint64(size)
		default:
			return fmt.Errorf("unknown option %s (%q)", key, storageOpt)
		}
	}

	return nil
}

// Set btrfs storage size
func (d *Driver) setStorageSize(dir string, driver *Driver) error {
	if driver.options.size <= 0 {
		return fmt.Errorf("btrfs: invalid storage size: %s", units.HumanSize(float64(driver.options.size)))
	}
	if d.options.minSpace > 0 && driver.options.size < d.options.minSpace {
		return fmt.Errorf("btrfs: storage size cannot be less than %s", units.HumanSize(float64(d.options.minSpace)))
	}

	if err := d.enableQuota(); err != nil {
		return err
	}

	if err := subvolLimitQgroup(dir, driver.options.size); err != nil {
		return err
	}

	return nil
}

// Remove the filesystem with given id.
func (d *Driver) Remove(id string) error {
	dir := d.subvolumesDirID(id)
	if err := fileutils.Exists(dir); err != nil {
		return err
	}
	quotasDir := d.quotasDirID(id)
	if err := fileutils.Exists(quotasDir); err == nil {
		if err := os.Remove(quotasDir); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// Call updateQuotaStatus() to invoke status update
	d.updateQuotaStatus()

	if err := subvolDelete(d.subvolumesDir(), id, d.quotaEnabled); err != nil {
		if d.quotaEnabled {
			return err
		}
		// If quota is not enabled, fallback to rmdir syscall to delete subvolumes.
		// This would allow unprivileged user to delete their owned subvolumes
		// in kernel >= 4.18 without user_subvol_rm_alowed mount option.
	}
	if err := system.EnsureRemoveAll(dir); err != nil {
		return err
	}
	if err := d.subvolRescanQuota(); err != nil {
		return err
	}
	return nil
}

// Get the requested filesystem id.
func (d *Driver) Get(id string, options graphdriver.MountOpts) (string, error) {
	dir := d.subvolumesDirID(id)
	st, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	for _, opt := range options.Options {
		if opt == "ro" {
			// ignore "ro" option
			continue
		}
		return "", fmt.Errorf("btrfs driver does not support mount options")
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s: not a directory", dir)
	}

	if quota, err := os.ReadFile(d.quotasDirID(id)); err == nil {
		if size, err := strconv.ParseUint(string(quota), 10, 64); err == nil && size >= d.options.minSpace {
			if err := d.enableQuota(); err != nil {
				return "", err
			}
			if err := subvolLimitQgroup(dir, size); err != nil {
				return "", err
			}
		}
	}

	return dir, nil
}

// Put is not implemented for BTRFS as there is no cleanup required for the id.
func (d *Driver) Put(id string) error {
	// Get() creates no runtime resources (like e.g. mounts)
	// so this doesn't need to do anything.
	return nil
}

// ReadWriteDiskUsage returns the disk usage of the writable directory for the ID.
// For BTRFS, it queries the subvolumes path for this ID.
func (d *Driver) ReadWriteDiskUsage(id string) (*directory.DiskUsage, error) {
	return directory.Usage(d.subvolumesDirID(id))
}

// Exists checks if the id exists in the filesystem.
func (d *Driver) Exists(id string) bool {
	dir := d.subvolumesDirID(id)
	err := fileutils.Exists(dir)
	return err == nil
}

// List all of the layers known to the driver.
func (d *Driver) ListLayers() ([]string, error) {
	entries, err := os.ReadDir(d.subvolumesDir())
	if err != nil {
		return nil, err
	}
	results := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		results = append(results, entry.Name())
	}
	return results, nil
}

// AdditionalImageStores returns additional image stores supported by the driver
func (d *Driver) AdditionalImageStores() []string {
	return nil
}

// Dedup performs deduplication of the driver's storage.
func (d *Driver) Dedup(req graphdriver.DedupArgs) (graphdriver.DedupResult, error) {
	return graphdriver.DedupResult{}, nil
}

// DeferredRemove is not implemented.
// It calls Remove directly.
func (d *Driver) DeferredRemove(id string) (tempdir.CleanupTempDirFunc, error) {
	return nil, d.Remove(id)
}

// GetTempDirRootDirs is not implemented.
func (d *Driver) GetTempDirRootDirs() []string {
	return []string{}
}
