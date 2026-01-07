package define

import (
	"bufio"
	"io"

	"go.podman.io/common/libnetwork/types"
)

var (
	// DefaultSHMLockPath is the default path for SHM locks
	DefaultSHMLockPath = "/libpod_lock"
	// DefaultRootlessSHMLockPath is the default path for rootless SHM locks
	DefaultRootlessSHMLockPath = "/libpod_rootless_lock"

	// NameRegex is a regular expression to validate container/pod names.
	// This must NOT be changed from outside of Libpod. It should be a
	// constant, but Go won't let us do that.
	NameRegex = types.NameRegex
	// RegexError is thrown in presence of an invalid container/pod name.
	RegexError = types.ErrInvalidName
)

const (
	// DefaultTransport is a prefix that we apply to an image name
	// to check docker hub first for the image
	DefaultTransport = "docker://"
)

// InfoData holds the info type, i.e store, host etc and the data for each type
type InfoData struct {
	Type string
	Data map[string]any
}

// VolumeDriverLocal is the "local" volume driver. It is managed by libpod
// itself.
const VolumeDriverLocal = "local"

// VolumeDriverImage is the "image" volume driver. It is managed by Libpod and
// uses volumes backed by an image.
const VolumeDriverImage = "image"

const (
	OCIManifestDir  = "oci-dir"
	OCIArchive      = "oci-archive"
	V2s2ManifestDir = "docker-dir"
	V2s2Archive     = "docker-archive"
)

// AttachStreams contains streams that will be attached to the container
type AttachStreams struct {
	// OutputStream will be attached to container's STDOUT
	OutputStream io.Writer
	// ErrorStream will be attached to container's STDERR
	ErrorStream io.Writer
	// InputStream will be attached to container's STDIN
	InputStream *bufio.Reader
	// AttachOutput is whether to attach to STDOUT
	// If false, stdout will not be attached
	AttachOutput bool
	// AttachError is whether to attach to STDERR
	// If false, stdout will not be attached
	AttachError bool
	// AttachInput is whether to attach to STDIN
	// If false, stdout will not be attached
	AttachInput bool
}

// JournaldLogging is the string conmon expects to specify journald logging
const JournaldLogging = "journald"

// KubernetesLogging is the string conmon expects when specifying to use the kubernetes logging format
const KubernetesLogging = "k8s-file"

// JSONLogging is the string conmon expects when specifying to use the json logging format
const JSONLogging = "json-file"

// NoLogging is the string conmon expects when specifying to use no log driver whatsoever
const NoLogging = "none"

// PassthroughLogging is the string conmon expects when specifying to use the passthrough driver
const PassthroughLogging = "passthrough"

// PassthroughTTYLogging is the string conmon expects when specifying to use the passthrough driver even on a tty.
const PassthroughTTYLogging = "passthrough-tty"

// DefaultRlimitValue is the value set by default for nofile and nproc
const RLimitDefaultValue = uint64(1048576)

// BindMountPrefix distinguishes its annotations from others
const BindMountPrefix = "bind-mount-options"
