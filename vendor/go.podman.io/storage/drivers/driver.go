package graphdriver

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"github.com/vbatts/tar-split/tar/storage"
	"go.podman.io/storage/internal/dedup"
	"go.podman.io/storage/internal/tempdir"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/directory"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
)

// FsMagic unsigned id of the filesystem in use.
type FsMagic uint32

const (
	// FsMagicUnsupported is a predefined constant value other than a valid filesystem id.
	FsMagicUnsupported = FsMagic(0x00000000)
)

var (
	// All registered drivers
	drivers map[string]InitFunc

	// ErrNotSupported returned when driver is not supported.
	ErrNotSupported = errors.New("driver not supported")
	// ErrPrerequisites returned when driver does not meet prerequisites.
	ErrPrerequisites = errors.New("prerequisites for driver not satisfied (wrong filesystem?)")
	// ErrIncompatibleFS returned when file system is not supported.
	ErrIncompatibleFS = errors.New("backing file system is unsupported for this graph driver")
	// ErrLayerUnknown returned when the specified layer is unknown by the driver.
	ErrLayerUnknown = errors.New("unknown layer")
)

// CreateOpts contains optional arguments for Create() and CreateReadWrite()
// methods.
type CreateOpts struct {
	MountLabel string
	StorageOpt map[string]string
	*idtools.IDMappings
	ignoreChownErrors bool
}

// MountOpts contains optional arguments for Driver.Get() methods.
type MountOpts struct {
	// Mount label is the MAC Labels to assign to mount point (SELINUX)
	MountLabel string
	// UidMaps & GidMaps are the User Namespace mappings to be assigned to content in the mount point
	UidMaps []idtools.IDMap //nolint: revive
	GidMaps []idtools.IDMap //nolint: revive
	Options []string

	// Volatile specifies whether the container storage can be optimized
	// at the cost of not syncing all the dirty files in memory.
	Volatile bool

	// DisableShifting forces the driver to not do any ID shifting at runtime.
	DisableShifting bool
}

// ApplyDiffOpts contains optional arguments for ApplyDiff methods.
type ApplyDiffOpts struct {
	Diff              io.Reader
	Mappings          *idtools.IDMappings
	MountLabel        string
	IgnoreChownErrors bool
	ForceMask         *os.FileMode
}

// ApplyDiffWithDifferOpts contains optional arguments for ApplyDiffWithDiffer methods.
type ApplyDiffWithDifferOpts struct {
	ApplyDiffOpts

	Flags map[string]any
}

// DedupArgs contains the information to perform storage deduplication.
type DedupArgs struct {
	// Layers is the list of layers to deduplicate.
	Layers []string

	// Options that are passed directly to the pkg/dedup.DedupDirs function.
	Options dedup.DedupOptions
}

// DedupResult contains the result of the Dedup() call.
type DedupResult struct {
	// Deduped represents the total number of bytes saved by deduplication.
	// This value accounts also for all previously deduplicated data, not only the savings
	// from the last run.
	Deduped uint64
}

// InitFunc initializes the storage driver.
type InitFunc func(homedir string, options Options) (Driver, error)

// ProtoDriver defines the basic capabilities of a driver.
// This interface exists solely to be a minimum set of methods
// for client code which choose not to implement the entire Driver
// interface and use the NaiveDiffDriver wrapper constructor.
//
// Use of ProtoDriver directly by client code is not recommended.
type ProtoDriver interface {
	// String returns a string representation of this driver.
	String() string
	// CreateReadWrite creates a new, empty filesystem layer that is ready
	// to be used as the storage for a container. Additional options can
	// be passed in opts. parent may be "" and opts may be nil.
	CreateReadWrite(id, parent string, opts *CreateOpts) error
	// Create creates a new, empty, filesystem layer with the
	// specified id and parent and options passed in opts. Parent
	// may be "" and opts may be nil.
	Create(id, parent string, opts *CreateOpts) error
	// CreateFromTemplate creates a new filesystem layer with the specified id
	// and parent, with contents identical to the specified template layer.
	CreateFromTemplate(id, template string, templateIDMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, opts *CreateOpts, readWrite bool) error
	// Remove attempts to remove the filesystem layer with this id.
	// This is soft-deprecated and should not get any new callers; use DeferredRemove.
	Remove(id string) error
	// DeferredRemove is used to remove the filesystem layer with this id.
	// This removal happen immediately (the layer is no longer usable),
	// but physically deleting the files may be deferred.
	// Caller MUST call returned Cleanup function EVEN IF the function returns an error.
	DeferredRemove(id string) (tempdir.CleanupTempDirFunc, error)
	// GetTempDirRootDirs returns the root directories for temporary directories.
	// Multiple directories may be returned when drivers support different filesystems
	// for layers (e.g., overlay with imageStore vs home directory).
	GetTempDirRootDirs() []string
	// Get returns the mountpoint for the layered filesystem referred
	// to by this id. You can optionally specify a mountLabel or "".
	// Optionally it gets the mappings used to create the layer.
	// Returns the absolute path to the mounted layered filesystem.
	Get(id string, options MountOpts) (dir string, err error)
	// Put releases the system resources for the specified id,
	// e.g, unmounting layered filesystem.
	Put(id string) error
	// Exists returns whether a filesystem layer with the specified
	// ID exists on this driver.
	Exists(id string) bool
	// Returns a list of layer ids that exist on this driver (does not include
	// additional storage layers). Not supported by all backends.
	// If the driver requires that layers be removed in a particular order,
	// usually due to parent-child relationships that it cares about, The
	// list should be sorted well enough so that if all layers need to be
	// removed, they can be removed in the order in which they're returned.
	ListLayers() ([]string, error)
	// Status returns a set of key-value pairs which give low
	// level diagnostic status about this driver.
	Status() [][2]string
	// Returns a set of key-value pairs which give low level information
	// about the image/container driver is managing.
	Metadata(id string) (map[string]string, error)
	// ReadWriteDiskUsage returns the disk usage of the writable directory for the specified ID.
	ReadWriteDiskUsage(id string) (*directory.DiskUsage, error)
	// Cleanup performs necessary tasks to release resources
	// held by the driver, e.g., unmounting all layered filesystems
	// known to this driver.
	Cleanup() error
	// AdditionalImageStores returns additional image stores supported by the driver
	// This API is experimental and can be changed without bumping the major version number.
	AdditionalImageStores() []string
	// Dedup performs deduplication of the driver's storage.
	Dedup(DedupArgs) (DedupResult, error)
}

// DiffDriver is the interface to use to implement graph diffs
type DiffDriver interface {
	// Diff produces an archive of the changes between the specified
	// layer and its parent layer which may be "".
	Diff(id string, idMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, mountLabel string) (io.ReadCloser, error)
	// Changes produces a list of changes between the specified layer
	// and its parent layer. If parent is "", then all changes will be ADD changes.
	Changes(id string, idMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, mountLabel string) ([]archive.Change, error)
	// ApplyDiff extracts the changeset from the given diff into the
	// layer with the specified id and parent, returning the size of the
	// new layer in bytes.
	// The io.Reader must be an uncompressed stream.
	ApplyDiff(id string, parent string, options ApplyDiffOpts) (size int64, err error)
	// DiffSize calculates the changes between the specified id
	// and its parent and returns the size in bytes of the changes
	// relative to its base filesystem directory.
	DiffSize(id string, idMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, mountLabel string) (size int64, err error)
}

// LayerIDMapUpdater is the interface that implements ID map changes for layers.
type LayerIDMapUpdater interface {
	// UpdateLayerIDMap walks the layer's filesystem tree, changing the ownership
	// information using the toContainer and toHost mappings, using them to replace
	// on-disk owner UIDs and GIDs which are "host" values in the first map with
	// UIDs and GIDs for "host" values from the second map which correspond to the
	// same "container" IDs.  This method should only be called after a layer is
	// first created and populated, and before it is mounted, as other changes made
	// relative to a parent layer, but before this method is called, may be discarded
	// by Diff().
	UpdateLayerIDMap(id string, toContainer, toHost *idtools.IDMappings, mountLabel string) error

	// SupportsShifting tells whether the driver support shifting of the UIDs/GIDs in a
	// image to the provided mapping and it is not required to Chown the files when running in
	// an user namespace.
	SupportsShifting(uidmap, gidmap []idtools.IDMap) bool
}

// Driver is the interface for layered/snapshot file system drivers.
type Driver interface {
	ProtoDriver
	DiffDriver
	LayerIDMapUpdater
}

// DriverWithDifferOutput is the result of ApplyDiffWithDiffer
// This API is experimental and can be changed without bumping the major version number.
type DriverWithDifferOutput struct {
	Differ             Differ
	Target             string
	Size               int64 // Size of the uncompressed layer, -1 if unknown. Must be known if UncompressedDigest is set.
	UIDs               []uint32
	GIDs               []uint32
	UncompressedDigest digest.Digest
	CompressedDigest   digest.Digest
	Metadata           string
	BigData            map[string][]byte
	// TarSplit is owned by the [DriverWithDifferOutput], and must be closed by calling one of
	// [Store.ApplyStagedLayer]/[Store.CleanupStagedLayer].  It is nil if not available.
	TarSplit  *os.File
	TOCDigest digest.Digest
	// RootDirMode is the mode of the root directory of the layer, if specified.
	RootDirMode *os.FileMode
	// Artifacts is a collection of additional artifacts
	// generated by the differ that the storage driver can use.
	Artifacts map[string]any
}

type DifferOutputFormat int

const (
	// DifferOutputFormatDir means the output is a directory and it will
	// keep the original layout.
	DifferOutputFormatDir = iota
	// DifferOutputFormatFlat will store the files by their checksum, per
	// pkg/chunked/internal/composefs.RegularFilePathForValidatedDigest.
	DifferOutputFormatFlat
)

// DifferFsVerity is a part of the experimental Differ interface and should not be used from outside of c/storage.
// It configures the fsverity requirement.
type DifferFsVerity int

const (
	// DifferFsVerityDisabled means no fs-verity is used
	DifferFsVerityDisabled = iota

	// DifferFsVerityIfAvailable means fs-verity is used when supported by
	// the underlying kernel and filesystem.
	DifferFsVerityIfAvailable

	// DifferFsVerityRequired means fs-verity is required.  Note this is not
	// currently set or exposed by the overlay driver.
	DifferFsVerityRequired
)

// DifferOptions is a part of the experimental Differ interface and should not be used from outside of c/storage.
// It overrides how the differ works.
type DifferOptions struct {
	// Format defines the destination directory layout format
	Format DifferOutputFormat

	// UseFsVerity defines whether fs-verity is used
	UseFsVerity DifferFsVerity
}

// Differ defines the interface for using a custom differ.
// This API is experimental and can be changed without bumping the major version number.
type Differ interface {
	ApplyDiff(dest string, options *archive.TarOptions, differOpts *DifferOptions) (DriverWithDifferOutput, error)
	Close() error
}

// DriverWithDiffer is the interface for direct diff access.
// This API is experimental and can be changed without bumping the major version number.
type DriverWithDiffer interface {
	Driver
	// ApplyDiffWithDiffer applies the changes using the callback function.
	// The staging directory created by this function is guaranteed to be usable with ApplyDiffFromStagingDirectory.
	ApplyDiffWithDiffer(options *ApplyDiffWithDifferOpts, differ Differ) (output DriverWithDifferOutput, err error)
	// ApplyDiffFromStagingDirectory applies the changes using the diffOutput target directory.
	ApplyDiffFromStagingDirectory(id, parent string, diffOutput *DriverWithDifferOutput, options *ApplyDiffWithDifferOpts) error
	// CleanupStagingDirectory cleanups the staging directory.  It can be used to cleanup the staging directory on errors
	CleanupStagingDirectory(stagingDirectory string) error
	// DifferTarget gets the location where files are stored for the layer.
	DifferTarget(id string) (string, error)
}

// Capabilities defines a list of capabilities a driver may implement.
// These capabilities are not required; however, they do determine how a
// graphdriver can be used.
type Capabilities struct {
	// Flags that this driver is capable of reproducing exactly equivalent
	// diffs for read-only layers. If set, clients can rely on the driver
	// for consistent tar streams, and avoid extra processing to account
	// for potential differences (eg: the layer store's use of tar-split).
	ReproducesExactDiffs bool
}

// CapabilityDriver is the interface for layered file system drivers that
// can report on their Capabilities.
type CapabilityDriver interface {
	Capabilities() Capabilities
}

// AdditionalLayer represents a layer that is stored in the additional layer store
// This API is experimental and can be changed without bumping the major version number.
type AdditionalLayer interface {
	// CreateAs creates a new layer from this additional layer
	CreateAs(id, parent string) error

	// Info returns arbitrary information stored along with this layer (i.e. `info` file)
	Info() (io.ReadCloser, error)

	// Blob returns a reader of the raw contents of this layer.
	Blob() (io.ReadCloser, error)

	// Release tells the additional layer store that we don't use this handler.
	Release()
}

// AdditionalLayerStoreDriver is the interface for driver that supports
// additional layer store functionality.
// This API is experimental and can be changed without bumping the major version number.
type AdditionalLayerStoreDriver interface {
	Driver

	// LookupAdditionalLayer looks up additional layer store by the specified
	// TOC digest and ref and returns an object representing that layer.
	LookupAdditionalLayer(tocDigest digest.Digest, ref string) (AdditionalLayer, error)

	// LookupAdditionalLayer looks up additional layer store by the specified
	// ID and returns an object representing that layer.
	LookupAdditionalLayerByID(id string) (AdditionalLayer, error)
}

// DiffGetterDriver is the interface for layered file system drivers that
// provide a specialized function for getting file contents for tar-split.
type DiffGetterDriver interface {
	Driver
	// DiffGetter returns an interface to efficiently retrieve the contents
	// of files in a layer.
	DiffGetter(id string) (FileGetCloser, error)
}

// FileGetCloser extends the storage.FileGetter interface with a Close method
// for cleaning up.
type FileGetCloser interface {
	storage.FileGetter
	// Close cleans up any resources associated with the FileGetCloser.
	Close() error
}

// Checker makes checks on specified filesystems.
type Checker interface {
	// IsMounted returns true if the provided path is mounted for the specific checker
	IsMounted(path string) bool
}

func init() {
	drivers = make(map[string]InitFunc)
}

// MustRegister registers an InitFunc for the driver, or panics.
// It is suitable for package’s init() sections.
func MustRegister(name string, initFunc InitFunc) {
	if err := Register(name, initFunc); err != nil {
		panic(fmt.Sprintf("failed to register containers/storage graph driver %q: %v", name, err))
	}
}

// Register registers an InitFunc for the driver.
func Register(name string, initFunc InitFunc) error {
	if _, exists := drivers[name]; exists {
		return fmt.Errorf("name already registered %s", name)
	}
	drivers[name] = initFunc

	return nil
}

// GetDriver initializes and returns the registered driver
func GetDriver(name string, config Options) (Driver, error) {
	if initFunc, exists := drivers[name]; exists {
		return initFunc(filepath.Join(config.Root, name), config)
	}

	logrus.Errorf("Failed to GetDriver graph %s %s", name, config.Root)
	return nil, fmt.Errorf("failed to GetDriver graph %s %s: %w", name, config.Root, ErrNotSupported)
}

// getBuiltinDriver initializes and returns the registered driver, but does not try to load from plugins
func getBuiltinDriver(name, home string, options Options) (Driver, error) {
	if initFunc, exists := drivers[name]; exists {
		return initFunc(filepath.Join(home, name), options)
	}
	logrus.Errorf("Failed to built-in GetDriver graph %s %s", name, home)
	return nil, fmt.Errorf("failed to built-in GetDriver graph %s %s: %w", name, home, ErrNotSupported)
}

// Options is used to initialize a graphdriver
type Options struct {
	Root                string
	RunRoot             string
	ImageStore          string
	DriverPriority      []string
	DriverOptions       []string
	ExperimentalEnabled bool
}

// New creates the driver and initializes it at the specified root.
func New(name string, config Options) (Driver, error) {
	if name != "" {
		logrus.Debugf("[graphdriver] trying provided driver %q", name) // so the logs show specified driver
		return GetDriver(name, config)
	}

	// Guess for prior driver
	driversMap := ScanPriorDrivers(config.Root)

	// use the supplied priority list unless it is empty
	prioList := config.DriverPriority
	if len(prioList) == 0 {
		prioList = Priority
	}

	for _, name := range prioList {
		if name == "vfs" && len(config.DriverPriority) == 0 {
			// don't use vfs even if there is state present and vfs
			// has not been explicitly added to the override driver
			// priority list
			continue
		}
		if _, prior := driversMap[name]; prior {
			// of the state found from prior drivers, check in order of our priority
			// which we would prefer
			driver, err := getBuiltinDriver(name, config.Root, config)
			if err != nil {
				// unlike below, we will return error here, because there is prior
				// state, and now it is no longer supported/prereq/compatible, so
				// something changed and needs attention. Otherwise the daemon's
				// images would just "disappear".
				logrus.Errorf("[graphdriver] prior storage driver %s failed: %s", name, err)
				return nil, err
			}

			// abort starting when there are other prior configured drivers
			// to ensure the user explicitly selects the driver to load
			if len(driversMap)-1 > 0 {
				var driversSlice []string
				for name := range driversMap {
					driversSlice = append(driversSlice, name)
				}

				return nil, fmt.Errorf("%s contains several valid graphdrivers: %s; Please cleanup or explicitly choose storage driver (-s <DRIVER>)", config.Root, strings.Join(driversSlice, ", "))
			}

			logrus.Infof("[graphdriver] using prior storage driver: %s", name)
			return driver, nil
		}
	}

	// Check for priority drivers first
	for _, name := range prioList {
		driver, err := getBuiltinDriver(name, config.Root, config)
		if err != nil {
			if isDriverNotSupported(err) {
				continue
			}
			return nil, err
		}
		return driver, nil
	}

	// Check all registered drivers if no priority driver is found
	for name, initFunc := range drivers {
		driver, err := initFunc(filepath.Join(config.Root, name), config)
		if err != nil {
			if isDriverNotSupported(err) {
				continue
			}
			return nil, err
		}
		return driver, nil
	}
	return nil, fmt.Errorf("no supported storage backend found")
}

// isDriverNotSupported returns true if the error initializing
// the graph driver is a non-supported error.
func isDriverNotSupported(err error) bool {
	return errors.Is(err, ErrNotSupported) || errors.Is(err, ErrPrerequisites) || errors.Is(err, ErrIncompatibleFS)
}

// scanPriorDrivers returns an un-ordered scan of directories of prior storage drivers
func ScanPriorDrivers(root string) map[string]bool {
	driversMap := make(map[string]bool)

	for driver := range drivers {
		p := filepath.Join(root, driver)
		if err := fileutils.Exists(p); err == nil {
			driversMap[driver] = true
		}
	}
	return driversMap
}

// driverPut is driver.Put, but errors are handled either by updating mainErr or just logging.
// Typical usage:
//
//	func …(…) (err error) {
//		…
//		defer driverPut(driver, id, &err)
//	}
func driverPut(driver ProtoDriver, id string, mainErr *error) {
	if err := driver.Put(id); err != nil {
		err = fmt.Errorf("unmounting layer %s: %w", id, err)
		if *mainErr == nil {
			*mainErr = err
		} else {
			logrus.Error(err)
		}
	}
}
