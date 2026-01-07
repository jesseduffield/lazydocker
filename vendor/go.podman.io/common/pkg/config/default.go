package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/internal/attributedstring"
	nettypes "go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/apparmor"
	"go.podman.io/common/pkg/cgroupv2"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/unshare"
	"go.podman.io/storage/types"
)

const (
	// _defaultGraphRoot points to the default path of the graph root.
	_defaultGraphRoot = "/var/lib/containers/storage"

	// _defaultTransport is a prefix that we apply to an image name to check
	// docker hub first for the image.
	_defaultTransport = "docker://"

	// _defaultImageVolumeMode is a mode to handle built-in image volumes.
	_defaultImageVolumeMode = "anonymous"

	// defaultInitName is the default name of the init binary.
	defaultInitName = "catatonit"
)

func getMaskedPaths() ([]string, error) {
	maskedPaths := []string{
		"/proc/acpi",
		"/proc/kcore",
		"/proc/keys",
		"/proc/latency_stats",
		"/proc/sched_debug",
		"/proc/scsi",
		"/proc/timer_list",
		"/proc/timer_stats",
		"/sys/devices/virtual/powercap",
		"/sys/firmware",
		"/sys/fs/selinux",
		"/proc/interrupts",
	}
	maskedPathsToGlob := []string{
		"/sys/devices/system/cpu/cpu*/thermal_throttle",
	}

	for _, p := range maskedPathsToGlob {
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, err
		}
		maskedPaths = append(maskedPaths, matches...)
	}
	return maskedPaths, nil
}

var DefaultMaskedPaths = sync.OnceValue(func() []string {
	maskedPaths, err := getMaskedPaths()
	// this should never happen, the only error possible
	// is ErrBadPattern and the patterns that were added must be valid
	if err != nil {
		panic(err)
	}

	return maskedPaths
})

var (
	DefaultReadOnlyPaths = []string{
		"/proc/asound",
		"/proc/bus",
		"/proc/fs",
		"/proc/irq",
		"/proc/sys",
		"/proc/sysrq-trigger",
	}

	// DefaultInfraImage is the default image to run as infrastructure containers in pods.
	DefaultInfraImage = ""
	// DefaultRootlessSHMLockPath is the default path for rootless SHM locks.
	DefaultRootlessSHMLockPath = "/libpod_rootless_lock"
	// DefaultDetachKeys is the default keys sequence for detaching a
	// container.
	DefaultDetachKeys = "ctrl-p,ctrl-q"
	// ErrConmonOutdated indicates the version of conmon found (whether via the configuration or $PATH)
	// is out of date for the current podman version.
	ErrConmonOutdated = errors.New("outdated conmon version")
	// ErrInvalidArg indicates that an invalid argument was passed.
	ErrInvalidArg = errors.New("invalid argument")
	// DefaultHooksDirs defines the default hooks directory.
	DefaultHooksDirs = []string{"/usr/share/containers/oci/hooks.d"}
	// DefaultCdiSpecDirs defines the default cdi spec directories.
	DefaultCdiSpecDirs = []string{"/etc/cdi", "/var/run/cdi"}
	// DefaultCapabilities is the default for the default_capabilities option in the containers.conf file.
	DefaultCapabilities = []string{
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FOWNER",
		"CAP_FSETID",
		"CAP_KILL",
		"CAP_NET_BIND_SERVICE",
		"CAP_SETFCAP",
		"CAP_SETGID",
		"CAP_SETPCAP",
		"CAP_SETUID",
		"CAP_SYS_CHROOT",
	}

	// Search these locations in which CNIPlugins can be installed.
	DefaultCNIPluginDirs = []string{
		"/usr/local/libexec/cni",
		"/usr/libexec/cni",
		"/usr/local/lib/cni",
		"/usr/lib/cni",
		"/opt/cni/bin",
	}
	DefaultNetavarkPluginDirs = []string{
		"/usr/local/libexec/netavark",
		"/usr/libexec/netavark",
		"/usr/local/lib/netavark",
		"/usr/lib/netavark",
	}
	DefaultSubnetPools = []SubnetPool{
		// 10.89.0.0/24-10.255.255.0/24
		parseSubnetPool("10.89.0.0/16", 24),
		parseSubnetPool("10.90.0.0/15", 24),
		parseSubnetPool("10.92.0.0/14", 24),
		parseSubnetPool("10.96.0.0/11", 24),
		parseSubnetPool("10.128.0.0/9", 24),
	}
	// additionalHelperBinariesDir is an extra helper binaries directory that
	// should be set during link-time, if different packagers put their
	// helper binary in a different location.
	additionalHelperBinariesDir string

	defaultContainerEnv = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
)

// nolint:unparam
func parseSubnetPool(subnet string, size int) SubnetPool {
	_, n, _ := net.ParseCIDR(subnet)
	return SubnetPool{
		Base: &nettypes.IPNet{IPNet: *n},
		Size: size,
	}
}

const (
	// _etcDir is the sysconfdir where podman should look for system config files.
	// It can be overridden at build time.
	_etcDir = "/etc"
	// InstallPrefix is the prefix where podman will be installed.
	// It can be overridden at build time.
	_installPrefix = "/usr"
	// CgroupfsCgroupsManager represents cgroupfs native cgroup manager.
	CgroupfsCgroupsManager = "cgroupfs"
	// DefaultApparmorProfile  specifies the default apparmor profile for the container.
	DefaultApparmorProfile = apparmor.Profile
	// DefaultHostsFile is the default path to the hosts file.
	DefaultHostsFile = "/etc/hosts"
	// SystemdCgroupsManager represents systemd native cgroup manager.
	SystemdCgroupsManager = "systemd"
	// DefaultLogSizeMax is the default value for the maximum log size
	// allowed for a container. Negative values mean that no limit is imposed.
	DefaultLogSizeMax = -1
	// DefaultEventsLogSize is the default value for the maximum events log size
	// before rotation.
	DefaultEventsLogSizeMax = uint64(1000000)
	// DefaultPidsLimit is the default value for maximum number of processes
	// allowed inside a container.
	DefaultPidsLimit = 2048
	// DefaultPullPolicy pulls the image if it does not exist locally.
	DefaultPullPolicy = "missing"
	// DefaultSubnet is the subnet that will be used for the default
	// network.
	DefaultSubnet = "10.88.0.0/16"
	// DefaultRootlessSignaturePolicyPath is the location within
	// XDG_CONFIG_HOME of the rootless policy.json file.
	DefaultRootlessSignaturePolicyPath = "containers/policy.json"
	// DefaultShmSize is the default upper limit on the size of tmpfs mounts.
	DefaultShmSize = "65536k"
	// DefaultUserNSSize indicates the default number of UIDs allocated for user namespace within a container.
	// Deprecated: no user of this field is known.
	DefaultUserNSSize = 65536
	// OCIBufSize limits maximum LogSizeMax.
	OCIBufSize = 8192
	// SeccompOverridePath if this exists it overrides the default seccomp path.
	SeccompOverridePath = _etcDir + "/containers/seccomp.json"
	// SeccompDefaultPath defines the default seccomp path.
	SeccompDefaultPath = _installPrefix + "/share/containers/seccomp.json"
	// DefaultVolumePluginTimeout is the default volume plugin timeout, in seconds.
	DefaultVolumePluginTimeout = 5
)

// defaultConfig returns Config with builtin defaults and minimal adjustments
// to the current host only. It does not read any config files from the host or
// the environment.
func defaultConfig() (*Config, error) {
	defaultEngineConfig, err := defaultEngineConfig()
	if err != nil {
		return nil, err
	}

	defaultEngineConfig.SignaturePolicyPath = DefaultSignaturePolicyPath
	// NOTE: For now we want Windows to use system locations.
	// GetRootlessUID == -1 on Windows, so exclude negative range
	if unshare.GetRootlessUID() > 0 {
		configHome, err := homedir.GetConfigHome()
		if err != nil {
			return nil, err
		}
		sigPath := filepath.Join(configHome, DefaultRootlessSignaturePolicyPath)
		defaultEngineConfig.SignaturePolicyPath = sigPath
		if err := fileutils.Exists(sigPath); err != nil {
			if err := fileutils.Exists(DefaultSignaturePolicyPath); err == nil {
				defaultEngineConfig.SignaturePolicyPath = DefaultSignaturePolicyPath
			}
		}
	}

	cgroupNS := "host"
	if cgroup2, _ := cgroupv2.Enabled(); cgroup2 {
		cgroupNS = "private"
	}

	return &Config{
		Containers: ContainersConfig{
			Annotations:         attributedstring.Slice{},
			ApparmorProfile:     DefaultApparmorProfile,
			BaseHostsFile:       "",
			CgroupNS:            cgroupNS,
			Cgroups:             getDefaultCgroupsMode(),
			DNSOptions:          attributedstring.Slice{},
			DNSSearches:         attributedstring.Slice{},
			DNSServers:          attributedstring.Slice{},
			DefaultCapabilities: attributedstring.NewSlice(DefaultCapabilities),
			DefaultSysctls:      attributedstring.Slice{},
			Devices:             attributedstring.Slice{},
			EnableKeyring:       true,
			EnableLabeling:      selinuxEnabled(),
			Env:                 attributedstring.NewSlice(defaultContainerEnv),
			EnvHost:             false,
			HTTPProxy:           true,
			IPCNS:               "shareable",
			Init:                false,
			InitPath:            "",
			LogDriver:           defaultLogDriver(),
			LogSizeMax:          DefaultLogSizeMax,
			Mounts:              attributedstring.Slice{},
			NetNS:               "private",
			NoHosts:             false,
			PidNS:               "private",
			PidsLimit:           DefaultPidsLimit,
			ShmSize:             DefaultShmSize,
			TZ:                  "",
			UTSNS:               "private",
			Umask:               "0022",
			UserNSSize:          DefaultUserNSSize, // Deprecated
			Volumes:             attributedstring.Slice{},
		},
		Network: NetworkConfig{
			FirewallDriver:            "",
			DefaultNetwork:            "podman",
			DefaultSubnet:             DefaultSubnet,
			DefaultSubnetPools:        DefaultSubnetPools,
			DefaultRootlessNetworkCmd: "pasta",
			DNSBindPort:               0,
			CNIPluginDirs:             attributedstring.NewSlice(DefaultCNIPluginDirs),
			NetavarkPluginDirs:        attributedstring.NewSlice(DefaultNetavarkPluginDirs),
		},
		Engine:   *defaultEngineConfig,
		Secrets:  defaultSecretConfig(),
		Machine:  defaultMachineConfig(),
		Farms:    defaultFarmConfig(),
		Podmansh: defaultPodmanshConfig(),
	}, nil
}

// defaultSecretConfig returns the default secret configuration.
// Please note that the default is choosing the "file" driver.
func defaultSecretConfig() SecretConfig {
	return SecretConfig{
		Driver: "file",
	}
}

// defaultMachineConfig returns the default machine configuration.
func defaultMachineConfig() MachineConfig {
	cpus := runtime.NumCPU() / 2
	if cpus == 0 {
		cpus = 1
	}
	return MachineConfig{
		CPUs:     uint64(cpus),
		DiskSize: 100,
		// TODO: Set machine image default here
		// Currently the default is set in Podman as we need time to stabilize
		// VM images and locations between different providers.
		Image:   "",
		Memory:  2048,
		User:    getDefaultMachineUser(),
		Volumes: attributedstring.NewSlice(getDefaultMachineVolumes()),
		Rosetta: true,
	}
}

// defaultFarmConfig returns the default farms configuration.
func defaultFarmConfig() FarmConfig {
	return FarmConfig{
		List: map[string][]string{},
	}
}

// defaultPodmanshConfig returns the default podmansh configuration.
func defaultPodmanshConfig() PodmanshConfig {
	return PodmanshConfig{
		Shell:     "/bin/sh",
		Container: "podmansh",

		// A value of 0 means "not set", needed to distinguish if engine.podmansh_timeout or podmansh.timeout should be used
		// This is needed to keep backwards compatibility to engine.PodmanshTimeout.
		Timeout: uint(0),
	}
}

// defaultEngineConfig returns a default engine configuration. Note that the
// config is different for root and rootless. It also parses the storage.conf.
func defaultEngineConfig() (*EngineConfig, error) {
	c := new(EngineConfig)
	tmp, err := defaultTmpDir()
	if err != nil {
		return nil, err
	}
	c.TmpDir = tmp

	c.EventsLogFileMaxSize = eventsLogMaxSize(DefaultEventsLogSizeMax)

	c.CompatAPIEnforceDockerHub = true
	c.ComposeProviders.Set(getDefaultComposeProviders()) // may vary across supported platforms
	c.ComposeWarningLogs = true

	storeOpts, err := types.DefaultStoreOptions()
	if err != nil {
		return nil, err
	}

	if storeOpts.GraphRoot == "" {
		logrus.Warnf("Storage configuration is unset - using hardcoded default graph root %q", _defaultGraphRoot)
		storeOpts.GraphRoot = _defaultGraphRoot
	}

	c.graphRoot = storeOpts.GraphRoot
	c.ImageCopyTmpDir = getDefaultTmpDir()

	c.VolumePluginTimeout = DefaultVolumePluginTimeout
	c.CompressionFormat = "gzip"

	c.HealthcheckEvents = true
	c.HelperBinariesDir.Set(defaultHelperBinariesDir)
	if additionalHelperBinariesDir != "" {
		// Prioritize additionalHelperBinariesDir over defaults.
		c.HelperBinariesDir.Set(append([]string{additionalHelperBinariesDir}, c.HelperBinariesDir.Get()...))
	}
	c.HooksDir.Set(DefaultHooksDirs)
	c.CdiSpecDirs.Set(DefaultCdiSpecDirs)
	c.ImageDefaultTransport = _defaultTransport
	c.ImageVolumeMode = _defaultImageVolumeMode

	c.ImageBuildFormat = "oci"

	c.CgroupManager = defaultCgroupManager()
	c.ServiceTimeout = uint(5)
	c.StopTimeout = uint(10)
	c.PodmanshTimeout = uint(30) // deprecated: use podmansh.timeout instead, kept for backwards-compatibility
	c.ExitCommandDelay = uint(5 * 60)
	c.Remote = isRemote()
	c.Retry = 3
	c.OCIRuntimes = map[string][]string{
		"crun": {
			"/usr/bin/crun",
			"/usr/sbin/crun",
			"/usr/local/bin/crun",
			"/usr/local/sbin/crun",
			"/sbin/crun",
			"/bin/crun",
			"/run/current-system/sw/bin/crun",
		},
		"crun-vm": {
			"/usr/bin/crun-vm",
			"/usr/local/bin/crun-vm",
			"/usr/local/sbin/crun-vm",
			"/sbin/crun-vm",
			"/bin/crun-vm",
			"/run/current-system/sw/bin/crun-vm",
		},
		"crun-wasm": {
			"/usr/bin/crun-wasm",
			"/usr/sbin/crun-wasm",
			"/usr/local/bin/crun-wasm",
			"/usr/local/sbin/crun-wasm",
			"/sbin/crun-wasm",
			"/bin/crun-wasm",
			"/run/current-system/sw/bin/crun-wasm",
		},
		"runc": {
			"/usr/bin/runc",
			"/usr/sbin/runc",
			"/usr/local/bin/runc",
			"/usr/local/sbin/runc",
			"/sbin/runc",
			"/bin/runc",
			"/usr/lib/cri-o-runc/sbin/runc",
			"/run/current-system/sw/bin/runc",
		},
		"runj": {
			"/usr/local/bin/runj",
		},
		"kata": {
			"/usr/bin/kata-runtime",
			"/usr/sbin/kata-runtime",
			"/usr/local/bin/kata-runtime",
			"/usr/local/sbin/kata-runtime",
			"/sbin/kata-runtime",
			"/bin/kata-runtime",
			"/usr/bin/kata-qemu",
			"/usr/bin/kata-fc",
		},
		"runsc": {
			"/usr/bin/runsc",
			"/usr/sbin/runsc",
			"/usr/local/bin/runsc",
			"/usr/local/sbin/runsc",
			"/bin/runsc",
			"/sbin/runsc",
			"/run/current-system/sw/bin/runsc",
		},
		"youki": {
			"/usr/local/bin/youki",
			"/usr/bin/youki",
			"/bin/youki",
			"/run/current-system/sw/bin/youki",
		},
		"krun": {
			"/usr/bin/krun",
			"/usr/local/bin/krun",
		},
		"ocijail": {
			"/usr/local/bin/ocijail",
		},
	}
	c.OCIRuntimesFlags = map[string][]string{}
	c.PlatformToOCIRuntime = map[string]string{
		"wasi/wasm":   "crun-wasm",
		"wasi/wasm32": "crun-wasm",
		"wasi/wasm64": "crun-wasm",
	}
	// Needs to be called after populating c.OCIRuntimes.
	c.OCIRuntime = c.findRuntime()

	c.ConmonEnvVars.Set([]string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"})
	c.ConmonPath.Set([]string{
		"/usr/libexec/podman/conmon",
		"/usr/local/libexec/podman/conmon",
		"/usr/local/lib/podman/conmon",
		"/usr/bin/conmon",
		"/usr/sbin/conmon",
		"/usr/local/bin/conmon",
		"/usr/local/sbin/conmon",
		"/run/current-system/sw/bin/conmon",
	})
	c.ConmonRsPath.Set([]string{
		"/usr/libexec/podman/conmonrs",
		"/usr/local/libexec/podman/conmonrs",
		"/usr/local/lib/podman/conmonrs",
		"/usr/bin/conmonrs",
		"/usr/sbin/conmonrs",
		"/usr/local/bin/conmonrs",
		"/usr/local/sbin/conmonrs",
		"/run/current-system/sw/bin/conmonrs",
	})
	c.PullPolicy = DefaultPullPolicy
	c.RuntimeSupportsJSON.Set([]string{
		"crun",
		"runc",
		"kata",
		"runsc",
		"youki",
		"krun",
		"ocijail",
	})
	c.RuntimeSupportsNoCgroups.Set([]string{"crun", "krun"})
	c.RuntimeSupportsKVM.Set([]string{"kata", "kata-runtime", "kata-qemu", "kata-fc", "krun"})
	c.NoPivotRoot = false

	c.InfraImage = DefaultInfraImage
	c.EnablePortReservation = true
	c.NumLocks = 2048
	c.EventsLogger = defaultEventsLogger()
	c.DetachKeys = DefaultDetachKeys
	c.SDNotify = false
	// TODO - ideally we should expose a `type LockType string` along with
	// constants.
	c.LockType = getDefaultLockType()
	c.ChownCopiedFiles = true

	c.PodExitPolicy = defaultPodExitPolicy
	c.SSHConfig = getDefaultSSHConfig()
	c.KubeGenerateType = "pod"

	return c, nil
}

func defaultTmpDir() (string, error) {
	// NOTE: For now we want Windows to use system locations.
	// GetRootlessUID == -1 on Windows, so exclude negative range
	rootless := unshare.GetRootlessUID() > 0
	if !rootless {
		return getLibpodTmpDir(), nil
	}

	runtimeDir, err := homedir.GetRuntimeDir()
	if err != nil {
		return "", err
	}
	libpodRuntimeDir := filepath.Join(runtimeDir, "libpod")

	if err := os.Mkdir(libpodRuntimeDir, 0o700|os.ModeSticky); err != nil {
		if !os.IsExist(err) {
			return "", err
		} else if err := os.Chmod(libpodRuntimeDir, 0o700|os.ModeSticky); err != nil {
			// The directory already exists, so we try to make sure it's private and has the sticky bit set on it.
			return "", fmt.Errorf("set sticky bit on: %w", err)
		}
	}
	return filepath.Join(libpodRuntimeDir, "tmp"), nil
}

// NetNS returns the default network namespace.
func (c *Config) NetNS() string {
	return c.Containers.NetNS
}

func (c *EngineConfig) EventsLogMaxSize() uint64 {
	return uint64(c.EventsLogFileMaxSize)
}

// SecurityOptions returns the default security options.
func (c *Config) SecurityOptions() []string {
	securityOpts := []string{}
	if c.Containers.SeccompProfile != "" && c.Containers.SeccompProfile != SeccompDefaultPath {
		securityOpts = append(securityOpts, "seccomp="+c.Containers.SeccompProfile)
	}
	if apparmor.IsEnabled() && c.Containers.ApparmorProfile != "" {
		securityOpts = append(securityOpts, "apparmor="+c.Containers.ApparmorProfile)
	}
	if selinux.GetEnabled() && !c.Containers.EnableLabeling {
		securityOpts = append(securityOpts, "label="+selinux.DisableSecOpt()[0])
	}
	return securityOpts
}

// Sysctls returns the default sysctls to set in containers.
func (c *Config) Sysctls() []string {
	return c.Containers.DefaultSysctls.Get()
}

// Volumes returns the default set of volumes that should be mounted in containers.
func (c *Config) Volumes() []string {
	return c.Containers.Volumes.Get()
}

// Mounts returns the default set of mounts that should be mounted in containers.
func (c *Config) Mounts() []string {
	return c.Containers.Mounts.Get()
}

// Devices returns the default additional devices for containers.
func (c *Config) Devices() []string {
	return c.Containers.Devices.Get()
}

// DNSServers returns the default DNS servers to add to resolv.conf in containers.
func (c *Config) DNSServers() []string {
	return c.Containers.DNSServers.Get()
}

// DNSSearches returns the default DNS searches to add to resolv.conf in containers.
func (c *Config) DNSSearches() []string {
	return c.Containers.DNSSearches.Get()
}

// DNSOptions returns the default DNS options to add to resolv.conf in containers.
func (c *Config) DNSOptions() []string {
	return c.Containers.DNSOptions.Get()
}

// Env returns the default additional environment variables to add to containers.
func (c *Config) Env() []string {
	return c.Containers.Env.Get()
}

// IPCNS returns the default IPC Namespace configuration to run containers with.
func (c *Config) IPCNS() string {
	return c.Containers.IPCNS
}

// PidNS returns the default PID Namespace configuration to run containers with.
func (c *Config) PidNS() string {
	return c.Containers.PidNS
}

// CgroupNS returns the default Cgroup Namespace configuration to run containers with.
func (c *Config) CgroupNS() string {
	return c.Containers.CgroupNS
}

// Cgroups returns whether to run containers in their own control groups, as configured by the "cgroups" setting in containers.conf.
func (c *Config) Cgroups() string {
	return c.Containers.Cgroups
}

// UTSNS returns the default UTS Namespace configuration to run containers with.
func (c *Config) UTSNS() string {
	return c.Containers.UTSNS
}

// ShmSize returns the default size for temporary file systems to use in containers.
func (c *Config) ShmSize() string {
	return c.Containers.ShmSize
}

// Ulimits returns the default ulimits to use in containers.
func (c *Config) Ulimits() []string {
	return c.Containers.DefaultUlimits.Get()
}

// PidsLimit returns the default maximum number of pids to use in containers.
func (c *Config) PidsLimit() int64 {
	if unshare.IsRootless() {
		if c.Engine.CgroupManager != SystemdCgroupsManager {
			return 0
		}
		cgroup2, _ := cgroupv2.Enabled()
		if !cgroup2 {
			return 0
		}
	}

	return c.Containers.PidsLimit
}

// DetachKeys returns the default detach keys to detach from a container.
func (c *Config) DetachKeys() string {
	return c.Engine.DetachKeys
}

// TZ returns the timezone to set in containers.
func (c *Config) TZ() string {
	return c.Containers.TZ
}

func (c *Config) Umask() string {
	return c.Containers.Umask
}

// LogDriver returns the logging driver to be used
// currently k8s-file or journald.
func (c *Config) LogDriver() string {
	return c.Containers.LogDriver
}

// MachineVolumes returns volumes to mount into the VM.
func (c *Config) MachineVolumes() ([]string, error) {
	return machineVolumes(c.Machine.Volumes.Get())
}

func machineVolumes(volumes []string) ([]string, error) {
	translatedVolumes := []string{}
	for _, v := range volumes {
		vol := os.ExpandEnv(v)
		split := strings.Split(vol, ":")
		if len(split) < 2 || len(split) > 3 {
			return nil, fmt.Errorf("invalid machine volume %s, 2 or 3 fields required", v)
		}
		if split[0] == "" || split[1] == "" {
			return nil, fmt.Errorf("invalid machine volume %s, fields must container data", v)
		}
		translatedVolumes = append(translatedVolumes, vol)
	}
	return translatedVolumes, nil
}

func getDefaultSSHConfig() string {
	if path, ok := os.LookupEnv("CONTAINERS_SSH_CONF"); ok {
		return path
	}
	dirname := homedir.Get()
	return filepath.Join(dirname, ".ssh", "config")
}

// getDefaultMachineUser returns the user to use for rootless podman
// This is only for the apple, hyperv, and qemu implementations.
// WSL's user will be hardcoded in podman to "user".
func getDefaultMachineUser() string {
	return "core"
}
