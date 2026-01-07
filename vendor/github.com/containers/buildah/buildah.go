package buildah

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/containers/buildah/define"
	"github.com/containers/buildah/docker"
	encconfig "github.com/containers/ocicrypt/config"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	nettypes "go.podman.io/common/libnetwork/types"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/ioutils"
)

const (
	// Package is the name of this package, used in help output and to
	// identify working containers.
	Package = define.Package
	// Version for the Package.
	Version = define.Version
	// The value we use to identify what type of information, currently a
	// serialized Builder structure, we are using as per-container state.
	// This should only be changed when we make incompatible changes to
	// that data structure, as it's used to distinguish containers which
	// are "ours" from ones that aren't.
	containerType = Package + " 0.0.1"
	// The file in the per-container directory which we use to store our
	// per-container state.  If it isn't there, then the container isn't
	// one of our build containers.
	stateFile = Package + ".json"
)

// PullPolicy takes the value PullIfMissing, PullAlways, PullIfNewer, or PullNever.
type PullPolicy = define.PullPolicy

const (
	// PullIfMissing is one of the values that BuilderOptions.PullPolicy
	// can take, signalling that the source image should be pulled from a
	// registry if a local copy of it is not already present.
	PullIfMissing = define.PullIfMissing
	// PullAlways is one of the values that BuilderOptions.PullPolicy can
	// take, signalling that a fresh, possibly updated, copy of the image
	// should be pulled from a registry before the build proceeds.
	PullAlways = define.PullAlways
	// PullIfNewer is one of the values that BuilderOptions.PullPolicy
	// can take, signalling that the source image should only be pulled
	// from a registry if a local copy is not already present or if a
	// newer version the image is present on the repository.
	PullIfNewer = define.PullIfNewer
	// PullNever is one of the values that BuilderOptions.PullPolicy can
	// take, signalling that the source image should not be pulled from a
	// registry if a local copy of it is not already present.
	PullNever = define.PullNever
)

// NetworkConfigurationPolicy takes the value NetworkDefault, NetworkDisabled,
// or NetworkEnabled.
type NetworkConfigurationPolicy = define.NetworkConfigurationPolicy

const (
	// NetworkDefault is one of the values that BuilderOptions.ConfigureNetwork
	// can take, signalling that the default behavior should be used.
	NetworkDefault = define.NetworkDefault
	// NetworkDisabled is one of the values that BuilderOptions.ConfigureNetwork
	// can take, signalling that network interfaces should NOT be configured for
	// newly-created network namespaces.
	NetworkDisabled = define.NetworkDisabled
	// NetworkEnabled is one of the values that BuilderOptions.ConfigureNetwork
	// can take, signalling that network interfaces should be configured for
	// newly-created network namespaces.
	NetworkEnabled = define.NetworkEnabled
)

// Builder objects are used to represent containers which are being used to
// build images.  They also carry potential updates which will be applied to
// the image's configuration when the container's contents are used to build an
// image.
type Builder struct {
	store storage.Store

	// Logger is the logrus logger to write log messages with
	Logger *logrus.Logger `json:"-"`

	// Args define variables that users can pass at build-time to the builder.
	Args map[string]string
	// Type is used to help identify a build container's metadata.  It
	// should not be modified.
	Type string `json:"type"`
	// FromImage is the name of the source image which was used to create
	// the container, if one was used.  It should not be modified.
	FromImage string `json:"image,omitempty"`
	// FromImageID is the ID of the source image which was used to create
	// the container, if one was used.  It should not be modified.
	FromImageID string `json:"image-id"`
	// FromImageDigest is the digest of the source image which was used to
	// create the container, if one was used.  It should not be modified.
	FromImageDigest string `json:"image-digest"`
	// Config is the source image's configuration.  It should not be
	// modified.
	Config []byte `json:"config,omitempty"`
	// Manifest is the source image's manifest.  It should not be modified.
	Manifest []byte `json:"manifest,omitempty"`

	// Container is the name of the build container.  It should not be modified.
	Container string `json:"container-name,omitempty"`
	// ContainerID is the ID of the build container.  It should not be modified.
	ContainerID string `json:"container-id,omitempty"`
	// MountPoint is the last location where the container's root
	// filesystem was mounted.  It should not be modified.
	MountPoint string `json:"mountpoint,omitempty"`
	// ProcessLabel is the SELinux process label to use during subsequent Run() calls.
	ProcessLabel string `json:"process-label,omitempty"`
	// MountLabel is the SELinux mount label associated with the container
	MountLabel string `json:"mount-label,omitempty"`

	// ImageAnnotations is a set of key-value pairs which is stored in the
	// image's manifest.
	ImageAnnotations map[string]string `json:"annotations,omitempty"`
	// ImageCreatedBy is a description of how this container was built.
	ImageCreatedBy string `json:"created-by,omitempty"`
	// ImageHistoryComment is a description of how our added layers were built.
	ImageHistoryComment string `json:"history-comment,omitempty"`

	// Image metadata and runtime settings, in multiple formats.
	OCIv1  v1.Image       `json:"ociv1"`
	Docker docker.V2Image `json:"docker"`
	// DefaultMountsFilePath is the file path holding the mounts to be mounted in "host-path:container-path" format.
	DefaultMountsFilePath string `json:"defaultMountsFilePath,omitempty"`

	// Isolation controls how we handle "RUN" statements and the Run() method.
	Isolation define.Isolation
	// NamespaceOptions controls how we set up the namespaces for processes that we Run().
	NamespaceOptions define.NamespaceOptions
	// ConfigureNetwork controls whether or not network interfaces and
	// routing are configured for a new network namespace (i.e., when not
	// joining another's namespace and not just using the host's
	// namespace), effectively deciding whether or not the process has a
	// usable network.
	ConfigureNetwork define.NetworkConfigurationPolicy
	// CNIPluginPath is the location of CNI plugin helpers, if they should be
	// run from a location other than the default location.
	CNIPluginPath string
	// CNIConfigDir is the location of CNI configuration files, if the files in
	// the default configuration directory shouldn't be used.
	CNIConfigDir string

	// NetworkInterface is the libnetwork network interface used to setup CNI or netavark networks.
	NetworkInterface nettypes.ContainerNetwork `json:"-"`

	// GroupAdd is a list of groups to add to the primary process when Run() is
	// called. The magic 'keep-groups' value indicates that the process should
	// be allowed to inherit the current set of supplementary groups.
	GroupAdd []string
	// ID mapping options to use when running processes with non-host user namespaces.
	IDMappingOptions define.IDMappingOptions
	// Capabilities is a list of capabilities to use when running commands in the container.
	Capabilities []string
	// PrependedEmptyLayers are history entries that we'll add to a
	// committed image, after any history items that we inherit from a base
	// image, but before the history item for the layer that we're
	// committing.
	PrependedEmptyLayers []v1.History
	// AppendedEmptyLayers are history entries that we'll add to a
	// committed image after the history item for the layer that we're
	// committing.
	AppendedEmptyLayers []v1.History
	CommonBuildOpts     *define.CommonBuildOptions
	// TopLayer is the top layer of the image
	TopLayer string
	// Format to use for a container image we eventually commit, when we do.
	Format string
	// TempVolumes are temporary mount points created during Run() calls.
	// Deprecated: do not use.
	TempVolumes map[string]bool
	// ContentDigester counts the digest of all Add()ed content since it was
	// last restarted.
	ContentDigester CompositeDigester
	// Devices are parsed additional devices to provide to Run() calls.
	Devices define.ContainerDevices
	// DeviceSpecs are unparsed additional devices to provide to Run() calls.
	DeviceSpecs []string
	// CDIConfigDir is the location of CDI configuration files, if the files in
	// the default configuration locations shouldn't be used.
	CDIConfigDir string
	// PrependedLinkedLayers and AppendedLinkedLayers are combinations of
	// history entries and locations of either directory trees (if
	// directories, per os.Stat()) or uncompressed layer blobs which should
	// be added to the image at commit-time.  The order of these relative
	// to PrependedEmptyLayers and AppendedEmptyLayers in the committed
	// image is not guaranteed.
	PrependedLinkedLayers, AppendedLinkedLayers []LinkedLayer
}

// BuilderInfo are used as objects to display container information
type BuilderInfo struct {
	Type                  string
	FromImage             string
	FromImageID           string
	FromImageDigest       string
	GroupAdd              []string
	Config                string
	Manifest              string
	Container             string
	ContainerID           string
	MountPoint            string
	ProcessLabel          string
	MountLabel            string
	ImageAnnotations      map[string]string
	ImageCreatedBy        string
	OCIv1                 v1.Image
	Docker                docker.V2Image
	DefaultMountsFilePath string
	Isolation             string
	NamespaceOptions      define.NamespaceOptions
	Capabilities          []string
	ConfigureNetwork      string
	CNIPluginPath         string
	CNIConfigDir          string
	IDMappingOptions      define.IDMappingOptions
	History               []v1.History
	Devices               define.ContainerDevices
	DeviceSpecs           []string
	CDIConfigDir          string
}

// GetBuildInfo gets a pointer to a Builder object and returns a BuilderInfo object from it.
// This is used in the inspect command to display Manifest and Config as string and not []byte.
func GetBuildInfo(b *Builder) BuilderInfo {
	history := copyHistory(b.OCIv1.History)
	history = append(history, copyHistory(b.PrependedEmptyLayers)...)
	history = append(history, copyHistory(b.AppendedEmptyLayers)...)
	sort.Strings(b.Capabilities)
	return BuilderInfo{
		Type:                  b.Type,
		FromImage:             b.FromImage,
		FromImageID:           b.FromImageID,
		FromImageDigest:       b.FromImageDigest,
		Config:                string(b.Config),
		Manifest:              string(b.Manifest),
		Container:             b.Container,
		ContainerID:           b.ContainerID,
		GroupAdd:              b.GroupAdd,
		MountPoint:            b.MountPoint,
		ProcessLabel:          b.ProcessLabel,
		MountLabel:            b.MountLabel,
		ImageAnnotations:      b.ImageAnnotations,
		ImageCreatedBy:        b.ImageCreatedBy,
		OCIv1:                 b.OCIv1,
		Docker:                b.Docker,
		DefaultMountsFilePath: b.DefaultMountsFilePath,
		Isolation:             b.Isolation.String(),
		NamespaceOptions:      b.NamespaceOptions,
		ConfigureNetwork:      fmt.Sprintf("%v", b.ConfigureNetwork),
		CNIPluginPath:         b.CNIPluginPath,
		CNIConfigDir:          b.CNIConfigDir,
		IDMappingOptions:      b.IDMappingOptions,
		Capabilities:          b.Capabilities,
		History:               history,
		Devices:               b.Devices,
		DeviceSpecs:           b.DeviceSpecs,
		CDIConfigDir:          b.CDIConfigDir,
	}
}

// CommonBuildOptions are resources that can be defined by flags for both buildah from and build
type CommonBuildOptions = define.CommonBuildOptions

// BuilderOptions are used to initialize a new Builder.
type BuilderOptions struct {
	// Args define variables that users can pass at build-time to the builder
	Args map[string]string
	// FromImage is the name of the image which should be used as the
	// starting point for the container.  It can be set to an empty value
	// or "scratch" to indicate that the container should not be based on
	// an image.
	FromImage string
	// ContainerSuffix is the suffix to add for generated container names
	ContainerSuffix string
	// Container is a desired name for the build container.
	Container string
	// PullPolicy decides whether or not we should pull the image that
	// we're using as a base image.  It should be PullIfMissing,
	// PullAlways, or PullNever.
	PullPolicy define.PullPolicy
	// Registry is a value which is prepended to the image's name, if it
	// needs to be pulled and the image name alone can not be resolved to a
	// reference to a source image.  No separator is implicitly added.
	Registry string
	// BlobDirectory is the name of a directory in which we'll attempt
	// to store copies of layer blobs that we pull down, if any.  It should
	// already exist.
	BlobDirectory string
	GroupAdd      []string
	// Logger is the logrus logger to write log messages with
	Logger *logrus.Logger `json:"-"`
	// Mount signals to NewBuilder() that the container should be mounted
	// immediately.
	Mount bool
	// SignaturePolicyPath specifies an override location for the signature
	// policy which should be used for verifying the new image as it is
	// being written.  Except in specific circumstances, no value should be
	// specified, indicating that the shared, system-wide default policy
	// should be used.
	SignaturePolicyPath string
	// ReportWriter is an io.Writer which will be used to log the reading
	// of the source image from a registry, if we end up pulling the image.
	ReportWriter io.Writer
	// github.com/containers/image/types SystemContext to hold credentials
	// and other authentication/authorization information.
	SystemContext *types.SystemContext
	// DefaultMountsFilePath is the file path holding the mounts to be
	// mounted in "host-path:container-path" format
	DefaultMountsFilePath string
	// Isolation controls how we handle "RUN" statements and the Run()
	// method.
	Isolation define.Isolation
	// NamespaceOptions controls how we set up namespaces for processes that
	// we might need to run using the container's root filesystem.
	NamespaceOptions define.NamespaceOptions
	// ConfigureNetwork controls whether or not network interfaces and
	// routing are configured for a new network namespace (i.e., when not
	// joining another's namespace and not just using the host's
	// namespace), effectively deciding whether or not the process has a
	// usable network.
	ConfigureNetwork define.NetworkConfigurationPolicy
	// CNIPluginPath is the location of CNI plugin helpers, if they should be
	// run from a location other than the default location.
	CNIPluginPath string
	// CNIConfigDir is the location of CNI configuration files, if the files in
	// the default configuration directory shouldn't be used.
	CNIConfigDir string

	// NetworkInterface is the libnetwork network interface used to setup CNI or netavark networks.
	NetworkInterface nettypes.ContainerNetwork `json:"-"`

	// ID mapping options to use if we're setting up our own user namespace.
	IDMappingOptions *define.IDMappingOptions
	// Capabilities is a list of capabilities to use when
	// running commands for Run().
	Capabilities    []string
	CommonBuildOpts *define.CommonBuildOptions
	// Format to use for a container image we eventually commit, when we do.
	Format string
	// Devices are additional parsed devices to provide for Run() calls.
	Devices define.ContainerDevices
	// DeviceSpecs are additional unparsed devices to provide for Run() calls.
	DeviceSpecs []string
	// DefaultEnv is deprecated and ignored.
	DefaultEnv []string
	// MaxPullRetries is the maximum number of attempts we'll make to pull
	// any one image from the external registry if the first attempt fails.
	MaxPullRetries int
	// PullRetryDelay is how long to wait before retrying a pull attempt.
	PullRetryDelay time.Duration
	// OciDecryptConfig contains the config that can be used to decrypt an image if it is
	// encrypted if non-nil. If nil, it does not attempt to decrypt an image.
	OciDecryptConfig *encconfig.DecryptConfig
	// ProcessLabel is the SELinux process label associated with commands we Run()
	ProcessLabel string
	// MountLabel is the SELinux mount label associated with the working container
	MountLabel string
	// PreserveBaseImageAnns indicates that we should preserve base
	// image information (Annotations) that are present in our base image,
	// rather than overwriting them with information about the base image
	// itself. Useful as an internal implementation detail of multistage
	// builds, and does not need to be set by most callers.
	PreserveBaseImageAnns bool
	// CDIConfigDir is the location of CDI configuration files, if the files in
	// the default configuration locations shouldn't be used.
	CDIConfigDir string
	// CompatScratchConfig controls whether a "scratch" image is created
	// with a truly empty configuration, as would have happened in the past
	// (when set to true), or with a minimal initial configuration which
	// has a working directory set in it.
	CompatScratchConfig types.OptionalBool
}

// ImportOptions are used to initialize a Builder from an existing container
// which was created elsewhere.
type ImportOptions struct {
	// Container is the name of the build container.
	Container string
	// SignaturePolicyPath specifies an override location for the signature
	// policy which should be used for verifying the new image as it is
	// being written.  Except in specific circumstances, no value should be
	// specified, indicating that the shared, system-wide default policy
	// should be used.
	SignaturePolicyPath string
}

// ImportFromImageOptions are used to initialize a Builder from an image.
type ImportFromImageOptions struct {
	// Image is the name or ID of the image we'd like to examine.
	Image string
	// SignaturePolicyPath specifies an override location for the signature
	// policy which should be used for verifying the new image as it is
	// being written.  Except in specific circumstances, no value should be
	// specified, indicating that the shared, system-wide default policy
	// should be used.
	SignaturePolicyPath string
	// github.com/containers/image/types SystemContext to hold information
	// about which registries we should check for completing image names
	// that don't include a domain portion.
	SystemContext *types.SystemContext
}

// ConfidentialWorkloadOptions encapsulates options which control whether or not
// we output an image whose rootfs contains a LUKS-compatibly-encrypted disk image
// instead of the usual rootfs contents.
type ConfidentialWorkloadOptions = define.ConfidentialWorkloadOptions

// SBOMScanOptions encapsulates options which control whether or not we run a
// scanner on the rootfs that we're about to commit, and how.
type SBOMScanOptions = define.SBOMScanOptions

// NewBuilder creates a new build container.
func NewBuilder(ctx context.Context, store storage.Store, options BuilderOptions) (*Builder, error) {
	if options.CommonBuildOpts == nil {
		options.CommonBuildOpts = &CommonBuildOptions{}
	}
	return newBuilder(ctx, store, options)
}

// ImportBuilder creates a new build configuration using an already-present
// container.
func ImportBuilder(ctx context.Context, store storage.Store, options ImportOptions) (*Builder, error) {
	return importBuilder(ctx, store, options)
}

// ImportBuilderFromImage creates a new builder configuration using an image.
// The returned object can be modified and examined, but it can not be saved
// or committed because it is not associated with a working container.
func ImportBuilderFromImage(ctx context.Context, store storage.Store, options ImportFromImageOptions) (*Builder, error) {
	return importBuilderFromImage(ctx, store, options)
}

// OpenBuilder loads information about a build container given its name or ID.
func OpenBuilder(store storage.Store, container string) (*Builder, error) {
	cdir, err := store.ContainerDirectory(container)
	if err != nil {
		return nil, err
	}
	buildstate, err := os.ReadFile(filepath.Join(cdir, stateFile))
	if err != nil {
		return nil, err
	}
	b := &Builder{}
	if err = json.Unmarshal(buildstate, &b); err != nil {
		return nil, fmt.Errorf("parsing %q, read from %q: %w", string(buildstate), filepath.Join(cdir, stateFile), err)
	}
	if b.Type != containerType {
		return nil, fmt.Errorf("container %q is not a %s container (is a %q container)", container, define.Package, b.Type)
	}

	netInt, err := getNetworkInterface(store, b.CNIConfigDir, b.CNIPluginPath)
	if err != nil {
		return nil, err
	}
	b.NetworkInterface = netInt
	b.store = store
	b.fixupConfig(nil)
	b.setupLogger()
	if b.CommonBuildOpts == nil {
		b.CommonBuildOpts = &CommonBuildOptions{}
	}
	return b, nil
}

// OpenBuilderByPath loads information about a build container given a
// path to the container's root filesystem
func OpenBuilderByPath(store storage.Store, path string) (*Builder, error) {
	containers, err := store.Containers()
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	builderMatchesPath := func(b *Builder, path string) bool {
		return (b.MountPoint == path)
	}
	for _, container := range containers {
		cdir, err := store.ContainerDirectory(container.ID)
		if err != nil {
			return nil, err
		}
		buildstate, err := os.ReadFile(filepath.Join(cdir, stateFile))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logrus.Debugf("error reading %q: %v, ignoring container %q", filepath.Join(cdir, stateFile), err, container.ID)
				continue
			}
			return nil, err
		}
		b := &Builder{}
		err = json.Unmarshal(buildstate, &b)
		if err == nil && b.Type == containerType && builderMatchesPath(b, abs) {
			b.store = store
			b.fixupConfig(nil)
			b.setupLogger()
			if b.CommonBuildOpts == nil {
				b.CommonBuildOpts = &CommonBuildOptions{}
			}
			return b, nil
		}
		if err != nil {
			logrus.Debugf("error parsing %q, read from %q: %v", string(buildstate), filepath.Join(cdir, stateFile), err)
		} else if b.Type != containerType {
			logrus.Debugf("container %q is not a %s container (is a %q container)", container.ID, define.Package, b.Type)
		}
	}
	return nil, storage.ErrContainerUnknown
}

// OpenAllBuilders loads all containers which have a state file that we use in
// their data directory, typically so that they can be listed.
func OpenAllBuilders(store storage.Store) (builders []*Builder, err error) {
	containers, err := store.Containers()
	if err != nil {
		return nil, err
	}
	for _, container := range containers {
		cdir, err := store.ContainerDirectory(container.ID)
		if err != nil {
			return nil, err
		}
		buildstate, err := os.ReadFile(filepath.Join(cdir, stateFile))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logrus.Debugf("%v, ignoring container %q", err, container.ID)
				continue
			}
			return nil, err
		}
		b := &Builder{}
		err = json.Unmarshal(buildstate, &b)
		if err == nil && b.Type == containerType {
			b.store = store
			b.setupLogger()
			b.fixupConfig(nil)
			if b.CommonBuildOpts == nil {
				b.CommonBuildOpts = &CommonBuildOptions{}
			}
			builders = append(builders, b)
			continue
		}
		if err != nil {
			logrus.Debugf("error parsing %q, read from %q: %v", string(buildstate), filepath.Join(cdir, stateFile), err)
		} else if b.Type != containerType {
			logrus.Debugf("container %q is not a %s container (is a %q container)", container.ID, define.Package, b.Type)
		}
	}
	return builders, nil
}

// Save saves the builder's current state to the build container's metadata.
// This should not need to be called directly, as other methods of the Builder
// object take care of saving their state.
func (b *Builder) Save() error {
	buildstate, err := json.Marshal(b)
	if err != nil {
		return err
	}
	cdir, err := b.store.ContainerDirectory(b.ContainerID)
	if err != nil {
		return err
	}
	if err = ioutils.AtomicWriteFile(filepath.Join(cdir, stateFile), buildstate, 0o600); err != nil {
		return fmt.Errorf("saving builder state to %q: %w", filepath.Join(cdir, stateFile), err)
	}
	return nil
}
