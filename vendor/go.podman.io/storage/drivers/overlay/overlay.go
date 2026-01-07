//go:build linux

package overlay

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	units "github.com/docker/go-units"
	digest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	graphdriver "go.podman.io/storage/drivers"
	"go.podman.io/storage/drivers/overlayutils"
	"go.podman.io/storage/drivers/quota"
	"go.podman.io/storage/internal/dedup"
	"go.podman.io/storage/internal/staging_lockfile"
	"go.podman.io/storage/internal/tempdir"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chrootarchive"
	"go.podman.io/storage/pkg/directory"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/fsutils"
	"go.podman.io/storage/pkg/idmap"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/parsers"
	"go.podman.io/storage/pkg/system"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

// untar defines the untar method
var untar = chrootarchive.UntarUncompressed

const (
	defaultPerms         = os.FileMode(0o555)
	selinuxLabelTest     = "system_u:object_r:container_file_t:s0"
	mountProgramFlagFile = ".has-mount-program"
)

// This backend uses the overlay union filesystem for containers
// with diff directories for each layer.

// This version of the overlay driver requires at least kernel
// 4.0.0 in order to support mounting multiple diff directories.

// Each container/image has at least a "diff" directory and "link" file.
// If there is also a "lower" file when there are diff layers
// below as well as "merged" and "work" directories. The "diff" directory
// has the upper layer of the overlay and is used to capture any
// changes to the layer. The "lower" file contains all the lower layer
// mounts separated by ":" and ordered from uppermost to lowermost
// layers. The overlay itself is mounted in the "merged" directory,
// and the "work" dir is needed for overlay to work.

// The "link" file for each layer contains a unique string for the layer.
// Under the "l" directory at the root there will be a symbolic link
// with that unique string pointing the "diff" directory for the layer.
// The symbolic links are used to reference lower layers in the "lower"
// file and on mount. The links are used to shorten the total length
// of a layer reference without requiring changes to the layer identifier
// or root directory. Mounts are always done relative to root and
// referencing the symbolic links in order to ensure the number of
// lower directories can fit in a single page for making the mount
// syscall. A hard upper limit of 500 lower layers is enforced to ensure
// that mounts do not fail due to length.

const (
	linkDir     = "l"
	stagingDir  = "staging"
	tempDirName = "tempdirs"
	lowerFile   = "lower"
	maxDepth    = 500

	stagingLockFile = "staging.lock"

	tocArtifact             = "toc"
	fsVerityDigestsArtifact = "fs-verity-digests"

	// idLength represents the number of random characters
	// which can be used to create the unique link identifier
	// for every layer. If this value is too long then the
	// page size limit for the mount command may be exceeded.
	// The idLength should be selected such that following equation
	// is true (512 is a buffer for label metadata, 128 is the
	// number of lowers we want to be able to use without having
	// to use bind mounts to get all the way to the kernel limit).
	// ((idLength + len(linkDir) + 1) * 128) <= (pageSize - 512)
	idLength = 26
)

type overlayOptions struct {
	imageStores       []string
	layerStores       []additionalLayerStore
	quota             quota.Quota
	mountProgram      string
	skipMountHome     bool
	mountOptions      string
	ignoreChownErrors bool
	forceMask         *os.FileMode
	useComposefs      bool
}

// Driver contains information about the home directory and the list of active mounts that are created using this driver.
type Driver struct {
	name             string
	home             string
	runhome          string
	imageStore       string
	ctr              *graphdriver.RefCounter
	quotaCtl         *quota.Control
	options          overlayOptions
	naiveDiff        graphdriver.DiffDriver
	supportsDType    bool
	supportsVolatile *bool
	supportsDataOnly *bool
	usingMetacopy    bool
	usingComposefs   bool

	stagingDirsLocksMutex sync.Mutex
	// stagingDirsLocks access is not thread safe, it is required that callers take
	// stagingDirsLocksMutex on each access to guard against concurrent map writes.
	stagingDirsLocks map[string]*staging_lockfile.StagingLockFile

	supportsIDMappedMounts *bool
}

type additionalLayerStore struct {
	// path is the directory where this store is available on the host.
	path string

	// withReference is true when the store contains image reference information (base64-encoded)
	// in its layer search path so the path to the diff will be
	//  <path>/base64(reference)/<layerdigest>/
	withReference bool
}

var (
	backingFs             = "<unknown>"
	projectQuotaSupported = false

	useNaiveDiffLock sync.Once
	useNaiveDiffOnly bool
)

func init() {
	graphdriver.MustRegister("overlay", Init)
	graphdriver.MustRegister("overlay2", Init)
}

func hasMetacopyOption(opts []string) bool {
	return slices.Contains(opts, "metacopy=on")
}

func getMountProgramFlagFile(path string) string {
	return filepath.Join(path, mountProgramFlagFile)
}

func checkSupportVolatile(home, runhome string) (bool, error) {
	const feature = "volatile"
	volatileCacheResult, _, err := cachedFeatureCheck(runhome, feature)
	var usingVolatile bool
	if err == nil {
		if volatileCacheResult {
			logrus.Debugf("Cached value indicated that volatile is being used")
		} else {
			logrus.Debugf("Cached value indicated that volatile is not being used")
		}
		usingVolatile = volatileCacheResult
	} else {
		usingVolatile, err = doesVolatile(home)
		if err == nil {
			if usingVolatile {
				logrus.Debugf("overlay: test mount indicated that volatile is being used")
			} else {
				logrus.Debugf("overlay: test mount indicated that volatile is not being used")
			}
			if err = cachedFeatureRecord(runhome, feature, usingVolatile, ""); err != nil {
				return false, fmt.Errorf("recording volatile-being-used status: %w", err)
			}
		} else {
			usingVolatile = false
		}
	}
	return usingVolatile, nil
}

// checkAndRecordIDMappedSupport checks and stores if the kernel supports mounting overlay on top of a
// idmapped lower layer.
func checkAndRecordIDMappedSupport(home, runhome string) (bool, error) {
	if os.Geteuid() != 0 {
		return false, nil
	}

	feature := "idmapped-lower-dir"
	overlayCacheResult, overlayCacheText, err := cachedFeatureCheck(runhome, feature)
	if err == nil {
		if overlayCacheResult {
			logrus.Debugf("Cached value indicated that idmapped mounts for overlay are supported")
			return true, nil
		}
		logrus.Debugf("Cached value indicated that idmapped mounts for overlay are not supported")
		return false, errors.New(overlayCacheText)
	}
	supportsIDMappedMounts, err := supportsIdmappedLowerLayers(home)
	if err2 := cachedFeatureRecord(runhome, feature, supportsIDMappedMounts, ""); err2 != nil {
		return false, fmt.Errorf("recording overlay idmapped mounts support status: %w", err2)
	}
	return supportsIDMappedMounts, err
}

func checkAndRecordOverlaySupport(home, runhome string) (bool, error) {
	var supportsDType bool

	if os.Geteuid() != 0 {
		return false, nil
	}

	feature := "overlay"
	overlayCacheResult, overlayCacheText, err := cachedFeatureCheck(runhome, feature)
	if err == nil {
		if overlayCacheResult {
			logrus.Debugf("Cached value indicated that overlay is supported")
		} else {
			logrus.Debugf("Cached value indicated that overlay is not supported")
		}
		supportsDType = overlayCacheResult
		if !supportsDType {
			return false, errors.New(overlayCacheText)
		}
	} else {
		supportsDType, err = supportsOverlay(home, 0, 0)
		if err != nil {
			os.Remove(filepath.Join(home, linkDir))
			os.Remove(home)
			patherr, ok := err.(*os.PathError)
			if ok && patherr.Err == syscall.ENOSPC {
				return false, err
			}
			err = fmt.Errorf("kernel does not support overlay fs: %w", err)
			if err2 := cachedFeatureRecord(runhome, feature, false, err.Error()); err2 != nil {
				return false, fmt.Errorf("recording overlay not being supported (%v): %w", err, err2)
			}
			return false, err
		}
		if err = cachedFeatureRecord(runhome, feature, supportsDType, ""); err != nil {
			return false, fmt.Errorf("recording overlay support status: %w", err)
		}
	}
	return supportsDType, nil
}

func (d *Driver) getSupportsVolatile() (bool, error) {
	if d.supportsVolatile != nil {
		return *d.supportsVolatile, nil
	}
	supportsVolatile, err := checkSupportVolatile(d.home, d.runhome)
	if err != nil {
		return false, err
	}
	d.supportsVolatile = &supportsVolatile
	return supportsVolatile, nil
}

func (d *Driver) getSupportsDataOnly() (bool, error) {
	if d.supportsDataOnly != nil {
		return *d.supportsDataOnly, nil
	}
	supportsDataOnly, err := supportsDataOnlyLayersCached(d.home, d.runhome)
	if err != nil {
		return false, err
	}
	d.supportsDataOnly = &supportsDataOnly
	return supportsDataOnly, nil
}

// isNetworkFileSystem checks if the specified file system is supported by native overlay
// as backing store when running in a user namespace.
func isNetworkFileSystem(fsMagic graphdriver.FsMagic) bool {
	switch fsMagic {
	// a bunch of network file systems...
	case graphdriver.FsMagicNfsFs, graphdriver.FsMagicSmbFs, graphdriver.FsMagicAcfs,
		graphdriver.FsMagicAfs, graphdriver.FsMagicCephFs, graphdriver.FsMagicCIFS,
		graphdriver.FsMagicGPFS, graphdriver.FsMagicIBRIX,
		graphdriver.FsMagicKAFS, graphdriver.FsMagicLUSTRE, graphdriver.FsMagicNCP,
		graphdriver.FsMagicNFSD, graphdriver.FsMagicOCFS2, graphdriver.FsMagicPANFS,
		graphdriver.FsMagicPRLFS, graphdriver.FsMagicSMB2, graphdriver.FsMagicSNFS,
		graphdriver.FsMagicVBOXSF, graphdriver.FsMagicVXFS:
		return true
	}
	return false
}

// Init returns the a native diff driver for overlay filesystem.
// If overlay filesystem is not supported on the host, a wrapped graphdriver.ErrNotSupported is returned as error.
// If an overlay filesystem is not supported over an existing filesystem then a wrapped graphdriver.ErrIncompatibleFS is returned.
func Init(home string, options graphdriver.Options) (graphdriver.Driver, error) {
	opts, err := parseOptions(options.DriverOptions)
	if err != nil {
		return nil, err
	}

	fsMagic, err := graphdriver.GetFSMagic(home)
	if err != nil {
		return nil, err
	}
	fsName, ok := graphdriver.FsNames[fsMagic]
	if !ok {
		fsName = "<unknown>"
	}
	backingFs = fsName

	runhome := filepath.Join(options.RunRoot, filepath.Base(home))

	// Create the driver home dir
	if err := os.MkdirAll(path.Join(home, linkDir), 0o755); err != nil {
		return nil, err
	}

	if options.ImageStore != "" {
		if err := idtools.MkdirAllAs(path.Join(options.ImageStore, linkDir), 0o755, 0, 0); err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(runhome, 0o700); err != nil {
		return nil, err
	}

	if opts.mountProgram == "" {
		if supported, err := SupportsNativeOverlay(home, runhome); err != nil {
			return nil, err
		} else if !supported {
			if path, err := exec.LookPath("fuse-overlayfs"); err == nil {
				opts.mountProgram = path
			}
		}
	}

	if opts.mountProgram != "" {
		if unshare.IsRootless() && isNetworkFileSystem(fsMagic) && opts.forceMask == nil {
			m := os.FileMode(0o700)
			opts.forceMask = &m
			logrus.Warnf("Network file system detected as backing store.  Enforcing overlay option `force_mask=\"%o\"`.  Add it to storage.conf to silence this warning", m)
		}

		if err := os.WriteFile(getMountProgramFlagFile(home), []byte("true"), 0o600); err != nil {
			return nil, err
		}
	} else {
		// check if they are running over btrfs, aufs, overlay, or ecryptfs
		switch fsMagic {
		case graphdriver.FsMagicAufs, graphdriver.FsMagicOverlay, graphdriver.FsMagicEcryptfs:
			return nil, fmt.Errorf("'overlay' is not supported over %s, a mount_program is required: %w", backingFs, graphdriver.ErrIncompatibleFS)
		}
		if unshare.IsRootless() && isNetworkFileSystem(fsMagic) {
			return nil, fmt.Errorf("a network file system with user namespaces is not supported.  Please use a mount_program: %w", graphdriver.ErrIncompatibleFS)
		}
	}

	if opts.useComposefs {
		if unshare.IsRootless() {
			return nil, fmt.Errorf("composefs is not supported in user namespaces")
		}
		if _, err := getComposeFsHelper(); err != nil {
			return nil, fmt.Errorf("composefs helper program not found: %w", err)
		}
	}

	var usingMetacopy bool
	var supportsDType bool
	var supportsVolatile *bool
	if opts.mountProgram != "" {
		supportsDType = true
		t := true
		supportsVolatile = &t
	} else {
		supportsDType, err = checkAndRecordOverlaySupport(home, runhome)
		if err != nil {
			return nil, err
		}
		feature := fmt.Sprintf("metacopy(%s)", opts.mountOptions)
		metacopyCacheResult, _, err := cachedFeatureCheck(runhome, feature)
		if err == nil {
			if metacopyCacheResult {
				logrus.Debugf("Cached value indicated that metacopy is being used")
			} else {
				logrus.Debugf("Cached value indicated that metacopy is not being used")
			}
			usingMetacopy = metacopyCacheResult
		} else {
			usingMetacopy, err = doesMetacopy(home, opts.mountOptions)
			if err == nil {
				if usingMetacopy {
					logrus.Debugf("overlay: test mount indicated that metacopy is being used")
				} else {
					logrus.Debugf("overlay: test mount indicated that metacopy is not being used")
				}
				if err = cachedFeatureRecord(runhome, feature, usingMetacopy, ""); err != nil {
					return nil, fmt.Errorf("recording metacopy-being-used status: %w", err)
				}
			} else {
				logrus.Infof("overlay: test mount did not indicate whether or not metacopy is being used: %v", err)
				return nil, err
			}
		}
	}

	if !opts.skipMountHome {
		if err := mount.MakePrivate(home); err != nil {
			return nil, fmt.Errorf("overlay: failed to make mount private: %w", err)
		}
	}

	fileSystemType := graphdriver.FsMagicOverlay
	if opts.mountProgram != "" {
		fileSystemType = graphdriver.FsMagicFUSE
	}

	d := &Driver{
		name:                  "overlay",
		home:                  home,
		imageStore:            options.ImageStore,
		runhome:               runhome,
		ctr:                   graphdriver.NewRefCounter(graphdriver.NewFsChecker(fileSystemType)),
		supportsDType:         supportsDType,
		usingMetacopy:         usingMetacopy,
		supportsVolatile:      supportsVolatile,
		usingComposefs:        opts.useComposefs,
		options:               *opts,
		stagingDirsLocksMutex: sync.Mutex{},
		stagingDirsLocks:      make(map[string]*staging_lockfile.StagingLockFile),
	}

	d.naiveDiff = graphdriver.NewNaiveDiffDriver(d, graphdriver.NewNaiveLayerIDMapUpdater(d))
	if backingFs == "xfs" {
		// Try to enable project quota support over xfs.
		if d.quotaCtl, err = quota.NewControl(home); err == nil {
			projectQuotaSupported = true
		} else if opts.quota.Size > 0 || opts.quota.Inodes > 0 {
			return nil, fmt.Errorf("storage options overlay.size and overlay.inodes not supported. Filesystem does not support Project Quota: %w", err)
		}
	} else if opts.quota.Size > 0 || opts.quota.Inodes > 0 {
		// if xfs is not the backing fs then error out if the storage-opt overlay.size is used.
		return nil, fmt.Errorf("storage option overlay.size and overlay.inodes only supported for backingFS XFS. Found %v", backingFs)
	}

	logrus.Debugf("backingFs=%s, projectQuotaSupported=%v, useNativeDiff=%v, usingMetacopy=%v", backingFs, projectQuotaSupported, !d.useNaiveDiff(), d.usingMetacopy)

	return d, nil
}

func parseOptions(options []string) (*overlayOptions, error) {
	o := &overlayOptions{}
	for _, option := range options {
		key, val, err := parsers.ParseKeyValueOpt(option)
		if err != nil {
			return nil, err
		}
		trimkey := strings.ToLower(key)
		trimkey = strings.TrimPrefix(trimkey, "overlay.")
		trimkey = strings.TrimPrefix(trimkey, "overlay2.")
		trimkey = strings.TrimPrefix(trimkey, ".")
		switch trimkey {
		case "override_kernel_check":
			logrus.Debugf("overlay: override_kernel_check option was specified, but is no longer necessary")
		case "mountopt":
			o.mountOptions = val
		case "size":
			logrus.Debugf("overlay: size=%s", val)
			size, err := units.RAMInBytes(val)
			if err != nil {
				return nil, err
			}
			o.quota.Size = uint64(size)
		case "inodes":
			logrus.Debugf("overlay: inodes=%s", val)
			inodes, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				return nil, err
			}
			o.quota.Inodes = inodes
		case "imagestore", "additionalimagestore":
			logrus.Debugf("overlay: imagestore=%s", val)
			// Additional read only image stores to use for lower paths
			if val == "" {
				continue
			}
			for store := range strings.SplitSeq(val, ",") {
				store = filepath.Clean(store)
				if !filepath.IsAbs(store) {
					return nil, fmt.Errorf("overlay: image path %q is not absolute.  Can not be relative", store)
				}
				st, err := os.Stat(store)
				if err != nil {
					return nil, fmt.Errorf("overlay: can't stat imageStore dir %s: %w", store, err)
				}
				if !st.IsDir() {
					return nil, fmt.Errorf("overlay: image path %q must be a directory", store)
				}
				o.imageStores = append(o.imageStores, store)
			}
		case "additionallayerstore":
			logrus.Debugf("overlay: additionallayerstore=%s", val)
			// Additional read only layer stores to use for lower paths
			if val == "" {
				continue
			}
			for lstore := range strings.SplitSeq(val, ",") {
				elems := strings.Split(lstore, ":")
				lstore = filepath.Clean(elems[0])
				if !filepath.IsAbs(lstore) {
					return nil, fmt.Errorf("overlay: additionallayerstore path %q is not absolute.  Can not be relative", lstore)
				}
				st, err := os.Stat(lstore)
				if err != nil {
					return nil, fmt.Errorf("overlay: can't stat additionallayerstore dir: %w", err)
				}
				if !st.IsDir() {
					return nil, fmt.Errorf("overlay: additionallayerstore path %q must be a directory", lstore)
				}
				var withReference bool
				for _, e := range elems[1:] {
					switch e {
					case "ref":
						if withReference {
							return nil, fmt.Errorf("overlay: additionallayerstore config of %q contains %q option twice", lstore, e)
						}
						withReference = true
					default:
						return nil, fmt.Errorf("overlay: additionallayerstore config %q contains unknown option %q", lstore, e)
					}
				}
				o.layerStores = append(o.layerStores, additionalLayerStore{
					path:          lstore,
					withReference: withReference,
				})
			}
		case "use_composefs":
			logrus.Debugf("overlay: use_composefs=%s", val)
			o.useComposefs, err = strconv.ParseBool(val)
			if err != nil {
				return nil, err
			}
		case "mount_program":
			logrus.Debugf("overlay: mount_program=%s", val)
			if val != "" {
				err := fileutils.Exists(val)
				if err != nil {
					return nil, fmt.Errorf("overlay: can't stat program %q: %w", val, err)
				}
			}
			o.mountProgram = val
		case "skip_mount_home":
			logrus.Debugf("overlay: skip_mount_home=%s", val)
			o.skipMountHome, err = strconv.ParseBool(val)
			if err != nil {
				return nil, err
			}
		case "ignore_chown_errors":
			logrus.Debugf("overlay: ignore_chown_errors=%s", val)
			o.ignoreChownErrors, err = strconv.ParseBool(val)
			if err != nil {
				return nil, err
			}
		case "force_mask":
			logrus.Debugf("overlay: force_mask=%s", val)
			var mask int64
			switch val {
			case "shared":
				mask = 0o755
			case "private":
				mask = 0o700
			default:
				mask, err = strconv.ParseInt(val, 8, 32)
				if err != nil {
					return nil, err
				}
			}
			m := os.FileMode(mask)
			o.forceMask = &m
		default:
			return nil, fmt.Errorf("overlay: unknown option %s", key)
		}
	}
	return o, nil
}

func cachedFeatureSet(feature string, set bool) string {
	if set {
		return fmt.Sprintf("%s-true", feature)
	}
	return fmt.Sprintf("%s-false", feature)
}

func cachedFeatureCheck(runhome, feature string) (supported bool, text string, err error) {
	content, err := os.ReadFile(filepath.Join(runhome, cachedFeatureSet(feature, true)))
	if err == nil {
		return true, string(content), nil
	}
	content, err = os.ReadFile(filepath.Join(runhome, cachedFeatureSet(feature, false)))
	if err == nil {
		return false, string(content), nil
	}
	return false, "", err
}

func cachedFeatureRecord(runhome, feature string, supported bool, text string) (err error) {
	f, err := os.Create(filepath.Join(runhome, cachedFeatureSet(feature, supported)))
	if f != nil {
		if text != "" {
			fmt.Fprintf(f, "%s", text)
		}
		f.Close()
	}
	return err
}

func SupportsNativeOverlay(home, runhome string) (bool, error) {
	if os.Geteuid() != 0 || home == "" || runhome == "" {
		return false, nil
	}

	var contents string
	flagContent, err := os.ReadFile(getMountProgramFlagFile(home))
	if err == nil {
		contents = strings.TrimSpace(string(flagContent))
	}
	switch contents {
	case "true":
		logrus.Debugf("overlay: storage already configured with a mount-program")
		return false, nil
	case "false":
		// Do nothing.
	default:
		needsMountProgram, err := scanForMountProgramIndicators(home)
		if err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if err := os.WriteFile(getMountProgramFlagFile(home), []byte(fmt.Sprintf("%t", needsMountProgram)), 0o600); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if needsMountProgram {
			return false, nil
		}
		// fall through to check if we find ourselves needing to use a
		// mount program now
	}

	for _, dir := range []string{home, runhome} {
		if err := fileutils.Exists(dir); err != nil {
			_ = idtools.MkdirAllAs(dir, 0o700, 0, 0)
		}
	}

	supportsDType, _ := checkAndRecordOverlaySupport(home, runhome)
	return supportsDType, nil
}

func supportsOverlay(home string, rootUID, rootGID int) (supportsDType bool, err error) {
	selinuxLabelTest := selinux.PrivContainerMountLabel()

	logLevel := logrus.ErrorLevel
	if unshare.IsRootless() {
		logLevel = logrus.DebugLevel
	}

	layerDir, err := os.MkdirTemp(home, "compat")
	if err != nil {
		patherr, ok := err.(*os.PathError)
		if ok && patherr.Err == syscall.ENOSPC {
			return false, err
		}
	}
	if err == nil {
		// Check if reading the directory's contents populates the d_type field, which is required
		// for proper operation of the overlay filesystem.
		supportsDType, err = fsutils.SupportsDType(layerDir)
		if err != nil {
			return false, err
		}
		if !supportsDType {
			return false, overlayutils.ErrDTypeNotSupported("overlay", backingFs)
		}

		// Try a test mount in the specific location we're looking at using.
		mergedDir := filepath.Join(layerDir, "merged")
		mergedSubdir := filepath.Join(mergedDir, "subdir")
		lower1Dir := filepath.Join(layerDir, "lower1")
		lower2Dir := filepath.Join(layerDir, "lower2")
		lower2Subdir := filepath.Join(lower2Dir, "subdir")
		lower2SubdirFile := filepath.Join(lower2Subdir, "file")
		upperDir := filepath.Join(layerDir, "upper")
		workDir := filepath.Join(layerDir, "work")
		defer func() {
			// Permitted to fail, since the various subdirectories
			// can be empty or not even there, and the home might
			// legitimately be not empty
			_ = unix.Unmount(mergedDir, unix.MNT_DETACH)
			_ = os.RemoveAll(layerDir)
			_ = os.Remove(home)
		}()
		_ = idtools.MkdirAs(mergedDir, 0o700, rootUID, rootGID)
		_ = idtools.MkdirAs(lower1Dir, 0o700, rootUID, rootGID)
		_ = idtools.MkdirAs(lower2Dir, 0o700, rootUID, rootGID)
		_ = idtools.MkdirAs(lower2Subdir, 0o700, rootUID, rootGID)
		_ = idtools.MkdirAs(upperDir, 0o700, rootUID, rootGID)
		_ = idtools.MkdirAs(workDir, 0o700, rootUID, rootGID)
		f, err := os.Create(lower2SubdirFile)
		if err != nil {
			logrus.Debugf("Unable to create test file: %v", err)
			return supportsDType, fmt.Errorf("unable to create test file: %w", err)
		}
		f.Close()
		flags := fmt.Sprintf("lowerdir=%s:%s,upperdir=%s,workdir=%s", lower1Dir, lower2Dir, upperDir, workDir)
		if selinux.GetEnabled() &&
			selinux.SecurityCheckContext(selinuxLabelTest) == nil {
			// Linux 5.11 introduced unprivileged overlay mounts but it has an issue
			// when used together with selinux labels.
			// Check that overlay supports selinux labels as well.
			flags = label.FormatMountLabel(flags, selinuxLabelTest)
		}
		if unshare.IsRootless() {
			flags = fmt.Sprintf("%s,userxattr", flags)
		}
		if err := syscall.Mknod(filepath.Join(upperDir, "whiteout"), syscall.S_IFCHR|0o600, int(unix.Mkdev(0, 0))); err != nil {
			logrus.Debugf("Unable to create kernel-style whiteout: %v", err)
			return supportsDType, fmt.Errorf("unable to create kernel-style whiteout: %w", err)
		}

		if len(flags) < unix.Getpagesize() {
			err := unix.Mount("overlay", mergedDir, "overlay", 0, flags)
			if err == nil {
				if err = os.RemoveAll(mergedSubdir); err != nil {
					logrus.StandardLogger().Logf(logLevel, "overlay: removing an item from the merged directory failed: %v", err)
					return supportsDType, fmt.Errorf("kernel returned %v when we tried to delete an item in the merged directory: %w", err, graphdriver.ErrNotSupported)
				}
				logrus.Debugf("overlay: test mount with multiple lowers succeeded")
				return supportsDType, nil
			}
			logrus.Debugf("overlay: test mount with multiple lowers failed: %v", err)
		}
		flags = fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lower1Dir, upperDir, workDir)
		if selinux.GetEnabled() {
			flags = label.FormatMountLabel(flags, selinuxLabelTest)
		}
		if len(flags) < unix.Getpagesize() {
			err := unix.Mount("overlay", mergedDir, "overlay", 0, flags)
			if err == nil {
				logrus.StandardLogger().Logf(logLevel, "overlay: test mount with multiple lowers failed, but succeeded with a single lower")
				return supportsDType, fmt.Errorf("kernel too old to provide multiple lowers feature for overlay: %w", graphdriver.ErrNotSupported)
			}
			logrus.Debugf("overlay: test mount with a single lower failed: %v", err)
		}
		logrus.StandardLogger().Logf(logLevel, "'overlay' is not supported over %s at %q", backingFs, home)
		return supportsDType, fmt.Errorf("'overlay' is not supported over %s at %q: %w", backingFs, home, graphdriver.ErrIncompatibleFS)
	}

	logrus.StandardLogger().Logf(logLevel, "'overlay' not found as a supported filesystem on this host. Please ensure kernel is new enough and has overlay support loaded.")
	return supportsDType, fmt.Errorf("'overlay' not found as a supported filesystem on this host. Please ensure kernel is new enough and has overlay support loaded.: %w", graphdriver.ErrNotSupported)
}

func (d *Driver) useNaiveDiff() bool {
	if d.usingComposefs {
		return true
	}

	useNaiveDiffLock.Do(func() {
		if d.options.mountProgram != "" {
			useNaiveDiffOnly = true
			return
		}
		feature := fmt.Sprintf("native-diff(%s)", d.options.mountOptions)
		nativeDiffCacheResult, nativeDiffCacheText, err := cachedFeatureCheck(d.runhome, feature)
		if err == nil {
			if nativeDiffCacheResult {
				logrus.Debugf("Cached value indicated that native-diff is usable")
			} else {
				logrus.Debugf("Cached value indicated that native-diff is not being used")
				logrus.Info(nativeDiffCacheText)
			}
			useNaiveDiffOnly = !nativeDiffCacheResult
			return
		}
		if err := doesSupportNativeDiff(d.home, d.options.mountOptions); err != nil {
			nativeDiffCacheText = fmt.Sprintf("Not using native diff for overlay, this may cause degraded performance for building images: %v", err)
			logrus.Info(nativeDiffCacheText)
			useNaiveDiffOnly = true
		}
		if err := cachedFeatureRecord(d.runhome, feature, !useNaiveDiffOnly, nativeDiffCacheText); err != nil {
			logrus.Warnf("Recording overlay native-diff support status: %v", err)
		}
	})
	return useNaiveDiffOnly
}

func (d *Driver) String() string {
	return d.name
}

// Status returns current driver information in a two dimensional string array.
// Output contains "Backing Filesystem" used in this implementation.
func (d *Driver) Status() [][2]string {
	supportsVolatile, err := d.getSupportsVolatile()
	if err != nil {
		supportsVolatile = false
	}
	return [][2]string{
		{"Backing Filesystem", backingFs},
		{"Supports d_type", strconv.FormatBool(d.supportsDType)},
		{"Native Overlay Diff", strconv.FormatBool(!d.useNaiveDiff())},
		{"Using metacopy", strconv.FormatBool(d.usingMetacopy)},
		{"Supports shifting", strconv.FormatBool(d.SupportsShifting(nil, nil))},
		{"Supports volatile", strconv.FormatBool(supportsVolatile)},
	}
}

// Metadata returns meta data about the overlay driver such as
// LowerDir, UpperDir, WorkDir and MergeDir used to store data.
func (d *Driver) Metadata(id string) (map[string]string, error) {
	dir, _, inAdditionalStore := d.dir2(id, false)
	if err := fileutils.Exists(dir); err != nil {
		return nil, err
	}

	metadata := map[string]string{
		"WorkDir":   path.Join(dir, "work"),
		"MergedDir": d.getMergedDir(id, dir, inAdditionalStore),
		"UpperDir":  path.Join(dir, "diff"),
	}

	lowerDirs, err := d.getLowerDirs(id)
	if err != nil {
		return nil, err
	}
	if len(lowerDirs) > 0 {
		metadata["LowerDir"] = strings.Join(lowerDirs, ":")
	}

	return metadata, nil
}

// Cleanup any state created by overlay which should be cleaned when
// the storage is being shutdown.  The only state created by the driver
// is the bind mount on the home directory.
func (d *Driver) Cleanup() error {
	anyPresent := d.pruneStagingDirectories()
	if anyPresent {
		return nil
	}
	return mount.Unmount(d.home)
}

// pruneStagingDirectories cleans up any staging directory that was leaked.
// It returns whether any staging directory is still present.
func (d *Driver) pruneStagingDirectories() bool {
	d.stagingDirsLocksMutex.Lock()
	for _, lock := range d.stagingDirsLocks {
		if err := lock.UnlockAndDelete(); err != nil {
			logrus.Warnf("Failed to unlock and delete staging lock file: %v", err)
		}
	}
	clear(d.stagingDirsLocks)
	d.stagingDirsLocksMutex.Unlock()

	anyPresent := false

	stagingDirBase := filepath.Join(d.homeDirForImageStore(), stagingDir)
	dirs, err := os.ReadDir(stagingDirBase)
	if err == nil {
		for _, dir := range dirs {
			stagingDirToRemove := filepath.Join(stagingDirBase, dir.Name())
			lock, err := staging_lockfile.TryLockPath(filepath.Join(stagingDirToRemove, stagingLockFile))
			if err != nil {
				anyPresent = true
				continue
			}
			_ = os.RemoveAll(stagingDirToRemove)
			if err := lock.UnlockAndDelete(); err != nil {
				logrus.Warnf("Failed to unlock and delete staging lock file: %v", err)
			}
		}
	}
	return anyPresent
}

// LookupAdditionalLayer looks up additional layer store by the specified
// TOC digest and ref and returns an object representing that layer.
// This API is experimental and can be changed without bumping the major version number.
// TODO: to remove the comment once it's no longer experimental.
func (d *Driver) LookupAdditionalLayer(tocDigest digest.Digest, ref string) (graphdriver.AdditionalLayer, error) {
	l, err := d.getAdditionalLayerPath(tocDigest, ref)
	if err != nil {
		return nil, err
	}
	// Tell the additional layer store that we use this layer.
	// This will increase reference counter on the store's side.
	// This will be decreased on Release() method.
	notifyUseAdditionalLayer(l)
	return &additionalLayer{
		path: l,
		d:    d,
	}, nil
}

// LookupAdditionalLayerByID looks up additional layer store by the specified
// ID and returns an object representing that layer.
// This API is experimental and can be changed without bumping the major version number.
// TODO: to remove the comment once it's no longer experimental.
func (d *Driver) LookupAdditionalLayerByID(id string) (graphdriver.AdditionalLayer, error) {
	l, err := d.getAdditionalLayerPathByID(id)
	if err != nil {
		return nil, err
	}
	// Tell the additional layer store that we use this layer.
	// This will increase reference counter on the store's side.
	// This will be decreased on Release() method.
	notifyUseAdditionalLayer(l)
	return &additionalLayer{
		path: l,
		d:    d,
	}, nil
}

// CreateFromTemplate creates a layer with the same contents and parent as another layer.
func (d *Driver) CreateFromTemplate(id, template string, templateIDMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, opts *graphdriver.CreateOpts, readWrite bool) error {
	if readWrite {
		return d.CreateReadWrite(id, template, opts)
	}
	return d.Create(id, template, opts)
}

// CreateReadWrite creates a layer that is writable for use as a container
// file system.
func (d *Driver) CreateReadWrite(id, parent string, opts *graphdriver.CreateOpts) error {
	if opts != nil && len(opts.StorageOpt) != 0 && !projectQuotaSupported {
		return fmt.Errorf("--storage-opt is supported only for overlay over xfs with 'pquota' mount option")
	}

	if opts == nil {
		opts = &graphdriver.CreateOpts{
			StorageOpt: map[string]string{},
		}
	}

	if d.options.forceMask != nil && d.options.mountProgram == "" {
		return fmt.Errorf("overlay: force_mask option for writeable layers is only supported with a mount_program")
	}

	if _, ok := opts.StorageOpt["size"]; !ok {
		if opts.StorageOpt == nil {
			opts.StorageOpt = map[string]string{}
		}
		opts.StorageOpt["size"] = strconv.FormatUint(d.options.quota.Size, 10)
	}

	if _, ok := opts.StorageOpt["inodes"]; !ok {
		if opts.StorageOpt == nil {
			opts.StorageOpt = map[string]string{}
		}
		opts.StorageOpt["inodes"] = strconv.FormatUint(d.options.quota.Inodes, 10)
	}

	return d.create(id, parent, opts, false)
}

// Create is used to create the upper, lower, and merge directories required for overlay fs for a given id.
// The parent filesystem is used to configure these directories for the overlay.
func (d *Driver) Create(id, parent string, opts *graphdriver.CreateOpts) (retErr error) {
	if opts != nil && len(opts.StorageOpt) != 0 {
		if _, ok := opts.StorageOpt["size"]; ok {
			return fmt.Errorf("--storage-opt size is only supported for ReadWrite Layers")
		}

		if _, ok := opts.StorageOpt["inodes"]; ok {
			return fmt.Errorf("--storage-opt inodes is only supported for ReadWrite Layers")
		}
	}

	return d.create(id, parent, opts, true)
}

func (d *Driver) create(id, parent string, opts *graphdriver.CreateOpts, readOnly bool) (retErr error) {
	dir, homedir, _ := d.dir2(id, readOnly)

	disableQuota := readOnly

	var uidMaps []idtools.IDMap
	var gidMaps []idtools.IDMap

	if opts != nil && opts.IDMappings != nil {
		uidMaps = opts.IDMappings.UIDs()
		gidMaps = opts.IDMappings.GIDs()
	}

	// Make the link directory if it does not exist
	if err := idtools.MkdirAllAs(path.Join(homedir, linkDir), 0o755, 0, 0); err != nil {
		return err
	}

	rootUID, rootGID, err := idtools.GetRootUIDGID(uidMaps, gidMaps)
	if err != nil {
		return err
	}

	idPair := idtools.IDPair{
		UID: rootUID,
		GID: rootGID,
	}

	if err := idtools.MkdirAllAndChownNew(path.Dir(dir), 0o755, idPair); err != nil {
		return err
	}

	st := idtools.Stat{IDs: idPair, Mode: defaultPerms}

	if parent != "" {
		parentBase := d.dir(parent)
		parentDiff := filepath.Join(parentBase, "diff")
		if xSt, err := idtools.GetContainersOverrideXattr(parentDiff); err == nil {
			st = xSt
		} else {
			systemSt, err := system.Stat(parentDiff)
			if err != nil {
				return err
			}
			st.IDs.UID = int(systemSt.UID())
			st.IDs.GID = int(systemSt.GID())
			st.Mode = os.FileMode(systemSt.Mode())
		}
	}

	if err := fileutils.Lexists(dir); err == nil {
		logrus.Warnf("Trying to create a layer %#v while directory %q already exists; removing it first", id, dir)
		// Don’t just os.RemoveAll(dir) here; d.Remove also removes the link in linkDir,
		// so that we can’t end up with two symlinks in linkDir pointing to the same layer.
		if err := d.Remove(id); err != nil {
			return fmt.Errorf("removing a pre-existing layer directory %q: %w", dir, err)
		}
	}

	if err := idtools.MkdirAllAndChownNew(dir, 0o700, idPair); err != nil {
		return err
	}

	defer func() {
		// Clean up on failure
		if retErr != nil {
			if err2 := os.RemoveAll(dir); err2 != nil {
				logrus.Errorf("While recovering from a failure creating a layer, error deleting %#v: %v", dir, err2)
			}
		}
	}()

	if d.quotaCtl != nil && !disableQuota {
		quota := quota.Quota{}
		if opts != nil && len(opts.StorageOpt) > 0 {
			driver := &Driver{}
			if err := d.parseStorageOpt(opts.StorageOpt, driver); err != nil {
				return err
			}
			if driver.options.quota.Size > 0 {
				quota.Size = driver.options.quota.Size
			}
			if driver.options.quota.Inodes > 0 {
				quota.Inodes = driver.options.quota.Inodes
			}
		}
		// Set container disk quota limit
		// If it is set to 0, we will track the disk usage, but not enforce a limit
		if err := d.quotaCtl.SetQuota(dir, quota); err != nil {
			return err
		}
	}

	forcedSt := st
	if d.options.forceMask != nil {
		forcedSt.IDs = idPair
		forcedSt.Mode = *d.options.forceMask
	}

	diff := path.Join(dir, "diff")
	if err := idtools.MkdirAs(diff, forcedSt.Mode, forcedSt.IDs.UID, forcedSt.IDs.GID); err != nil {
		return err
	}

	if d.options.forceMask != nil {
		st.Mode |= os.ModeDir
		if err := idtools.SetContainersOverrideXattr(diff, st); err != nil {
			return err
		}
	}

	lid := generateID(idLength)

	linkBase := path.Join("..", id, "diff")
	if err := os.Symlink(linkBase, path.Join(homedir, linkDir, lid)); err != nil {
		return err
	}

	// Write link id to link file
	if err := os.WriteFile(path.Join(dir, "link"), []byte(lid), 0o644); err != nil {
		return err
	}

	if err := idtools.MkdirAs(path.Join(dir, "work"), 0o700, forcedSt.IDs.UID, forcedSt.IDs.GID); err != nil {
		return err
	}
	if err := idtools.MkdirAs(path.Join(dir, "merged"), 0o700, forcedSt.IDs.UID, forcedSt.IDs.GID); err != nil {
		return err
	}

	// if no parent directory, create a dummy lower directory and skip writing a "lowers" file
	if parent == "" {
		return idtools.MkdirAs(path.Join(dir, "empty"), 0o700, forcedSt.IDs.UID, forcedSt.IDs.GID)
	}

	lower, err := d.getLower(parent)
	if err != nil {
		return err
	}
	if lower != "" {
		if err := os.WriteFile(path.Join(dir, lowerFile), []byte(lower), 0o666); err != nil {
			return err
		}
	}

	return nil
}

// Parse overlay storage options
func (d *Driver) parseStorageOpt(storageOpt map[string]string, driver *Driver) error {
	// Read size to set the disk project quota per container
	for key, val := range storageOpt {
		key := strings.ToLower(key)
		switch key {
		case "size":
			size, err := units.RAMInBytes(val)
			if err != nil {
				return err
			}
			driver.options.quota.Size = uint64(size)
		case "inodes":
			inodes, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				return err
			}
			driver.options.quota.Inodes = inodes
		default:
			return fmt.Errorf("unknown option %s", key)
		}
	}

	return nil
}

func (d *Driver) getLower(parent string) (string, error) {
	parentDir := d.dir(parent)

	// Ensure parent exists
	if err := fileutils.Lexists(parentDir); err != nil {
		return "", err
	}

	// Read Parent link fileA
	parentLink, err := os.ReadFile(path.Join(parentDir, "link"))
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		logrus.Warnf("Can't read parent link %q because it does not exist. Going through storage to recreate the missing links.", path.Join(parentDir, "link"))
		if err := d.recreateSymlinks(); err != nil {
			return "", fmt.Errorf("recreating the links: %w", err)
		}
		parentLink, err = os.ReadFile(path.Join(parentDir, "link"))
		if err != nil {
			return "", err
		}
	}
	lowers := []string{path.Join(linkDir, string(parentLink))}

	parentLower, err := os.ReadFile(path.Join(parentDir, lowerFile))
	if err == nil {
		parentLowers := strings.SplitSeq(string(parentLower), ":")
		lowers = slices.AppendSeq(lowers, parentLowers)
	}
	return strings.Join(lowers, ":"), nil
}

func (d *Driver) dir(id string) string {
	p, _, _ := d.dir2(id, false)
	return p
}

func (d *Driver) getAllImageStores() []string {
	additionalImageStores := d.AdditionalImageStores()
	if d.imageStore != "" {
		additionalImageStores = append([]string{d.imageStore}, additionalImageStores...)
	}
	return additionalImageStores
}

// homeDirForImageStore returns the home directory to use when an image store is configured
func (d *Driver) homeDirForImageStore() string {
	if d.imageStore != "" {
		return path.Join(d.imageStore, d.name)
	}
	// If there is not an image store configured, use the same
	// store
	return d.home
}

func (d *Driver) dir2(id string, useImageStore bool) (string, string, bool) {
	homedir := d.home
	if useImageStore {
		homedir = d.homeDirForImageStore()
	}
	newpath := path.Join(homedir, id)
	if err := fileutils.Exists(newpath); err != nil {
		for _, p := range d.getAllImageStores() {
			l := path.Join(p, d.name, id)
			err = fileutils.Exists(l)
			if err == nil {
				return l, homedir, true
			}
		}
	}
	return newpath, homedir, false
}

func (d *Driver) getLowerDirs(id string) ([]string, error) {
	var lowersArray []string
	lowers, err := os.ReadFile(path.Join(d.dir(id), lowerFile))
	if err == nil {
		for s := range strings.SplitSeq(string(lowers), ":") {
			lower := d.dir(s)
			lp, err := os.Readlink(lower)
			// if the link does not exist, we lost the symlinks during a sudden reboot.
			// Let's go ahead and recreate those symlinks.
			if err != nil {
				if os.IsNotExist(err) {
					logrus.Warnf("Can't read link %q because it does not exist. A storage corruption might have occurred, attempting to recreate the missing symlinks. It might be best wipe the storage to avoid further errors due to storage corruption.", lower)
					if err := d.recreateSymlinks(); err != nil {
						return nil, fmt.Errorf("recreating the missing symlinks: %w", err)
					}
					// let's call Readlink on lower again now that we have recreated the missing symlinks
					lp, err = os.Readlink(lower)
					if err != nil {
						return nil, err
					}
				} else {
					return nil, err
				}
			}
			lowersArray = append(lowersArray, path.Clean(d.dir(path.Join("link", lp))))
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return lowersArray, nil
}

func (d *Driver) optsAppendMappings(opts string, uidMaps, gidMaps []idtools.IDMap) string {
	if uidMaps != nil {
		var uids, gids bytes.Buffer
		if len(uidMaps) == 1 && uidMaps[0].Size == 1 {
			uids.WriteString(fmt.Sprintf("squash_to_uid=%d", uidMaps[0].HostID))
		} else {
			uids.WriteString("uidmapping=")
			for _, i := range uidMaps {
				if uids.Len() > 0 {
					uids.WriteString(":")
				}
				uids.WriteString(fmt.Sprintf("%d:%d:%d", i.ContainerID, i.HostID, i.Size))
			}
		}
		if len(gidMaps) == 1 && gidMaps[0].Size == 1 {
			gids.WriteString(fmt.Sprintf("squash_to_gid=%d", gidMaps[0].HostID))
		} else {
			gids.WriteString("gidmapping=")
			for _, i := range gidMaps {
				if gids.Len() > 0 {
					gids.WriteString(":")
				}
				gids.WriteString(fmt.Sprintf("%d:%d:%d", i.ContainerID, i.HostID, i.Size))
			}
		}
		return fmt.Sprintf("%s,%s,%s", opts, uids.String(), gids.String())
	}
	return opts
}

// Remove cleans the directories that are created for this id.
func (d *Driver) Remove(id string) error {
	return d.removeCommon(id, system.EnsureRemoveAll)
}

func (d *Driver) removeCommon(id string, cleanup func(string) error) error {
	dir := d.dir(id)
	lid, err := os.ReadFile(path.Join(dir, "link"))
	if err == nil {
		linkPath := path.Join(d.home, linkDir, string(lid))
		if err := cleanup(linkPath); err != nil {
			logrus.Debugf("Failed to remove link: %v", err)
		}
	}

	d.releaseAdditionalLayerByID(id)

	if err := cleanup(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	if d.quotaCtl != nil {
		d.quotaCtl.ClearQuota(dir)
		if d.imageStore != "" {
			d.quotaCtl.ClearQuota(d.imageStore)
		}
	}
	return nil
}

func (d *Driver) GetTempDirRootDirs() []string {
	tempDirs := []string{filepath.Join(d.home, tempDirName)}
	// Include imageStore temp directory if it's configured
	// Writable layers can only be in d.home or d.imageStore, not in additional image stores
	if d.imageStore != "" {
		tempDirs = append(tempDirs, filepath.Join(d.imageStore, d.name, tempDirName))
	}
	return tempDirs
}

// Determine the correct temp directory root based on where the layer actually exists.
func (d *Driver) getTempDirRoot(id string) string {
	layerDir := d.dir(id)
	if d.imageStore != "" {
		expectedLayerDir := path.Join(d.imageStore, d.name, id)
		if layerDir == expectedLayerDir {
			return filepath.Join(d.imageStore, d.name, tempDirName)
		}
	}
	return filepath.Join(d.home, tempDirName)
}

func (d *Driver) DeferredRemove(id string) (tempdir.CleanupTempDirFunc, error) {
	tempDirRoot := d.getTempDirRoot(id)
	t, err := tempdir.NewTempDir(tempDirRoot)
	if err != nil {
		return nil, err
	}

	if err := d.removeCommon(id, t.StageDeletion); err != nil {
		return t.Cleanup, fmt.Errorf("failed to add to stage directory: %w", err)
	}
	return t.Cleanup, nil
}

// recreateSymlinks goes through the driver's home directory and checks if the diff directory
// under each layer has a symlink created for it under the linkDir. If the symlink does not
// exist, it creates them
func (d *Driver) recreateSymlinks() error {
	// We have at most 3 corrective actions per layer, so 10 iterations is plenty.
	const maxIterations = 10

	// List all the directories under the home directory
	dirs, err := os.ReadDir(d.home)
	if err != nil {
		return fmt.Errorf("reading driver home directory %q: %w", d.home, err)
	}
	// This makes the link directory if it doesn't exist
	if err := idtools.MkdirAllAs(path.Join(d.home, linkDir), 0o755, 0, 0); err != nil {
		return err
	}
	// Keep looping as long as we take some corrective action in each iteration
	var errs error
	madeProgress := true
	iterations := 0
	for madeProgress {
		errs = nil
		madeProgress = false
		// Check that for each layer, there's a link in "l" with the name in
		// the layer's "link" file that points to the layer's "diff" directory.
		for _, dir := range dirs {
			// Skip over the linkDir, stagingDir, tempDirName and anything that is not a directory
			if dir.Name() == linkDir || dir.Name() == stagingDir || dir.Name() == tempDirName || !dir.IsDir() {
				continue
			}
			// Read the "link" file under each layer to get the name of the symlink
			data, err := os.ReadFile(path.Join(d.dir(dir.Name()), "link"))
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("reading name of symlink for %q: %w", dir.Name(), err))
				continue
			}
			linkPath := path.Join(d.home, linkDir, strings.Trim(string(data), "\n"))
			// Check if the symlink exists, and if it doesn't, create it again with the
			// name we got from the "link" file
			err = fileutils.Lexists(linkPath)
			if err != nil && os.IsNotExist(err) {
				if err := os.Symlink(path.Join("..", dir.Name(), "diff"), linkPath); err != nil {
					errs = errors.Join(errs, err)
					continue
				}
				madeProgress = true
			} else if err != nil {
				errs = errors.Join(errs, err)
				continue
			}
		}

		// linkDirFullPath is the full path to the linkDir
		linkDirFullPath := filepath.Join(d.home, "l")
		// Now check if we somehow lost a "link" file, by making sure
		// that each symlink we have corresponds to one.
		links, err := os.ReadDir(linkDirFullPath)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		// Go through all of the symlinks in the "l" directory
		for _, link := range links {
			// Read the symlink's target, which should be "../$layer/diff"
			target, err := os.Readlink(filepath.Join(linkDirFullPath, link.Name()))
			if err != nil {
				errs = errors.Join(errs, err)
				continue
			}
			targetComponents := strings.Split(target, string(os.PathSeparator))
			if len(targetComponents) != 3 || targetComponents[0] != ".." || targetComponents[2] != "diff" {
				errs = errors.Join(errs, fmt.Errorf("link target of %q looks weird: %q", link, target))
				// force the link to be recreated on the next pass
				if err := os.Remove(filepath.Join(linkDirFullPath, link.Name())); err != nil {
					if !os.IsNotExist(err) {
						errs = errors.Join(errs, fmt.Errorf("removing link %q: %w", link, err))
					} // else don’t report any error, but also don’t set madeProgress.
					continue
				}
				madeProgress = true
				continue
			}
			// Reconstruct the name of the target's link file and check that
			// it has the basename of our symlink in it.
			targetID := targetComponents[1]
			linkFile := filepath.Join(d.dir(targetID), "link")
			data, err := os.ReadFile(linkFile)
			if err != nil || string(data) != link.Name() {
				// NOTE: If two or more links point to the same target, we will update linkFile
				// with every value of link.Name(), and set madeProgress = true every time.
				if err := os.WriteFile(linkFile, []byte(link.Name()), 0o644); err != nil {
					errs = errors.Join(errs, fmt.Errorf("correcting link for layer %s: %w", targetID, err))
					continue
				}
				madeProgress = true
			}
		}
		iterations++
		if iterations >= maxIterations {
			errs = errors.Join(errs, fmt.Errorf("reached %d iterations in overlay graph driver’s recreateSymlink, giving up", iterations))
			break
		}
	}
	return errs
}

// Get creates and mounts the required file system for the given id and returns the mount path.
func (d *Driver) Get(id string, options graphdriver.MountOpts) (string, error) {
	return d.get(id, false, options)
}

func (d *Driver) get(id string, disableShifting bool, options graphdriver.MountOpts) (_ string, retErr error) {
	dir, _, inAdditionalStore := d.dir2(id, false)
	if err := fileutils.Exists(dir); err != nil {
		return "", err
	}
	if _, err := redirectDiffIfAdditionalLayer(path.Join(dir, "diff"), true); err != nil {
		return "", err
	}

	// user namespace requires this to move a directory from lower to upper.
	rootUID, rootGID, err := idtools.GetRootUIDGID(options.UidMaps, options.GidMaps)
	if err != nil {
		return "", err
	}

	mergedDir := d.getMergedDir(id, dir, inAdditionalStore)
	// Attempt to create the merged dir if it doesn't exist, but don't chown an already existing directory (it might be in an additional store)
	if err := idtools.MkdirAllAndChownNew(mergedDir, 0o700, idtools.IDPair{UID: rootUID, GID: rootGID}); err != nil && !os.IsExist(err) {
		return "", err
	}

	if count := d.ctr.Increment(mergedDir); count > 1 {
		return mergedDir, nil
	}
	defer func() {
		if retErr != nil {
			if c := d.ctr.Decrement(mergedDir); c <= 0 {
				if mntErr := unix.Unmount(mergedDir, 0); mntErr != nil {
					// Ignore EINVAL, it means the directory is not a mount point and it can happen
					// if the current function fails before the mount point is created.
					if !errors.Is(mntErr, unix.EINVAL) {
						logrus.Errorf("Unmounting %v: %v", mergedDir, mntErr)
					}
				}
			}
		}
	}()

	readWrite := !inAdditionalStore

	if !d.SupportsShifting(options.UidMaps, options.GidMaps) || options.DisableShifting {
		disableShifting = true
	}

	logLevel := logrus.WarnLevel
	if unshare.IsRootless() {
		logLevel = logrus.DebugLevel
	}
	optsList := options.Options

	needsIDMapping := !disableShifting && len(options.UidMaps) > 0 && len(options.GidMaps) > 0 && d.options.mountProgram == ""

	if len(optsList) == 0 {
		if d.options.mountOptions != "" {
			optsList = strings.Split(d.options.mountOptions, ",")
		}
	} else {
		// If metacopy=on is present in d.options.mountOptions it must be present in the mount
		// options otherwise the kernel refuses to follow the metacopy xattr.
		if hasMetacopyOption(strings.Split(d.options.mountOptions, ",")) && !hasMetacopyOption(options.Options) {
			if d.usingMetacopy {
				logrus.StandardLogger().Logf(logrus.DebugLevel, "Adding metacopy option, configured globally")
				optsList = append(optsList, "metacopy=on")
			}
		}
	}
	if !d.usingMetacopy {
		if hasMetacopyOption(optsList) {
			if d.options.mountProgram == "" {
				release := ""
				var uts unix.Utsname
				if err := unix.Uname(&uts); err == nil {
					release = " " + string(uts.Release[:]) + " " + string(uts.Version[:])
				}
				logrus.StandardLogger().Logf(logLevel, "Ignoring global metacopy option, not supported with booted kernel %s", release)
			} else {
				logrus.Debugf("Ignoring global metacopy option, the mount program doesn't support it")
			}
		}
		optsList = slices.DeleteFunc(optsList, func(opt string) bool {
			return opt == "metacopy=on"
		})
	}

	if slices.Contains(optsList, "ro") {
		readWrite = false
	}

	lowers, err := os.ReadFile(path.Join(dir, lowerFile))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	splitLowers := strings.Split(string(lowers), ":")
	if len(splitLowers) > maxDepth {
		return "", errors.New("max depth exceeded")
	}

	// absLowers is the list of lowers as absolute paths.
	absLowers := []string{}

	diffN := 1
	perms := defaultPerms
	if d.options.forceMask != nil {
		perms = *d.options.forceMask
	}
	permsKnown := false
	st, err := os.Stat(filepath.Join(dir, nameWithSuffix("diff", diffN)))
	if err == nil {
		perms = st.Mode()
		permsKnown = true
	}
	for err == nil {
		absLowers = append(absLowers, filepath.Join(dir, nameWithSuffix("diff", diffN)))
		diffN++
		err = fileutils.Exists(filepath.Join(dir, nameWithSuffix("diff", diffN)))
	}

	idmappedMountProcessPid := -1
	if needsIDMapping {
		pid, cleanupFunc, err := idmap.CreateUsernsProcess(options.UidMaps, options.GidMaps)
		if err != nil {
			return "", err
		}
		idmappedMountProcessPid = pid
		defer cleanupFunc()
	}

	skipIDMappingLayers := make(map[string]string)

	composefsMounts := []string{}
	defer func() {
		for _, m := range composefsMounts {
			defer func(m string) {
				if err := unix.Unmount(m, unix.MNT_DETACH); err != nil {
					logrus.Warnf("Unmount %q: %v", m, err)
				}
			}(m)
		}
	}()

	composeFsLayers := []string{}
	maybeAddComposefsMount := func(lowerID string, i int, readWrite bool) (string, error) {
		composefsBlob := d.getComposefsData(lowerID)
		if err := fileutils.Exists(composefsBlob); err != nil {
			if os.IsNotExist(err) {
				return "", nil
			}
			return "", err
		}
		logrus.Debugf("overlay: using composefs blob %s for lower %s", composefsBlob, lowerID)

		if readWrite && i == 0 {
			return "", fmt.Errorf("cannot mount a composefs layer as writeable")
		}

		dest := d.getStorePrivateDirectory(id, dir, fmt.Sprintf("composefs-layers/%d", i), inAdditionalStore)
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return "", err
		}

		if err := mountComposefsBlob(composefsBlob, dest); err != nil {
			return "", err
		}
		composefsMounts = append(composefsMounts, dest)
		composeFsPath, err := d.getDiffPath(lowerID)
		if err != nil {
			return "", err
		}
		composeFsLayers = append(composeFsLayers, composeFsPath)
		skipIDMappingLayers[composeFsPath] = composeFsPath
		return dest, nil
	}

	diffDir := path.Join(dir, "diff")

	if dest, err := maybeAddComposefsMount(id, 0, readWrite); err != nil {
		return "", err
	} else if dest != "" {
		diffDir = dest
	}

	// For each lower, resolve its path, and append it and any additional diffN
	// directories to the lowers list.
	for i, l := range splitLowers {
		if l == "" {
			continue
		}

		lower := ""
		newpath := path.Join(d.home, l)
		if st, err := os.Stat(newpath); err != nil {
			for _, p := range d.getAllImageStores() {
				lower = path.Join(p, d.name, l)
				if st2, err2 := os.Stat(lower); err2 == nil {
					if !permsKnown {
						perms = st2.Mode()
						permsKnown = true
					}
					break
				}
				lower = ""
			}
			// if it is a "not found" error, that means the symlinks were lost in a sudden reboot
			// so call the recreateSymlinks function to go through all the layer dirs and recreate
			// the symlinks with the name from their respective "link" files
			if lower == "" && os.IsNotExist(err) {
				logrus.Warnf("Can't stat lower layer %q because it does not exist. Going through storage to recreate the missing symlinks.", newpath)
				if err := d.recreateSymlinks(); err != nil {
					return "", fmt.Errorf("recreating the missing symlinks: %w", err)
				}
				lower = newpath
			} else if lower == "" {
				return "", fmt.Errorf("can't stat lower layer %q: %w", newpath, err)
			}
		} else {
			if !permsKnown {
				perms = st.Mode()
				permsKnown = true
			}
			lower = newpath
		}

		linkContent, err := os.Readlink(lower)
		if err != nil {
			return "", err
		}
		lowerID := filepath.Base(filepath.Dir(linkContent))
		composefsMount, err := maybeAddComposefsMount(lowerID, i+1, readWrite)
		if err != nil {
			return "", err
		}
		if composefsMount != "" {
			if needsIDMapping {
				if err := idmap.CreateIDMappedMount(composefsMount, composefsMount, idmappedMountProcessPid); err != nil {
					return "", fmt.Errorf("create mapped mount for %q: %w", composefsMount, err)
				}
				skipIDMappingLayers[composefsMount] = composefsMount
				// overlay takes a reference on the mount, so it is safe to unmount
				// the mapped idmounts as soon as the final overlay file system is mounted.
				defer func() {
					if err := unix.Unmount(composefsMount, unix.MNT_DETACH); err != nil {
						logrus.Warnf("Unmount %q: %v", composefsMount, err)
					}
				}()
			}
			absLowers = append(absLowers, composefsMount)
			continue
		}

		absLowers = append(absLowers, lower)
		diffN = 1
		err = fileutils.Exists(dumbJoin(lower, "..", nameWithSuffix("diff", diffN)))
		for err == nil {
			absLowers = append(absLowers, dumbJoin(lower, "..", nameWithSuffix("diff", diffN)))
			diffN++
			err = fileutils.Exists(dumbJoin(lower, "..", nameWithSuffix("diff", diffN)))
		}
	}

	if len(composeFsLayers) > 0 {
		optsList = append(optsList, "metacopy=on", "redirect_dir=on")
	}

	if len(absLowers) == 0 {
		absLowers = append(absLowers, path.Join(dir, "empty"))
	}

	if err := idtools.MkdirAllAs(diffDir, perms, rootUID, rootGID); err != nil {
		if !inAdditionalStore {
			return "", err
		}
		// if it is in an additional store, do not fail if the directory already exists
		if err2 := fileutils.Exists(diffDir); err2 != nil {
			return "", err
		}
	}

	workdir := path.Join(dir, "work")

	if d.options.mountProgram == "" && unshare.IsRootless() {
		optsList = append(optsList, "userxattr")
	}

	if options.Volatile && !slices.Contains(optsList, "volatile") {
		supported, err := d.getSupportsVolatile()
		if err != nil {
			return "", err
		}
		// If "volatile" is not supported by the file system, just ignore the request
		if supported {
			optsList = append(optsList, "volatile")
		}
	}

	if needsIDMapping {
		var newAbsDir []string
		idMappedMounts := make(map[string]string)

		mappedRoot := filepath.Join(d.home, id, "mapped")
		if err := os.MkdirAll(mappedRoot, 0o700); err != nil {
			return "", err
		}

		// rewrite the lower dirs to their idmapped mount.
		c := 0
		for _, absLower := range absLowers {
			mappedMountSrc := getMappedMountRoot(absLower)

			if _, ok := skipIDMappingLayers[absLower]; ok {
				newAbsDir = append(newAbsDir, absLower)
				continue
			}

			root, found := idMappedMounts[mappedMountSrc]
			if !found {
				root = filepath.Join(mappedRoot, fmt.Sprintf("%d", c))
				c++
				if err := idmap.CreateIDMappedMount(mappedMountSrc, root, idmappedMountProcessPid); err != nil {
					return "", fmt.Errorf("create mapped mount for %q on %q: %w", mappedMountSrc, root, err)
				}
				idMappedMounts[mappedMountSrc] = root

				// overlay takes a reference on the mount, so it is safe to unmount
				// the mapped idmounts as soon as the final overlay file system is mounted.
				defer func() {
					if err := unix.Unmount(root, unix.MNT_DETACH); err != nil {
						logrus.Warnf("Unmount %q: %v", root, err)
					}
				}()
			}

			// relative path to the layer through the id mapped mount
			rel, err := filepath.Rel(mappedMountSrc, absLower)
			if err != nil {
				return "", err
			}

			newAbsDir = append(newAbsDir, filepath.Join(root, rel))
		}
		absLowers = newAbsDir
	}

	lowerDirs := strings.Join(absLowers, ":")
	if len(composeFsLayers) > 0 {
		sep := "::"
		supportsDataOnly, err := d.getSupportsDataOnly()
		if err != nil {
			return "", err
		}
		if !supportsDataOnly {
			sep = ":"
		}
		composeFsLayersLowerDirs := strings.Join(composeFsLayers, sep)
		lowerDirs = lowerDirs + sep + composeFsLayersLowerDirs
	}
	// absLowers is not valid anymore now as we have added composeFsLayers to it, so prevent
	// its usage.
	absLowers = nil //nolint:ineffassign

	var opts string
	if readWrite {
		opts = fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDirs, diffDir, workdir)
	} else {
		opts = fmt.Sprintf("lowerdir=%s:%s", diffDir, lowerDirs)
	}
	if len(optsList) > 0 {
		opts = fmt.Sprintf("%s,%s", opts, strings.Join(optsList, ","))
	}

	mountData := label.FormatMountLabel(opts, options.MountLabel)
	mountFunc := unix.Mount
	mountTarget := mergedDir

	pageSize := unix.Getpagesize()

	if d.options.mountProgram != "" {
		mountFunc = func(source string, target string, mType string, flags uintptr, label string) error {
			if !disableShifting {
				label = d.optsAppendMappings(label, options.UidMaps, options.GidMaps)
			}

			// if forceMask is in place, tell fuse-overlayfs to write the permissions mask to an unprivileged xattr as well.
			if d.options.forceMask != nil {
				label = label + ",xattr_permissions=2"
			}

			mountProgram := exec.Command(d.options.mountProgram, "-o", label, target)
			mountProgram.Dir = d.home
			var b bytes.Buffer
			mountProgram.Stderr = &b
			err := mountProgram.Run()
			if err != nil {
				output := b.String()
				if output == "" {
					output = "<stderr empty>"
				}
				return fmt.Errorf("using mount program %s: %s: %w", d.options.mountProgram, output, err)
			}
			return nil
		}
	} else if len(mountData) >= pageSize {
		// Use mountFrom when the mount data has exceeded the page size. The mount syscall fails if
		// the mount data cannot fit within a page and relative links make the mount data much
		// smaller at the expense of requiring a fork exec to chdir().
		if readWrite {
			diffDir := path.Join(id, "diff")
			workDir := path.Join(id, "work")
			opts = fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDirs, diffDir, workDir)
		} else {
			opts = fmt.Sprintf("lowerdir=%s:%s", diffDir, lowerDirs)
		}
		if len(optsList) > 0 {
			opts = strings.Join(append([]string{opts}, optsList...), ",")
		}
		mountData = label.FormatMountLabel(opts, options.MountLabel)
		mountFunc = func(source string, target string, mType string, flags uintptr, label string) error {
			return mountOverlayFrom(d.home, source, target, mType, flags, label)
		}
		if !inAdditionalStore {
			mountTarget = path.Join(id, "merged")
		}
	}

	// overlay has a check in place to prevent mounting the same file system twice
	// if volatile was already specified. Yes, the kernel repeats the "work" component.
	err = os.RemoveAll(filepath.Join(workdir, "work", "incompat", "volatile"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	flags, data := mount.ParseOptions(mountData)
	logrus.Debugf("overlay: mount_data=%s", mountData)
	if err := mountFunc("overlay", mountTarget, "overlay", uintptr(flags), data); err != nil {
		return "", fmt.Errorf("creating overlay mount to %s, mount_data=%q: %w", mountTarget, mountData, err)
	}

	return mergedDir, nil
}

// getStorePrivateDirectory returns a directory path for storing data that requires exclusive access.
// If 'inAdditionalStore' is true, the path will be under the rundir, otherwise it will be placed in
// the primary store.
func (d *Driver) getStorePrivateDirectory(id, layerDir, subdir string, inAdditionalStore bool) string {
	if inAdditionalStore {
		return path.Join(d.runhome, id, subdir)
	}
	return path.Join(layerDir, subdir)
}

// getMergedDir returns the directory path that should be used as the mount point for the overlayfs.
func (d *Driver) getMergedDir(id, dir string, inAdditionalStore bool) string {
	// Ordinarily, .Get() (layer mounting) callers are supposed to guarantee exclusion.
	//
	// But additional stores are initialized with RO locks and don’t support a write
	// lock operation at all; and naiveDiff operations cause mounts/unmounts, so they might
	// happen on code paths where we might only holding a RO lock for the additional store.
	// To prevent races with other processes mounting or unmounting the layer,
	// use a private directory under the main store rundir, not the "merged" directory inside the
	// original layer store holding the layer data.
	//
	// To support this, contrary to the _general_ locking rules for .Diff / .Changes (which allow a RO lock),
	// the top-level Store implementation uses an exclusive lock for the primary layer store;
	// and since the rundir cannot be shared for different stores, it is safe to assume the
	// current process has exclusive access to it.
	//
	// TO DO: LOCKING BUG: the .DiffSize operation does not currently hold an exclusive lock on the primary store.
	// (_Some_ of the callers might be better ported to use a metadata-only size computation instead of DiffSize,
	// but DiffSize probably needs to remain for computing sizes of container’s RW layers.)
	return d.getStorePrivateDirectory(id, dir, "merged", inAdditionalStore)
}

// Put unmounts the mount path created for the give id.
func (d *Driver) Put(id string) error {
	dir, _, inAdditionalStore := d.dir2(id, false)
	if err := fileutils.Exists(dir); err != nil {
		return err
	}
	mountpoint := d.getMergedDir(id, dir, inAdditionalStore)

	if count := d.ctr.Decrement(mountpoint); count > 0 {
		return nil
	}
	if err := fileutils.Exists(path.Join(dir, lowerFile)); err != nil && !os.IsNotExist(err) {
		return err
	}

	unmounted := false

	mappedRoot := filepath.Join(d.home, id, "mapped")
	// It should not happen, but cleanup any mapped mount if it was leaked.
	if err := fileutils.Exists(mappedRoot); err == nil {
		mounts, err := os.ReadDir(mappedRoot)
		if err == nil {
			// Go through all of the mapped mounts.
			for _, m := range mounts {
				_ = unix.Unmount(filepath.Join(mappedRoot, m.Name()), unix.MNT_DETACH)
			}
		}
	}

	if d.options.mountProgram != "" {
		// Attempt to unmount the FUSE mount using either fusermount or fusermount3.
		// If they fail, fallback to unix.Unmount
		for _, v := range []string{"fusermount3", "fusermount"} {
			err := exec.Command(v, "-u", mountpoint).Run()
			if err != nil && !errors.Is(err, exec.ErrNotFound) {
				logrus.Debugf("Error unmounting %s with %s - %v", mountpoint, v, err)
			}
			if err == nil {
				unmounted = true
				break
			}
		}
		// If fusermount|fusermount3 failed to unmount the FUSE file system, make sure all
		// pending changes are propagated to the file system
		if !unmounted {
			fd, err := unix.Open(mountpoint, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err == nil {
				if err := unix.Syncfs(fd); err != nil {
					logrus.Debugf("Error Syncfs(%s) - %v", mountpoint, err)
				}
				unix.Close(fd)
			}
		}
	}

	if !unmounted {
		if err := unix.Unmount(mountpoint, unix.MNT_DETACH); err != nil && !os.IsNotExist(err) {
			logrus.Debugf("Failed to unmount %s overlay: %s - %v", id, mountpoint, err)
			if !errors.Is(err, unix.EINVAL) {
				return fmt.Errorf("unmounting %q: %w", mountpoint, err)
			}
		}
	}

	if inAdditionalStore {
		// check the base name for extra safety
		if strings.HasPrefix(mountpoint, d.runhome) && filepath.Base(mountpoint) == "merged" {
			err := os.RemoveAll(filepath.Dir(mountpoint))
			if err != nil {
				logrus.Warningf("Failed to remove mountpoint %s overlay: %s: %v", id, mountpoint, err)
			}
		}
	} else {
		uid, gid := int(0), int(0)
		fi, err := os.Stat(mountpoint)
		if err != nil {
			return err
		}
		if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(stat.Uid), int(stat.Gid)
		}

		tmpMountpoint := path.Join(dir, "merged.1")
		if err := idtools.MkdirAs(tmpMountpoint, 0o700, uid, gid); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		// rename(2) can be used on an empty directory, as it is the mountpoint after umount, and it retains
		// its atomic semantic.  In this way the "merged" directory is never removed.
		if err := unix.Rename(tmpMountpoint, mountpoint); err != nil {
			logrus.Debugf("Failed to replace mountpoint %s overlay: %s: %v", id, mountpoint, err)
			return fmt.Errorf("replacing mount point %q: %w", mountpoint, err)
		}
	}
	return nil
}

// Exists checks to see if the id is already mounted.
func (d *Driver) Exists(id string) bool {
	err := fileutils.Exists(d.dir(id))
	return err == nil
}

// List layers (not including additional image stores)
func (d *Driver) ListLayers() ([]string, error) {
	entries, err := os.ReadDir(d.home)
	if err != nil {
		return nil, err
	}
	layers := make([]string, 0)

	for _, entry := range entries {
		id := entry.Name()
		switch id {
		case linkDir, stagingDir, tempDirName, quota.BackingFsBlockDeviceLink, mountProgramFlagFile:
			// expected, but not a layer. skip it
			continue
		default:
			// Does it look like a datadir directory?
			if !entry.IsDir() {
				continue
			}
			layers = append(layers, id)
		}
	}
	return layers, nil
}

// isParent returns if the passed in parent is the direct parent of the passed in layer
func (d *Driver) isParent(id, parent string) bool {
	lowers, err := d.getLowerDirs(id)
	if err != nil {
		return false
	}
	if parent == "" && len(lowers) > 0 {
		return false
	}

	parentDir := d.dir(parent)
	var ld string
	if len(lowers) > 0 {
		ld = filepath.Dir(lowers[0])
	}
	if ld == "" && parent == "" {
		return true
	}
	return ld == parentDir
}

func (d *Driver) getWhiteoutFormat() archive.WhiteoutFormat {
	whiteoutFormat := archive.OverlayWhiteoutFormat
	if d.options.mountProgram != "" {
		// If we are using a mount program, we are most likely running
		// as an unprivileged user that cannot use mknod, so fallback to the
		// AUFS whiteout format.
		whiteoutFormat = archive.AUFSWhiteoutFormat
	}
	return whiteoutFormat
}

type overlayFileGetter struct {
	diffDirs        []string
	composefsMounts map[string]*os.File // map from diff dir to the directory with the composefs blob mounted
}

func (g *overlayFileGetter) Get(path string) (io.ReadCloser, error) {
	buf := make([]byte, unix.PathMax)
	for _, d := range g.diffDirs {
		if f, found := g.composefsMounts[d]; found {
			// there is no *at equivalent for getxattr, but it can be emulated by opening the file under /proc/self/fd/$FD/$PATH
			len, err := unix.Getxattr(fmt.Sprintf("/proc/self/fd/%d/%s", int(f.Fd()), path), "trusted.overlay.redirect", buf)
			if err != nil {
				if errors.Is(err, unix.ENODATA) {
					continue
				}
				return nil, &fs.PathError{Op: "getxattr", Path: path, Err: err}
			}

			// the xattr value is the path to the file in the composefs layer diff directory
			return os.Open(filepath.Join(d, string(buf[:len])))
		}

		f, err := os.Open(filepath.Join(d, path))
		if err == nil {
			return f, nil
		}
	}
	if len(g.diffDirs) > 0 {
		return os.Open(filepath.Join(g.diffDirs[0], path))
	}
	return nil, fmt.Errorf("%s: %w", path, os.ErrNotExist)
}

func (g *overlayFileGetter) Close() (errs error) {
	for _, f := range g.composefsMounts {
		if err := f.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
		if err := unix.Rmdir(f.Name()); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// newStagingDir creates a new staging directory and returns the path to it.
func (d *Driver) newStagingDir() (string, error) {
	stagingDirBase := filepath.Join(d.homeDirForImageStore(), stagingDir)
	err := os.MkdirAll(stagingDirBase, 0o700)
	if err != nil && !os.IsExist(err) {
		return "", err
	}
	return os.MkdirTemp(stagingDirBase, "")
}

// DiffGetter returns a FileGetCloser that can read files from the directory that
// contains files for the layer differences, either for this layer, or one of our
// lowers if we're just a template directory. Used for direct access for tar-split.
func (d *Driver) DiffGetter(id string) (_ graphdriver.FileGetCloser, Err error) {
	p, err := d.getDiffPath(id)
	if err != nil {
		return nil, err
	}
	paths, err := d.getLowerDiffPaths(id)
	if err != nil {
		return nil, err
	}

	// map from diff dir to the directory with the composefs blob mounted
	composefsMounts := make(map[string]*os.File)
	defer func() {
		if Err != nil {
			for _, f := range composefsMounts {
				f.Close()
				if err := unix.Rmdir(f.Name()); err != nil && !os.IsNotExist(err) {
					logrus.Warnf("Failed to remove %s: %v", f.Name(), err)
				}
			}
		}
	}()
	diffDirs := append([]string{p}, paths...)
	for _, diffDir := range diffDirs {
		// diffDir has the form $GRAPH_ROOT/overlay/$ID/diff, so grab the $ID from the parent directory
		id := path.Base(path.Dir(diffDir))
		composefsData := d.getComposefsData(id)
		if fileutils.Exists(composefsData) != nil {
			// not a composefs layer, ignore it
			continue
		}
		fd, err := openComposefsMount(composefsData)
		if err != nil {
			return nil, err
		}
		composefsMounts[diffDir] = os.NewFile(uintptr(fd), composefsData)
	}
	return &overlayFileGetter{diffDirs: diffDirs, composefsMounts: composefsMounts}, nil
}

// CleanupStagingDirectory cleanups the staging directory.
func (d *Driver) CleanupStagingDirectory(stagingDirectory string) error {
	parentStagingDir := filepath.Dir(stagingDirectory)

	d.stagingDirsLocksMutex.Lock()
	if lock, ok := d.stagingDirsLocks[parentStagingDir]; ok {
		delete(d.stagingDirsLocks, parentStagingDir)
		if err := lock.UnlockAndDelete(); err != nil {
			d.stagingDirsLocksMutex.Unlock()
			return err
		}
	}
	d.stagingDirsLocksMutex.Unlock()

	return os.RemoveAll(parentStagingDir)
}

func supportsDataOnlyLayersCached(home, runhome string) (bool, error) {
	feature := "dataonly-layers"
	overlayCacheResult, _, err := cachedFeatureCheck(runhome, feature)
	if err == nil {
		if overlayCacheResult {
			logrus.Debugf("Cached value indicated that data-only layers for overlay are supported")
			return true, nil
		}
		logrus.Debugf("Cached value indicated that data-only layers for overlay are not supported")
		return false, nil
	}
	supportsDataOnly, err := supportsDataOnlyLayers(home)
	if err2 := cachedFeatureRecord(runhome, feature, supportsDataOnly, ""); err2 != nil {
		return false, fmt.Errorf("recording overlay data-only layers support status: %w", err2)
	}
	return supportsDataOnly, err
}

// ApplyDiffWithDiffer applies the changes in the new layer using the specified function
func (d *Driver) ApplyDiffWithDiffer(options *graphdriver.ApplyDiffWithDifferOpts, differ graphdriver.Differ) (output graphdriver.DriverWithDifferOutput, errRet error) {
	var idMappings *idtools.IDMappings
	var forceMask *os.FileMode

	if options != nil {
		idMappings = options.Mappings
		forceMask = options.ForceMask
	}
	if d.options.forceMask != nil {
		forceMask = d.options.forceMask
	}

	if idMappings == nil {
		idMappings = &idtools.IDMappings{}
	}

	layerDir, err := d.newStagingDir()
	if err != nil {
		return graphdriver.DriverWithDifferOutput{}, err
	}
	perms := defaultPerms
	if forceMask != nil {
		perms = *forceMask
	}
	applyDir := filepath.Join(layerDir, "dir")
	if err := os.Mkdir(applyDir, perms); err != nil {
		return graphdriver.DriverWithDifferOutput{}, err
	}

	lock, err := staging_lockfile.TryLockPath(filepath.Join(layerDir, stagingLockFile))
	if err != nil {
		return graphdriver.DriverWithDifferOutput{}, err
	}
	defer func() {
		if errRet != nil {
			d.stagingDirsLocksMutex.Lock()
			delete(d.stagingDirsLocks, layerDir)
			d.stagingDirsLocksMutex.Unlock()
			if err := lock.UnlockAndDelete(); err != nil {
				errRet = errors.Join(errRet, err)
			}
		}
	}()
	d.stagingDirsLocksMutex.Lock()
	d.stagingDirsLocks[layerDir] = lock
	d.stagingDirsLocksMutex.Unlock()

	logrus.Debugf("Applying differ in %s", applyDir)

	differOptions := graphdriver.DifferOptions{
		Format: graphdriver.DifferOutputFormatDir,
	}
	if d.usingComposefs {
		differOptions.Format = graphdriver.DifferOutputFormatFlat
		differOptions.UseFsVerity = graphdriver.DifferFsVerityIfAvailable
	}
	out, err := differ.ApplyDiff(applyDir, &archive.TarOptions{
		UIDMaps:           idMappings.UIDs(),
		GIDMaps:           idMappings.GIDs(),
		IgnoreChownErrors: d.options.ignoreChownErrors,
		WhiteoutFormat:    d.getWhiteoutFormat(),
		InUserNS:          unshare.IsRootless(),
		ForceMask:         forceMask,
	}, &differOptions)

	out.Target = applyDir

	return out, err
}

// ApplyDiffFromStagingDirectory applies the changes using the specified staging directory.
func (d *Driver) ApplyDiffFromStagingDirectory(id, parent string, diffOutput *graphdriver.DriverWithDifferOutput, options *graphdriver.ApplyDiffWithDifferOpts) (errRet error) {
	stagingDirectory := diffOutput.Target
	parentStagingDir := filepath.Dir(stagingDirectory)

	defer func() {
		d.stagingDirsLocksMutex.Lock()
		if lock, ok := d.stagingDirsLocks[parentStagingDir]; ok {
			delete(d.stagingDirsLocks, parentStagingDir)
			if err := lock.UnlockAndDelete(); err != nil {
				errRet = errors.Join(errRet, err)
			}
		}
		d.stagingDirsLocksMutex.Unlock()
	}()

	diffPath, err := d.getDiffPath(id)
	if err != nil {
		return err
	}

	// If the current layer doesn't set the mode for the parent, override it with the parent layer's mode.
	if d.options.forceMask == nil && diffOutput.RootDirMode == nil && parent != "" {
		parentDiffPath, err := d.getDiffPath(parent)
		if err != nil {
			return err
		}
		parentSt, err := os.Stat(parentDiffPath)
		if err != nil {
			return err
		}
		if err := os.Chmod(stagingDirectory, parentSt.Mode()); err != nil {
			return err
		}
	}

	if d.usingComposefs {
		toc := diffOutput.Artifacts[tocArtifact]
		verityDigests := diffOutput.Artifacts[fsVerityDigestsArtifact].(map[string]string)
		if err := generateComposeFsBlob(verityDigests, toc, d.getComposefsData(id)); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(diffPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.Rename(stagingDirectory, diffPath)
}

// DifferTarget gets the location where files are stored for the layer.
func (d *Driver) DifferTarget(id string) (string, error) {
	return d.getDiffPath(id)
}

// ApplyDiff applies the new layer into a root
func (d *Driver) ApplyDiff(id, parent string, options graphdriver.ApplyDiffOpts) (size int64, err error) {
	if !d.isParent(id, parent) {
		if d.options.ignoreChownErrors {
			options.IgnoreChownErrors = d.options.ignoreChownErrors
		}
		if d.options.forceMask != nil {
			options.ForceMask = d.options.forceMask
		}
		return d.naiveDiff.ApplyDiff(id, parent, options)
	}

	idMappings := options.Mappings
	if idMappings == nil {
		idMappings = &idtools.IDMappings{}
	}

	applyDir, err := d.getDiffPath(id)
	if err != nil {
		return 0, err
	}

	logrus.Debugf("Applying tar in %s", applyDir)
	// Overlay doesn't need the parent id to apply the diff
	if err := untar(options.Diff, applyDir, &archive.TarOptions{
		UIDMaps:           idMappings.UIDs(),
		GIDMaps:           idMappings.GIDs(),
		IgnoreChownErrors: d.options.ignoreChownErrors,
		ForceMask:         d.options.forceMask,
		WhiteoutFormat:    d.getWhiteoutFormat(),
		InUserNS:          unshare.IsRootless(),
	}); err != nil {
		return 0, err
	}

	return directory.Size(applyDir)
}

func (d *Driver) getComposefsData(id string) string {
	dir := d.dir(id)
	return path.Join(dir, "composefs-data")
}

func (d *Driver) getDiffPath(id string) (string, error) {
	dir := d.dir(id)
	return redirectDiffIfAdditionalLayer(path.Join(dir, "diff"), false)
}

func (d *Driver) getLowerDiffPaths(id string) ([]string, error) {
	layers, err := d.getLowerDirs(id)
	if err != nil {
		return nil, err
	}
	for i, l := range layers {
		layers[i], err = redirectDiffIfAdditionalLayer(l, false)
		if err != nil {
			return nil, err
		}
	}
	return layers, nil
}

// DiffSize calculates the changes between the specified id
// and its parent and returns the size in bytes of the changes
// relative to its base filesystem directory.
func (d *Driver) DiffSize(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (size int64, err error) {
	if !d.isParent(id, parent) {
		return d.naiveDiff.DiffSize(id, idMappings, parent, parentMappings, mountLabel)
	}

	p, err := d.getDiffPath(id)
	if err != nil {
		return 0, err
	}
	return directory.Size(p)
}

// Diff produces an archive of the changes between the specified
// layer and its parent layer which may be "".
func (d *Driver) Diff(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (io.ReadCloser, error) {
	if d.useNaiveDiff() || !d.isParent(id, parent) {
		return d.naiveDiff.Diff(id, idMappings, parent, parentMappings, mountLabel)
	}

	if idMappings == nil {
		idMappings = &idtools.IDMappings{}
	}

	lowerDirs, err := d.getLowerDiffPaths(id)
	if err != nil {
		return nil, err
	}

	diffPath, err := d.getDiffPath(id)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("Tar with options on %s", diffPath)
	return archive.TarWithOptions(diffPath, &archive.TarOptions{
		Compression:    archive.Uncompressed,
		UIDMaps:        idMappings.UIDs(),
		GIDMaps:        idMappings.GIDs(),
		WhiteoutFormat: d.getWhiteoutFormat(),
		WhiteoutData:   lowerDirs,
	})
}

// Changes produces a list of changes between the specified layer
// and its parent layer. If parent is "", then all changes will be ADD changes.
func (d *Driver) Changes(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) ([]archive.Change, error) {
	if d.useNaiveDiff() || !d.isParent(id, parent) {
		return d.naiveDiff.Changes(id, idMappings, parent, parentMappings, mountLabel)
	}
	// Overlay doesn't have snapshots, so we need to get changes from all parent
	// layers.
	diffPath, err := d.getDiffPath(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff path: %w", err)
	}
	layers, err := d.getLowerDiffPaths(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get lower diff path: %w", err)
	}

	c, err := archive.OverlayChanges(layers, diffPath)
	if err != nil {
		return nil, fmt.Errorf("computing changes: %w", err)
	}
	return c, nil
}

// AdditionalImageStores returns additional image stores supported by the driver
func (d *Driver) AdditionalImageStores() []string {
	return d.options.imageStores
}

// UpdateLayerIDMap updates ID mappings in a from matching the ones specified
// by toContainer to those specified by toHost.
func (d *Driver) UpdateLayerIDMap(id string, toContainer, toHost *idtools.IDMappings, mountLabel string) error {
	var err error
	dir := d.dir(id)
	diffDir := filepath.Join(dir, "diff")

	rootUID, rootGID := 0, 0
	if toHost != nil {
		rootUID, rootGID, err = idtools.GetRootUIDGID(toHost.UIDs(), toHost.GIDs())
		if err != nil {
			return err
		}
	}

	// Mount the new layer and handle ownership changes and possible copy_ups in it.
	options := graphdriver.MountOpts{
		MountLabel: mountLabel,
		Options:    strings.Split(d.options.mountOptions, ","),
	}
	layerFs, err := d.get(id, true, options)
	if err != nil {
		return err
	}
	err = graphdriver.ChownPathByMaps(layerFs, toContainer, toHost)
	if err != nil {
		if err2 := d.Put(id); err2 != nil {
			logrus.Errorf("%v; unmounting %v: %v", err, id, err2)
		}
		return err
	}
	if err = d.Put(id); err != nil {
		return err
	}

	// Rotate the diff directories.
	i := 0
	perms := defaultPerms
	st, err := os.Stat(nameWithSuffix(diffDir, i))
	if d.options.forceMask != nil {
		perms = *d.options.forceMask
	} else {
		if err == nil {
			perms = st.Mode()
		}
	}
	for err == nil {
		i++
		err = fileutils.Exists(nameWithSuffix(diffDir, i))
	}

	for i > 0 {
		err = os.Rename(nameWithSuffix(diffDir, i-1), nameWithSuffix(diffDir, i))
		if err != nil {
			return err
		}
		i--
	}

	// We need to re-create the work directory as it might keep a reference
	// to the old upper layer in the index.
	workDir := filepath.Join(dir, "work")
	if err := os.RemoveAll(workDir); err == nil {
		if err := idtools.MkdirAs(workDir, defaultPerms, rootUID, rootGID); err != nil {
			return err
		}
	}

	// Re-create the directory that we're going to use as the upper layer.
	if err := idtools.MkdirAs(diffDir, perms, rootUID, rootGID); err != nil {
		return err
	}
	return nil
}

// supportsIDmappedMounts returns whether the kernel supports using idmapped mounts with
// overlay lower layers.
func (d *Driver) supportsIDmappedMounts() bool {
	if d.supportsIDMappedMounts != nil {
		return *d.supportsIDMappedMounts
	}

	supportsIDMappedMounts, err := checkAndRecordIDMappedSupport(d.home, d.runhome)
	d.supportsIDMappedMounts = &supportsIDMappedMounts
	if err == nil {
		return supportsIDMappedMounts
	}
	logrus.Debugf("Check for idmapped mounts support %v", err)
	return false
}

// SupportsShifting tells whether the driver support shifting of the UIDs/GIDs to the provided mapping in an userNS
func (d *Driver) SupportsShifting(uidmap, gidmap []idtools.IDMap) bool {
	if os.Getenv("_CONTAINERS_OVERLAY_DISABLE_IDMAP") == "yes" {
		return false
	}
	if d.options.mountProgram != "" {
		// fuse-overlayfs supports only contiguous mappings, since it performs the mapping on the
		// upper layer too, to avoid https://github.com/containers/podman/issues/10272
		if !idtools.IsContiguous(uidmap) {
			return false
		}
		if !idtools.IsContiguous(gidmap) {
			return false
		}
		return true
	}
	return d.supportsIDmappedMounts()
}

// dumbJoin is more or less a dumber version of filepath.Join, but one which
// won't Clean() the path, allowing us to append ".." as a component and trust
// pathname resolution to do some non-obvious work.
func dumbJoin(names ...string) string {
	if len(names) == 0 {
		return string(os.PathSeparator)
	}
	return strings.Join(names, string(os.PathSeparator))
}

func nameWithSuffix(name string, number int) string {
	if number == 0 {
		return name
	}
	return fmt.Sprintf("%s%d", name, number)
}

func validateOneAdditionalLayerPath(target string) error {
	for _, p := range []string{
		filepath.Join(target, "diff"),
		filepath.Join(target, "info"),
		filepath.Join(target, "blob"),
	} {
		if err := fileutils.Exists(p); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) getAdditionalLayerPath(tocDigest digest.Digest, ref string) (string, error) {
	refElem := base64.StdEncoding.EncodeToString([]byte(ref))
	for _, ls := range d.options.layerStores {
		ref := ""
		if ls.withReference {
			ref = refElem
		}
		target := path.Join(ls.path, ref, tocDigest.String())
		err := validateOneAdditionalLayerPath(target)
		if err == nil {
			return target, nil
		}
		logrus.Debugf("additional Layer Store %v failed to stat additional layer: %v", ls, err)
	}

	return "", fmt.Errorf("additional layer (%q, %q) not found: %w", tocDigest, ref, graphdriver.ErrLayerUnknown)
}

func (d *Driver) releaseAdditionalLayerByID(id string) {
	if al, err := d.getAdditionalLayerPathByID(id); err == nil {
		notifyReleaseAdditionalLayer(al)
	} else if !os.IsNotExist(err) {
		logrus.Warnf("Unexpected error on reading Additional Layer Store pointer %v", err)
	}
}

// additionalLayer represents a layer in Additional Layer Store.
type additionalLayer struct {
	path        string
	d           *Driver
	releaseOnce sync.Once
}

// Info returns arbitrary information stored along with this layer (i.e. `info` file).
// This API is experimental and can be changed without bumping the major version number.
// TODO: to remove the comment once it's no longer experimental.
func (al *additionalLayer) Info() (io.ReadCloser, error) {
	return os.Open(filepath.Join(al.path, "info"))
}

// Blob returns a reader of the raw contents of this layer.
func (al *additionalLayer) Blob() (io.ReadCloser, error) {
	return os.Open(filepath.Join(al.path, "blob"))
}

// CreateAs creates a new layer from this additional layer.
// This API is experimental and can be changed without bumping the major version number.
// TODO: to remove the comment once it's no longer experimental.
func (al *additionalLayer) CreateAs(id, parent string) error {
	// TODO: support opts
	if err := al.d.Create(id, parent, nil); err != nil {
		return err
	}
	dir := al.d.dir(id)
	diffDir := path.Join(dir, "diff")
	if err := os.RemoveAll(diffDir); err != nil {
		return err
	}
	// tell the additional layer store that we use this layer.
	// mark this layer as "additional layer"
	if err := os.WriteFile(path.Join(dir, "additionallayer"), []byte(al.path), 0o644); err != nil {
		return err
	}
	notifyUseAdditionalLayer(al.path)
	return os.Symlink(filepath.Join(al.path, "diff"), diffDir)
}

func (d *Driver) getAdditionalLayerPathByID(id string) (string, error) {
	al, err := os.ReadFile(path.Join(d.dir(id), "additionallayer"))
	if err != nil {
		return "", err
	}
	return string(al), nil
}

// Release tells the additional layer store that we don't use this handler.
// This API is experimental and can be changed without bumping the major version number.
// TODO: to remove the comment once it's no longer experimental.
func (al *additionalLayer) Release() {
	// Tell the additional layer store that we don't use this layer handler.
	// This will decrease the reference counter on the store's side, which was
	// increased in LookupAdditionalLayer (so this must be called only once).
	al.releaseOnce.Do(func() {
		notifyReleaseAdditionalLayer(al.path)
	})
}

// notifyUseAdditionalLayer notifies Additional Layer Store that we use the specified layer.
// This is done by creating "use" file in the layer directory. This is useful for
// Additional Layer Store to consider when to perform GC. Notification-aware Additional
// Layer Store must return ENOENT.
func notifyUseAdditionalLayer(al string) {
	if !path.IsAbs(al) {
		logrus.Warnf("additionallayer must be absolute (got: %v)", al)
		return
	}
	useFile := path.Join(al, "use")
	f, err := os.Create(useFile)
	if os.IsNotExist(err) {
		return
	} else if err == nil {
		f.Close()
		if err := os.Remove(useFile); err != nil {
			logrus.Warnf("Failed to remove use file")
		}
	}
	logrus.Warnf("Unexpected error by Additional Layer Store %v during use; GC doesn't seem to be supported", err)
}

// notifyReleaseAdditionalLayer notifies Additional Layer Store that we don't use the specified
// layer anymore. This is done by rmdir-ing the layer directory. This is useful for
// Additional Layer Store to consider when to perform GC. Notification-aware Additional
// Layer Store must return ENOENT.
func notifyReleaseAdditionalLayer(al string) {
	if !path.IsAbs(al) {
		logrus.Warnf("additionallayer must be absolute (got: %v)", al)
		return
	}
	// tell the additional layer store that we don't use this layer anymore.
	err := unix.Rmdir(al)
	if os.IsNotExist(err) {
		return
	}
	logrus.Warnf("Unexpected error by Additional Layer Store %v during release; GC doesn't seem to be supported", err)
}

// redirectDiffIfAdditionalLayer checks if the passed diff path is Additional Layer and
// returns the redirected path. If the passed diff is not the one in Additional Layer
// Store, it returns the original path without changes.
func redirectDiffIfAdditionalLayer(diffPath string, checkExistence bool) (string, error) {
	if ld, err := os.Readlink(diffPath); err == nil {
		// diff is the link to Additional Layer Store
		if !path.IsAbs(ld) {
			return "", fmt.Errorf("linkpath must be absolute (got: %q)", ld)
		}
		if checkExistence {
			if err := fileutils.Exists(ld); err != nil {
				return "", fmt.Errorf("failed to access to the linked additional layer: %w", err)
			}
		}
		diffPath = ld
	} else if err.(*os.PathError).Err != syscall.EINVAL {
		return "", err
	}
	return diffPath, nil
}

// getMappedMountRoot is a heuristic that calculates the parent directory where
// the idmapped mount should be applied.
// It is useful to minimize the number of idmapped mounts and at the same time use
// a common path as long as possible to reduce the length of the mount data argument.
func getMappedMountRoot(path string) string {
	dirName := filepath.Dir(path)
	if filepath.Base(dirName) == linkDir {
		return filepath.Dir(dirName)
	}
	return dirName
}

// Dedup performs deduplication of the driver's storage.
func (d *Driver) Dedup(req graphdriver.DedupArgs) (graphdriver.DedupResult, error) {
	var dirs []string
	for _, layer := range req.Layers {
		dir, _, inAdditionalStore := d.dir2(layer, false)
		if inAdditionalStore {
			continue
		}
		if err := fileutils.Exists(dir); err == nil {
			dirs = append(dirs, filepath.Join(dir, "diff"))
		}
	}
	r, err := dedup.DedupDirs(dirs, req.Options)
	if err != nil {
		return graphdriver.DedupResult{}, err
	}
	return graphdriver.DedupResult{Deduped: r.Deduped}, nil
}
