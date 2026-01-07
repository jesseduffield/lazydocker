package buildah

import (
	"fmt"
	"io"
	"net"

	"github.com/containers/buildah/copier"
	"github.com/containers/buildah/define"
	"github.com/containers/buildah/internal"
	"github.com/containers/buildah/pkg/sshagent"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/lockfile"
)

const (
	// runUsingRuntimeCommand is a command we use as a key for reexec
	runUsingRuntimeCommand = define.Package + "-oci-runtime"
)

// compatLayerExclusions is the set of items to omit from layers if
// options.CompatLayerOmissions is set to true.  For whatever reason, the
// classic builder didn't bake these into images, but BuildKit does.
var compatLayerExclusions = []copier.ConditionalRemovePath{
	{Path: "dev", Owner: &idtools.IDPair{UID: 0, GID: 0}},
	{Path: "proc", Owner: &idtools.IDPair{UID: 0, GID: 0}},
	{Path: "sys", Owner: &idtools.IDPair{UID: 0, GID: 0}},
}

// TerminalPolicy takes the value DefaultTerminal, WithoutTerminal, or WithTerminal.
type TerminalPolicy int

const (
	// DefaultTerminal indicates that this Run invocation should be
	// connected to a pseudoterminal if we're connected to a terminal.
	DefaultTerminal TerminalPolicy = iota
	// WithoutTerminal indicates that this Run invocation should NOT be
	// connected to a pseudoterminal.
	WithoutTerminal
	// WithTerminal indicates that this Run invocation should be connected
	// to a pseudoterminal.
	WithTerminal
)

// String converts a TerminalPolicy into a string.
func (t TerminalPolicy) String() string {
	switch t {
	case DefaultTerminal:
		return "DefaultTerminal"
	case WithoutTerminal:
		return "WithoutTerminal"
	case WithTerminal:
		return "WithTerminal"
	}
	return fmt.Sprintf("unrecognized terminal setting %d", t)
}

// NamespaceOption controls how we set up a namespace when launching processes.
type NamespaceOption = define.NamespaceOption

// NamespaceOptions provides some helper methods for a slice of NamespaceOption
// structs.
type NamespaceOptions = define.NamespaceOptions

// IDMappingOptions controls how we set up UID/GID mapping when we set up a
// user namespace.
type IDMappingOptions = define.IDMappingOptions

// Isolation provides a way to specify whether we're supposed to use a proper
// OCI runtime, or some other method for running commands.
type Isolation = define.Isolation

const (
	// IsolationDefault is whatever we think will work best.
	IsolationDefault = define.IsolationDefault
	// IsolationOCI is a proper OCI runtime.
	IsolationOCI = define.IsolationOCI
	// IsolationChroot is a more chroot-like environment: less isolation,
	// but with fewer requirements.
	IsolationChroot = define.IsolationChroot
	// IsolationOCIRootless is a proper OCI runtime in rootless mode.
	IsolationOCIRootless = define.IsolationOCIRootless
)

// RunOptions can be used to alter how a command is run in the container.
type RunOptions struct {
	// Logger is the logrus logger to write log messages with
	Logger *logrus.Logger `json:"-"`
	// Hostname is the hostname we set for the running container.
	Hostname string
	// Isolation is either IsolationDefault, IsolationOCI, IsolationChroot, or IsolationOCIRootless.
	Isolation define.Isolation
	// Runtime is the name of the runtime to run.  It should accept the
	// same arguments that runc does, and produce similar output.
	Runtime string
	// Args adds global arguments for the runtime.
	Args []string
	// NoHostname won't create new /etc/hostname file
	NoHostname bool
	// NoHosts won't create new /etc/hosts file
	NoHosts bool
	// NoPivot adds the --no-pivot runtime flag.
	NoPivot bool
	// Mounts are additional mount points which we want to provide.
	Mounts []specs.Mount
	// Env is additional environment variables to set.
	Env []string
	// User is the user as whom to run the command.
	User string
	// WorkingDir is an override for the working directory.
	WorkingDir string
	// ContextDir is used as the root directory for the source location for mounts that are of type "bind".
	ContextDir string
	// Shell is default shell to run in a container.
	Shell string
	// Cmd is an override for the configured default command.
	Cmd []string
	// Entrypoint is an override for the configured entry point.
	Entrypoint []string
	// NamespaceOptions controls how we set up the namespaces for the process.
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
	// Terminal provides a way to specify whether or not the command should
	// be run with a pseudoterminal.  By default (DefaultTerminal), a
	// terminal is used if os.Stdout is connected to a terminal, but that
	// decision can be overridden by specifying either WithTerminal or
	// WithoutTerminal.
	Terminal TerminalPolicy
	// TerminalSize provides a way to set the number of rows and columns in
	// a pseudo-terminal, if we create one, and Stdin/Stdout/Stderr aren't
	// connected to a terminal.
	TerminalSize *specs.Box
	// The stdin/stdout/stderr descriptors to use.  If set to nil, the
	// corresponding files in the "os" package are used as defaults.
	Stdin  io.Reader `json:"-"`
	Stdout io.Writer `json:"-"`
	Stderr io.Writer `json:"-"`
	// Quiet tells the run to turn off output to stdout.
	Quiet bool
	// AddCapabilities is a list of capabilities to add to the default set.
	AddCapabilities []string
	// DropCapabilities is a list of capabilities to remove from the default set,
	// after processing the AddCapabilities set.  If a capability appears in both
	// lists, it will be dropped.
	DropCapabilities []string
	// Devices are parsed additional devices to add
	Devices define.ContainerDevices
	// DeviceSpecs are unparsed additional devices to add
	DeviceSpecs []string
	// Secrets are the available secrets to use
	Secrets map[string]define.Secret
	// SSHSources is the available ssh agents to use
	SSHSources map[string]*sshagent.Source `json:"-"`
	// RunMounts are unparsed mounts to be added for this run
	RunMounts []string
	// Map of stages and container mountpoint if any from stage executor
	StageMountPoints map[string]internal.StageMountDetails
	// IDs of mounted images to be unmounted before returning
	// Deprecated: before 1.39, these images would not be consistently
	// unmounted if Run() returned an error
	ExternalImageMounts []string
	// System context of current build
	SystemContext *types.SystemContext
	// CgroupManager to use for running OCI containers
	CgroupManager string
	// CDIConfigDir is the location of CDI configuration files, if the files in
	// the default configuration locations shouldn't be used.
	CDIConfigDir string
	// CompatBuiltinVolumes causes the contents of locations marked as
	// volumes in the container's configuration to be set up as bind mounts to
	// directories which are not in the container's rootfs, hiding changes
	// made to contents of those changes when the container is subsequently
	// committed.
	CompatBuiltinVolumes types.OptionalBool
}

// RunMountArtifacts are the artifacts created when using a run mount.
type runMountArtifacts struct {
	// RunOverlayDirs are overlay directories which will need to be cleaned up using overlay.RemoveTemp()
	RunOverlayDirs []string
	// Any images which were mounted, which should be unmounted
	MountedImages []string
	// Agents are the ssh agents started, which should have their Shutdown() methods called
	Agents []*sshagent.AgentServer
	// SSHAuthSock is the path to the ssh auth sock inside the container
	SSHAuthSock string
	// Lock files, which should have their Unlock() methods called
	TargetLocks []*lockfile.LockFile
	// Intermediate mount points, which should be Unmount()ed and Removed()d
	IntermediateMounts []string
}

// RunMountInfo are the available run mounts for this run
type runMountInfo struct {
	// WorkDir is the current working directory inside the container.
	WorkDir string
	// ContextDir is the root directory for the source location for bind mounts.
	ContextDir string
	// Secrets are the available secrets to use in a RUN
	Secrets map[string]define.Secret
	// SSHSources is the available ssh agents to use in a RUN
	SSHSources map[string]*sshagent.Source `json:"-"`
	// Map of stages and container mountpoint if any from stage executor
	StageMountPoints map[string]internal.StageMountDetails
	// System context of current build
	SystemContext *types.SystemContext
}

// IDMaps are the UIDs, GID, and maps for the run
type IDMaps struct {
	uidmap     []specs.LinuxIDMapping
	gidmap     []specs.LinuxIDMapping
	rootUID    int
	rootGID    int
	processUID int
	processGID int
}

// netResult type to hold network info for hosts/resolv.conf
type netResult struct {
	entries                           etchosts.HostEntries
	dnsServers                        []string
	excludeIPs                        []net.IP
	ipv6                              bool
	keepHostResolvers                 bool
	preferredHostContainersInternalIP string
}
