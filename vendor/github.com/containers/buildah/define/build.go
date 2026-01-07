package define

import (
	"io"
	"time"

	encconfig "github.com/containers/ocicrypt/config"
	"go.podman.io/common/libimage/manifests"
	nettypes "go.podman.io/common/libnetwork/types"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/archive"
	"golang.org/x/sync/semaphore"
)

// AdditionalBuildContext contains verbose details about a parsed build context from --build-context
type AdditionalBuildContext struct {
	// Value is the URL of an external tar archive.
	IsURL bool
	// Value is the name of an image which may or may not have already been pulled.
	IsImage bool
	// Value holds a URL (if IsURL), an image name (if IsImage), or an absolute filesystem path.
	Value string
	// Absolute filesystem path to a downloaded and exported build context
	// from an external tar archive.  This will be populated only if the
	// build context was a URL and its contents have been downloaded.
	DownloadedCache string
}

// CommonBuildOptions are resources that can be defined by flags for both buildah from and build
type CommonBuildOptions struct {
	// AddHost is the list of hostnames to add to the build container's /etc/hosts.
	AddHost []string
	// OmitHistory tells the builder to ignore the history of build layers and
	// base while preparing image-spec, setting this to true will ensure no history
	// is added to the image-spec. (default false)
	OmitHistory bool
	// CgroupParent is the path to cgroups under which the cgroup for the container will be created.
	CgroupParent string
	// CPUPeriod limits the CPU CFS (Completely Fair Scheduler) period
	CPUPeriod uint64
	// CPUQuota limits the CPU CFS (Completely Fair Scheduler) quota
	CPUQuota int64
	// CPUShares (relative weight
	CPUShares uint64
	// CPUSetCPUs in which to allow execution (0-3, 0,1)
	CPUSetCPUs string
	// CPUSetMems memory nodes (MEMs) in which to allow execution (0-3, 0,1). Only effective on NUMA systems.
	CPUSetMems string
	// HTTPProxy determines whether *_proxy env vars from the build host are passed into the container.
	HTTPProxy bool
	// IdentityLabel if set controls whether or not a `io.buildah.version` label is added to the built image.
	// Setting this to false does not clear the label if it would be inherited from the base image.
	IdentityLabel types.OptionalBool
	// Memory is the upper limit (in bytes) on how much memory running containers can use.
	Memory int64
	// DNSSearch is the list of DNS search domains to add to the build container's /etc/resolv.conf
	DNSSearch []string
	// DNSServers is the list of DNS servers to add to the build container's /etc/resolv.conf
	DNSServers []string
	// DNSOptions is the list of DNS
	DNSOptions []string
	// LabelOpts is a slice of the fields of an SELinux context, given in "field:pair" format, or "disable".
	// Recognized field names are "role", "type", and "level".
	LabelOpts []string
	// Paths to mask
	Masks []string
	// MemorySwap limits the amount of memory and swap together.
	MemorySwap int64
	// NoHostname tells the builder not to create /etc/hostname content when running
	// containers.
	NoHostname bool
	// NoHosts tells the builder not to create /etc/hosts content when running
	// containers.
	NoHosts bool
	// NoNewPrivileges removes the ability for the container to gain privileges
	NoNewPrivileges bool
	// OmitTimestamp forces epoch 0 as created timestamp to allow for
	// deterministic, content-addressable builds.
	OmitTimestamp bool
	// SeccompProfilePath is the pathname of a seccomp profile.
	SeccompProfilePath string
	// ApparmorProfile is the name of an apparmor profile.
	ApparmorProfile string
	// ShmSize is the "size" value to use when mounting an shmfs on the container's /dev/shm directory.
	ShmSize string
	// Ulimit specifies resource limit options, in the form type:softlimit[:hardlimit].
	// These types are recognized:
	// "core": maximum core dump size (ulimit -c)
	// "cpu": maximum CPU time (ulimit -t)
	// "data": maximum size of a process's data segment (ulimit -d)
	// "fsize": maximum size of new files (ulimit -f)
	// "locks": maximum number of file locks (ulimit -x)
	// "memlock": maximum amount of locked memory (ulimit -l)
	// "msgqueue": maximum amount of data in message queues (ulimit -q)
	// "nice": niceness adjustment (nice -n, ulimit -e)
	// "nofile": maximum number of open files (ulimit -n)
	// "nproc": maximum number of processes (ulimit -u)
	// "rss": maximum size of a process's (ulimit -m)
	// "rtprio": maximum real-time scheduling priority (ulimit -r)
	// "rttime": maximum amount of real-time execution between blocking syscalls
	// "sigpending": maximum number of pending signals (ulimit -i)
	// "stack": maximum stack size (ulimit -s)
	Ulimit []string
	// Volumes to bind mount into the container
	Volumes []string
	// Secrets are the available secrets to use in a build.  Each item in the
	// slice takes the form "id=foo,src=bar", where both "id" and "src" are
	// required, in that order, and "bar" is the name of a file.
	Secrets []string
	// SSHSources is the available ssh agent connections to forward in the build
	SSHSources []string
	// OCIHooksDir is the location of OCI hooks for the build containers
	OCIHooksDir []string
	// Paths to unmask
	Unmasks []string
}

// BuildOptions can be used to alter how an image is built.
type BuildOptions struct {
	// ContainerSuffix it the name to suffix containers with
	ContainerSuffix string
	// ContextDirectory is the default source location for COPY and ADD
	// commands.
	ContextDirectory string
	// PullPolicy controls whether or not we pull images.  It should be one
	// of PullIfMissing, PullAlways, PullIfNewer, or PullNever.
	PullPolicy PullPolicy
	// Registry is a value which is prepended to the image's name, if it
	// needs to be pulled and the image name alone can not be resolved to a
	// reference to a source image.  No separator is implicitly added.
	Registry string
	// IgnoreUnrecognizedInstructions tells us to just log instructions we
	// don't recognize, and try to keep going.
	IgnoreUnrecognizedInstructions bool
	// Manifest Name to which the image will be added.
	Manifest string
	// Quiet tells us whether or not to announce steps as we go through them.
	Quiet bool
	// Isolation controls how Run() runs things.
	Isolation Isolation
	// Runtime is the name of the command to run for RUN instructions when
	// Isolation is either IsolationDefault or IsolationOCI.  It should
	// accept the same arguments and flags that runc does.
	Runtime string
	// RuntimeArgs adds global arguments for the runtime.
	RuntimeArgs []string
	// TransientMounts is a list of unparsed mounts that will be provided to
	// RUN instructions.
	TransientMounts []string
	// CacheFrom specifies any remote repository which can be treated as
	// potential cache source.
	CacheFrom []reference.Named
	// CacheTo specifies any remote repository which can be treated as
	// potential cache destination.
	CacheTo []reference.Named
	// CacheTTL specifies duration, if specified using `--cache-ttl` then
	// cache intermediate images under this duration will be considered as
	// valid cache sources and images outside this duration will be ignored.
	CacheTTL time.Duration
	// Compression specifies the type of compression which is applied to
	// layer blobs.  The default is to not use compression, but
	// archive.Gzip is recommended.
	Compression archive.Compression
	// Arguments which can be interpolated into Dockerfiles
	Args map[string]string
	// Map of external additional build contexts
	AdditionalBuildContexts map[string]*AdditionalBuildContext
	// Name of the image to write to.
	Output string
	// BuildOutputs specifies if any custom build output is selected for
	// following build.  It allows the end user to export the image's
	// rootfs to a directory or a tar archive.  See the documentation of
	// 'buildah build --output' for the details of the syntax.
	BuildOutputs []string
	// Deprecated: use BuildOutputs instead.
	BuildOutput string
	// ConfidentialWorkload controls whether or not, and if so, how, we produce an
	// image that's meant to be run using krun as a VM instead of a conventional
	// process-type container.
	ConfidentialWorkload ConfidentialWorkloadOptions
	// Additional tags to add to the image that we write, if we know of a
	// way to add them.
	AdditionalTags []string
	// Logfile specifies if log output is redirected to an external file
	// instead of stdout, stderr.
	LogFile string
	// LogByPlatform tells imagebuildah to split log to different log files
	// for each platform if logging to external file was selected.
	LogSplitByPlatform bool
	// Log is a callback that will print a progress message.  If no value
	// is supplied, the message will be sent to Err (or os.Stderr, if Err
	// is nil) by default.
	Log func(format string, args ...any)
	// In is connected to stdin for RUN instructions.
	In io.Reader
	// Out is a place where non-error log messages are sent.
	Out io.Writer
	// Err is a place where error log messages should be sent.
	Err io.Writer
	// SignaturePolicyPath specifies an override location for the signature
	// policy which should be used for verifying the new image as it is
	// being written.  Except in specific circumstances, no value should be
	// specified, indicating that the shared, system-wide default policy
	// should be used.
	SignaturePolicyPath string
	// SkipUnusedStages allows users to skip stages in a multi-stage builds
	// which do not contribute anything to the target stage. Expected default
	// value is true.
	SkipUnusedStages types.OptionalBool
	// ReportWriter is an io.Writer which will be used to report the
	// progress of the (possible) pulling of the source image and the
	// writing of the new image.
	ReportWriter io.Writer
	// OutputFormat is the format of the output image's manifest and
	// configuration data.
	// Accepted values are buildah.OCIv1ImageManifest and buildah.Dockerv2ImageManifest.
	OutputFormat string
	// SystemContext holds parameters used for authentication.
	SystemContext *types.SystemContext
	// NamespaceOptions controls how we set up namespaces processes that we
	// might need when handling RUN instructions.
	NamespaceOptions []NamespaceOption
	// ConfigureNetwork controls whether or not network interfaces and
	// routing are configured for a new network namespace (i.e., when not
	// joining another's namespace and not just using the host's
	// namespace), effectively deciding whether or not the process has a
	// usable network.
	ConfigureNetwork NetworkConfigurationPolicy
	// CNIPluginPath is the location of CNI plugin helpers, if they should be
	// run from a location other than the default location.
	CNIPluginPath string
	// CNIConfigDir is the location of CNI configuration files, if the files in
	// the default configuration directory shouldn't be used.
	CNIConfigDir string

	// NetworkInterface is the libnetwork network interface used to setup CNI or netavark networks.
	NetworkInterface nettypes.ContainerNetwork `json:"-"`

	// ID mapping options to use if we're setting up our own user namespace
	// when handling RUN instructions.
	IDMappingOptions *IDMappingOptions
	// InheritLabels controls whether or not built images will retain the labels
	// which were set in their base images
	InheritLabels types.OptionalBool
	// InheritAnnotations controls whether or not built images will retain the annotations
	// which were set in their base images
	InheritAnnotations types.OptionalBool
	// AddCapabilities is a list of capabilities to add to the default set when
	// handling RUN instructions.
	AddCapabilities []string
	// DropCapabilities is a list of capabilities to remove from the default set
	// when handling RUN instructions. If a capability appears in both lists, it
	// will be dropped.
	DropCapabilities []string
	// CommonBuildOpts is *required*.
	CommonBuildOpts *CommonBuildOptions
	// CPPFlags are additional arguments to pass to the C Preprocessor (cpp).
	CPPFlags []string
	// DefaultMountsFilePath is the file path holding the mounts to be mounted for RUN
	// instructions in "host-path:container-path" format
	DefaultMountsFilePath string
	// IIDFile tells the builder to write the image ID to the specified file
	IIDFile string
	// Squash tells the builder to produce an image with a single layer instead of with
	// possibly more than one layer, by only committing a new layer after processing the
	// final instruction.
	Squash bool
	// Labels to set in a committed image.
	Labels []string
	// LayerLabels metadata for an intermediate image
	LayerLabels []string
	// Annotations to set in a committed image, in OCI format.
	Annotations []string
	// OnBuild commands to be run by builds that use the image we'll commit as a base image.
	OnBuild []string
	// Layers tells the builder to commit an image for each step in the Dockerfile.
	Layers bool
	// NoCache tells the builder to build the image from scratch without checking for a cache.
	// It creates a new set of cached images for the build.
	NoCache bool
	// RemoveIntermediateCtrs tells the builder whether to remove intermediate containers used
	// during the build process. Default is true.
	RemoveIntermediateCtrs bool
	// ForceRmIntermediateCtrs tells the builder to remove all intermediate containers even if
	// the build was unsuccessful.
	ForceRmIntermediateCtrs bool
	// BlobDirectory is a directory which we'll use for caching layer blobs.
	//
	// This option will be overridden for cache pulls if
	// CachePullDestinationLookupReferenceFunc is set, and overridden for cache pushes if
	// CachePushSourceLookupReferenceFunc is set.
	BlobDirectory string
	// Target the targeted FROM in the Dockerfile to build.
	Target string
	// Devices are unparsed devices to provide to RUN instructions.
	Devices []string
	// SignBy is the fingerprint of a GPG key to use for signing images.
	SignBy string
	// Architecture specifies the target architecture of the image to be built.
	Architecture string
	// Timestamp specifies a timestamp to use for the image's created-on
	// date, the corresponding field in new history entries, the timestamps
	// to set on contents in new layer diffs, and the timestamps to set on
	// contents written as specified in the BuildOutput field.  If left
	// unset, the current time is used for the configuration and manifest,
	// and layer contents are recorded as-is.
	Timestamp *time.Time
	// SourceDateEpoch specifies a timestamp to use for the image's
	// created-on date and the corresponding field in new history entries,
	// and any content written as specified in the BuildOutput field.  If
	// left unset, the current time is used for the configuration and
	// manifest, and layer and BuildOutput contents retain their original
	// timestamps.
	SourceDateEpoch *time.Time
	// RewriteTimestamp, if set, forces timestamps in generated layers to
	// not be later than the SourceDateEpoch, if it is also set.
	RewriteTimestamp bool
	// OS is the specifies the operating system of the image to be built.
	OS string
	// MaxPullPushRetries is the maximum number of attempts we'll make to pull or push any one
	// image from or to an external registry if the first attempt fails.
	MaxPullPushRetries int
	// PullPushRetryDelay is how long to wait before retrying a pull or push attempt.
	PullPushRetryDelay time.Duration
	// OciDecryptConfig contains the config that can be used to decrypt an image if it is
	// encrypted if non-nil. If nil, it does not attempt to decrypt an image.
	OciDecryptConfig *encconfig.DecryptConfig
	// Jobs is the number of stages to run in parallel.  If not specified it defaults to 1.
	// Ignored if a JobSemaphore is provided.
	Jobs *int
	// JobSemaphore, for when you want Jobs to be shared with more than just this build.
	JobSemaphore *semaphore.Weighted
	// LogRusage logs resource usage for each step.
	LogRusage bool
	// File to which the Rusage logs will be saved to instead of stdout.
	RusageLogFile string
	// Excludes is a list of excludes to be used instead of the .dockerignore file.
	Excludes []string
	// IgnoreFile is a name of the .containerignore file
	IgnoreFile string
	// From is the image name to use to replace the value specified in the first
	// FROM instruction in the Containerfile.
	From string
	// GroupAdd is a list of groups to add to the primary process when handling RUN
	// instructions. The magic 'keep-groups' value indicates that the process should
	// be allowed to inherit the current set of supplementary groups.
	GroupAdd []string
	// Platforms is the list of parsed OS/Arch/Variant triples that we want
	// to build the image for.  If this slice has items in it, the OS and
	// Architecture fields above are ignored.
	Platforms []struct{ OS, Arch, Variant string }
	// AllPlatforms tells the builder to set the list of target platforms
	// to match the set of platforms for which all of the build's base
	// images are available.  If this field is set, Platforms is ignored.
	AllPlatforms bool
	// UnsetEnvs is a list of environments to not add to final image.
	UnsetEnvs []string
	// UnsetLabels is a list of labels to not add to final image from base image.
	UnsetLabels []string
	// UnsetAnnotations is a list of annotations to not add to final image from base image.
	UnsetAnnotations []string
	// Envs is a list of environment variables to set in the final image.
	Envs []string
	// OSFeatures specifies operating system features the image requires.
	// It is typically only set when the OS is "windows".
	OSFeatures []string
	// OSVersion specifies the exact operating system version the image
	// requires.  It is typically only set when the OS is "windows".  Any
	// value set in a base image will be preserved, so this does not
	// frequently need to be set.
	OSVersion string
	// SBOMScanOptions encapsulates options which control whether or not we
	// run scanners on the rootfs that we're about to commit, and how.
	SBOMScanOptions []SBOMScanOptions
	// CDIConfigDir is the location of CDI configuration files, if the files in
	// the default configuration locations shouldn't be used.
	CDIConfigDir string
	// CachePullSourceLookupReferenceFunc is an optional LookupReferenceFunc
	// used to look up source references for cache pulls.
	CachePullSourceLookupReferenceFunc manifests.LookupReferenceFunc
	// CachePullDestinationLookupReferenceFunc is an optional generator
	// function which provides a LookupReferenceFunc used to look up
	// destination references for cache pulls.
	//
	// BlobDirectory will be ignored for cache pulls if this option is set.
	CachePullDestinationLookupReferenceFunc func(srcRef types.ImageReference) manifests.LookupReferenceFunc
	// CachePushSourceLookupReferenceFunc is an optional generator function
	// which provides a LookupReferenceFunc used to look up source
	// references for cache pushes.
	//
	// BlobDirectory will be ignored for cache pushes if this option is set.
	CachePushSourceLookupReferenceFunc func(dest types.ImageReference) manifests.LookupReferenceFunc
	// CachePushDestinationLookupReferenceFunc is an optional
	// LookupReferenceFunc used to look up destination references for cache
	// pushes
	CachePushDestinationLookupReferenceFunc manifests.LookupReferenceFunc
	// CompatSetParent causes the "parent" field to be set in the image's
	// configuration when committing in Docker format.  Newer
	// BuildKit-based docker build doesn't set this field.
	CompatSetParent types.OptionalBool
	// CompatVolumes causes the contents of locations marked as volumes in
	// base images or by a VOLUME instruction to be preserved during RUN
	// instructions.  Newer BuildKit-based docker build doesn't bother.
	CompatVolumes types.OptionalBool
	// CompatScratchConfig causes the image, if it does not have a base
	// image, to begin with a truly empty default configuration instead of
	// a minimal default configuration. Newer BuildKit-based docker build
	// provides a minimal initial configuration with a working directory
	// set in it.
	CompatScratchConfig types.OptionalBool
	// CompatLayerOmissions causes the "/dev", "/proc", and "/sys"
	// directories to be omitted from the image and related output.  Newer
	// BuildKit-based builds include them in the built image by default.
	CompatLayerOmissions types.OptionalBool
	// NoPivotRoot inhibits the usage of pivot_root when setting up the rootfs
	NoPivotRoot bool
	// CreatedAnnotation controls whether or not an "org.opencontainers.image.created"
	// annotation is present in the output image.
	CreatedAnnotation types.OptionalBool
}
