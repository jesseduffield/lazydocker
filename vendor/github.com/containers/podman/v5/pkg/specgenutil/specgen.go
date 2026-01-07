package specgenutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/containers/podman/v5/cmd/podman/parse"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/domain/entities"
	envLib "github.com/containers/podman/v5/pkg/env"
	"github.com/containers/podman/v5/pkg/namespaces"
	"github.com/containers/podman/v5/pkg/specgen"
	systemdDefine "github.com/containers/podman/v5/pkg/systemd/define"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/docker/go-units"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/manifest"
)

const (
	rlimitPrefix = "rlimit_"
)

func getCPULimits(c *entities.ContainerCreateOptions) *specs.LinuxCPU {
	cpu := &specs.LinuxCPU{}
	hasLimits := false

	if c.CPUS > 0 {
		period, quota := util.CoresToPeriodAndQuota(c.CPUS)

		cpu.Period = &period
		cpu.Quota = &quota
		hasLimits = true
	}
	if c.CPUShares > 0 {
		cpu.Shares = &c.CPUShares
		hasLimits = true
	}
	if c.CPUPeriod > 0 {
		cpu.Period = &c.CPUPeriod
		hasLimits = true
	}
	if c.CPUSetCPUs != "" {
		cpu.Cpus = c.CPUSetCPUs
		hasLimits = true
	}
	if c.CPUSetMems != "" {
		cpu.Mems = c.CPUSetMems
		hasLimits = true
	}
	if c.CPUQuota > 0 {
		cpu.Quota = &c.CPUQuota
		hasLimits = true
	}
	if c.CPURTPeriod > 0 {
		cpu.RealtimePeriod = &c.CPURTPeriod
		hasLimits = true
	}
	if c.CPURTRuntime > 0 {
		cpu.RealtimeRuntime = &c.CPURTRuntime
		hasLimits = true
	}

	if !hasLimits {
		return nil
	}
	return cpu
}

func getIOLimits(s *specgen.SpecGenerator, c *entities.ContainerCreateOptions) (*specs.LinuxBlockIO, error) {
	var err error
	io := &specs.LinuxBlockIO{}
	if s.ResourceLimits == nil {
		s.ResourceLimits = &specs.LinuxResources{}
	}
	hasLimits := false
	if b := c.BlkIOWeight; len(b) > 0 {
		if s.ResourceLimits.BlockIO == nil {
			s.ResourceLimits.BlockIO = &specs.LinuxBlockIO{}
		}
		u, err := strconv.ParseUint(b, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid value for blkio-weight: %w", err)
		}
		nu := uint16(u)
		io.Weight = &nu
		s.ResourceLimits.BlockIO.Weight = &nu
		hasLimits = true
	}

	if len(c.BlkIOWeightDevice) > 0 {
		if s.WeightDevice, err = parseWeightDevices(c.BlkIOWeightDevice); err != nil {
			return nil, err
		}
		hasLimits = true
	}

	if bps := c.DeviceReadBPs; len(bps) > 0 {
		if s.ThrottleReadBpsDevice, err = parseThrottleBPSDevices(bps); err != nil {
			return nil, err
		}
		hasLimits = true
	}

	if bps := c.DeviceWriteBPs; len(bps) > 0 {
		if s.ThrottleWriteBpsDevice, err = parseThrottleBPSDevices(bps); err != nil {
			return nil, err
		}
		hasLimits = true
	}

	if iops := c.DeviceReadIOPs; len(iops) > 0 {
		if s.ThrottleReadIOPSDevice, err = parseThrottleIOPsDevices(iops); err != nil {
			return nil, err
		}
		hasLimits = true
	}

	if iops := c.DeviceWriteIOPs; len(iops) > 0 {
		if s.ThrottleWriteIOPSDevice, err = parseThrottleIOPsDevices(iops); err != nil {
			return nil, err
		}
		hasLimits = true
	}

	if !hasLimits {
		return nil, nil
	}
	return io, nil
}

func LimitToSwap(memory *specs.LinuxMemory, swap string, ml int64) {
	if ml > 0 {
		memory.Limit = &ml
		if swap == "" {
			limit := 2 * ml
			memory.Swap = &(limit)
		}
	}
}

func getMemoryLimits(c *entities.ContainerCreateOptions) (*specs.LinuxMemory, error) {
	var err error
	memory := &specs.LinuxMemory{}
	hasLimits := false
	if m := c.Memory; len(m) > 0 {
		ml, err := units.RAMInBytes(m)
		if err != nil {
			return nil, fmt.Errorf("invalid value for memory: %w", err)
		}
		LimitToSwap(memory, c.MemorySwap, ml)
		hasLimits = true
	}
	if m := c.MemoryReservation; len(m) > 0 {
		mr, err := units.RAMInBytes(m)
		if err != nil {
			return nil, fmt.Errorf("invalid value for memory: %w", err)
		}
		memory.Reservation = &mr
		hasLimits = true
	}
	if m := c.MemorySwap; len(m) > 0 {
		var ms int64
		// only set memory swap if it was set
		// -1 indicates unlimited
		if m != "-1" {
			ms, err = units.RAMInBytes(m)
			memory.Swap = &ms
			if err != nil {
				return nil, fmt.Errorf("invalid value for memory: %w", err)
			}
			hasLimits = true
		}
	}
	if c.MemorySwappiness >= 0 {
		swappiness := uint64(c.MemorySwappiness)
		memory.Swappiness = &swappiness
		hasLimits = true
	}
	if c.OOMKillDisable {
		memory.DisableOOMKiller = &c.OOMKillDisable
		hasLimits = true
	}
	if !hasLimits {
		return nil, nil
	}
	return memory, nil
}

func setNamespaces(rtc *config.Config, s *specgen.SpecGenerator, c *entities.ContainerCreateOptions) error {
	var err error

	if c.PID != "" {
		s.PidNS, err = specgen.ParseNamespace(c.PID)
		if err != nil {
			return err
		}
	}
	if c.IPC != "" {
		s.IpcNS, err = specgen.ParseIPCNamespace(c.IPC)
		if err != nil {
			return err
		}
	}
	if c.UTS != "" {
		s.UtsNS, err = specgen.ParseNamespace(c.UTS)
		if err != nil {
			return err
		}
	}
	if c.CgroupNS != "" {
		s.CgroupNS, err = specgen.ParseNamespace(c.CgroupNS)
		if err != nil {
			return err
		}
	}
	userns := c.UserNS
	// caller must make sure s.Pod is set before calling this function.
	if userns == "" && s.Pod == "" {
		if ns, ok := os.LookupEnv("PODMAN_USERNS"); ok {
			userns = ns
		} else {
			// TODO: This should be moved into pkg/specgen/generate so we don't use the client's containers.conf
			userns = rtc.Containers.UserNS
		}
	}
	// userns must be treated differently
	if userns != "" {
		s.UserNS, err = specgen.ParseUserNamespace(userns)
		if err != nil {
			return err
		}
	}
	if c.Net != nil {
		s.NetNS = c.Net.Network
	}

	if s.IDMappings == nil {
		userNS := namespaces.UsernsMode(s.UserNS.NSMode)
		tempIDMap, err := util.ParseIDMapping(namespaces.UsernsMode(userns), []string{}, []string{}, "", "")
		if err != nil {
			return err
		}
		s.IDMappings, err = util.ParseIDMapping(userNS, c.UIDMap, c.GIDMap, c.SubUIDName, c.SubGIDName)
		if err != nil {
			return err
		}
		if len(s.IDMappings.GIDMap) == 0 {
			s.IDMappings.AutoUserNsOpts.AdditionalGIDMappings = tempIDMap.AutoUserNsOpts.AdditionalGIDMappings
			if s.UserNS.NSMode == specgen.NamespaceMode("auto") {
				s.IDMappings.AutoUserNs = true
			}
		}
		if len(s.IDMappings.UIDMap) == 0 {
			s.IDMappings.AutoUserNsOpts.AdditionalUIDMappings = tempIDMap.AutoUserNsOpts.AdditionalUIDMappings
			if s.UserNS.NSMode == specgen.NamespaceMode("auto") {
				s.IDMappings.AutoUserNs = true
			}
		}
		if tempIDMap.AutoUserNsOpts.Size != 0 {
			s.IDMappings.AutoUserNsOpts.Size = tempIDMap.AutoUserNsOpts.Size
		}
		// If some mappings are specified, assume a private user namespace
		if userNS.IsDefaultValue() && (!s.IDMappings.HostUIDMapping || !s.IDMappings.HostGIDMapping) {
			s.UserNS.NSMode = specgen.Private
		} else {
			s.UserNS.NSMode = specgen.NamespaceMode(userNS)
		}
	}

	return nil
}

func GenRlimits(ulimits []string) ([]specs.POSIXRlimit, error) {
	rlimits := make([]specs.POSIXRlimit, 0, len(ulimits))
	// Rlimits/Ulimits
	for _, ulimit := range ulimits {
		if ulimit == "host" {
			rlimits = nil
			break
		}
		// `ulimitNameMapping` from go-units uses lowercase and names
		// without prefixes, e.g. `RLIMIT_NOFILE` should be converted to `nofile`.
		// https://github.com/containers/podman/issues/9803
		u := strings.TrimPrefix(strings.ToLower(ulimit), rlimitPrefix)
		ul, err := units.ParseUlimit(u)
		if err != nil {
			return nil, fmt.Errorf("ulimit option %q requires name=SOFT:HARD, failed to be parsed: %w", u, err)
		}
		rl := specs.POSIXRlimit{
			Type: ul.Name,
			Hard: uint64(ul.Hard),
			Soft: uint64(ul.Soft),
		}
		rlimits = append(rlimits, rl)
	}
	return rlimits, nil
}

func currentLabelOpts() ([]string, error) {
	label, err := selinux.CurrentLabel()
	if err != nil {
		return nil, err
	}
	if label == "" {
		return nil, nil
	}
	con, err := selinux.NewContext(label)
	if err != nil {
		return nil, err
	}
	return []string{
		fmt.Sprintf("label=user:%s", con["user"]),
		fmt.Sprintf("label=role:%s", con["role"]),
	}, nil
}

func FillOutSpecGen(s *specgen.SpecGenerator, c *entities.ContainerCreateOptions, args []string) error {
	rtc, err := config.Default()
	if err != nil {
		return err
	}

	// TODO: This needs to move into pkg/specgen/generate so we aren't using containers.conf on the client.
	if rtc.Containers.EnableLabeledUsers {
		defSecurityOpts, err := currentLabelOpts()
		if err != nil {
			return err
		}

		c.SecurityOpt = append(defSecurityOpts, c.SecurityOpt...)
	}

	// validate flags as needed
	if err := validate(c); err != nil {
		return err
	}
	s.User = c.User
	var inputCommand []string
	if !c.IsInfra {
		if len(args) > 1 {
			inputCommand = args[1:]
		}
	}

	if len(c.HealthCmd) > 0 {
		if c.NoHealthCheck {
			return errors.New("cannot specify both --no-healthcheck and --health-cmd")
		}
		s.HealthConfig, err = MakeHealthCheckFromCli(c.HealthCmd, c.HealthInterval, c.HealthRetries, c.HealthTimeout, c.HealthStartPeriod, false)
		if err != nil {
			return err
		}
	} else if c.NoHealthCheck {
		s.HealthConfig = &manifest.Schema2HealthConfig{
			Test: []string{"NONE"},
		}
	}

	onFailureAction, err := define.ParseHealthCheckOnFailureAction(c.HealthOnFailure)
	if err != nil {
		return err
	}
	s.HealthCheckOnFailureAction = onFailureAction

	s.HealthLogDestination = c.HealthLogDestination

	s.HealthMaxLogCount = c.HealthMaxLogCount

	s.HealthMaxLogSize = c.HealthMaxLogSize

	if c.StartupHCCmd != "" {
		if c.NoHealthCheck {
			return errors.New("cannot specify both --no-healthcheck and --health-startup-cmd")
		}
		// The hardcoded "1s" will be discarded, as the startup
		// healthcheck does not have a period. So just hardcode
		// something that parses correctly.
		tmpHcConfig, err := MakeHealthCheckFromCli(c.StartupHCCmd, c.StartupHCInterval, c.StartupHCRetries, c.StartupHCTimeout, "1s", true)
		if err != nil {
			return err
		}
		s.StartupHealthConfig = new(define.StartupHealthCheck)
		s.StartupHealthConfig.Test = tmpHcConfig.Test
		s.StartupHealthConfig.Interval = tmpHcConfig.Interval
		s.StartupHealthConfig.Timeout = tmpHcConfig.Timeout
		s.StartupHealthConfig.Retries = tmpHcConfig.Retries
		s.StartupHealthConfig.Successes = int(c.StartupHCSuccesses)
	}

	if len(s.Pod) == 0 || len(c.Pod) > 0 {
		s.Pod = c.Pod
	}

	if len(c.PodIDFile) > 0 {
		if len(s.Pod) > 0 {
			return errors.New("cannot specify both --pod and --pod-id-file")
		}
		podID, err := ReadPodIDFile(c.PodIDFile)
		if err != nil {
			return err
		}
		s.Pod = podID
	}

	// Important s.Pod must be set above here.
	if err := setNamespaces(rtc, s, c); err != nil {
		return err
	}

	if s.Terminal == nil {
		s.Terminal = &c.TTY
	}

	if err := verifyExpose(c.Expose); err != nil {
		return err
	}
	// We are not handling the Expose flag yet.
	// s.PortsExpose = c.Expose
	if c.Net != nil {
		s.PortMappings = c.Net.PublishPorts
	}
	if s.PublishExposedPorts == nil {
		s.PublishExposedPorts = &c.PublishAll
	}

	expose, err := CreateExpose(c.Expose)
	if err != nil {
		return err
	}

	if len(s.Expose) == 0 {
		s.Expose = expose
	}

	if sig := c.StopSignal; len(sig) > 0 {
		stopSignal, err := util.ParseSignal(sig)
		if err != nil {
			return err
		}
		s.StopSignal = &stopSignal
	}

	// ENVIRONMENT VARIABLES
	//
	// Precedence order (higher index wins):
	//  1) containers.conf (EnvHost, EnvHTTP, Env) 2) image data, 3 User EnvHost/EnvHTTP, 4) env-file, 5) env
	// containers.conf handled and image data handled on the server side
	// user specified EnvHost and EnvHTTP handled on Server Side relative to Server
	// env-file and env handled on client side
	var env map[string]string

	// First transform the os env into a map. We need it for the labels later in
	// any case.
	osEnv := envLib.Map(os.Environ())

	if s.EnvHost == nil {
		s.EnvHost = &c.EnvHost
	}

	if s.HTTPProxy == nil {
		s.HTTPProxy = &c.HTTPProxy
	}

	// env-file overrides any previous variables
	for _, f := range c.EnvFile {
		fileEnv, err := envLib.ParseFile(f)
		if err != nil {
			return err
		}
		// File env is overridden by env.
		env = envLib.Join(env, fileEnv)
	}

	parsedEnv, err := envLib.ParseSlice(c.Env)
	if err != nil {
		return err
	}

	if len(s.Env) == 0 {
		s.Env = envLib.Join(env, parsedEnv)
	}

	// LABEL VARIABLES
	labels, err := parse.GetAllLabels(c.LabelFile, c.Label)
	if err != nil {
		return fmt.Errorf("unable to process labels: %w", err)
	}

	if systemdUnit, exists := osEnv[systemdDefine.EnvVariable]; exists {
		labels[systemdDefine.EnvVariable] = systemdUnit
	}

	if len(s.Labels) == 0 {
		s.Labels = labels
	}

	// Intel RDT CAT
	if c.IntelRdtClosID != "" {
		s.IntelRdt = &specs.LinuxIntelRdt{}
		s.IntelRdt.ClosID = c.IntelRdtClosID
	}

	// ANNOTATIONS
	annotations := make(map[string]string)

	// Last, add user annotations
	for _, annotation := range c.Annotation {
		key, val, hasVal := strings.Cut(annotation, "=")
		if !hasVal {
			return errors.New("annotations must be formatted KEY=VALUE")
		}
		annotations[key] = val
	}
	if len(s.Annotations) == 0 {
		s.Annotations = annotations
	}
	// Add the user namespace configuration to the annotations
	if c.UserNS != "" {
		s.Annotations[define.UserNsAnnotation] = c.UserNS
	}

	if c.PIDsLimit != nil {
		s.Annotations[define.PIDsLimitAnnotation] = strconv.FormatInt(*c.PIDsLimit, 10)
	}

	if c.CPUSetCPUs != "" {
		s.Annotations[define.CpusetAnnotation] = c.CPUSetCPUs
	}

	if c.CPUSetMems != "" {
		s.Annotations[define.MemoryNodesAnnotation] = c.CPUSetMems
	}

	if len(c.StorageOpts) > 0 {
		opts := make(map[string]string, len(c.StorageOpts))
		for _, opt := range c.StorageOpts {
			key, val, hasVal := strings.Cut(opt, "=")
			if !hasVal {
				return errors.New("storage-opt must be formatted KEY=VALUE")
			}
			opts[key] = val
		}
		s.StorageOpts = opts
	}
	if len(s.WorkDir) == 0 {
		s.WorkDir = c.Workdir
	}
	if c.Entrypoint != nil {
		entrypoint := []string{}
		// Check if entrypoint specified is json
		if err := json.Unmarshal([]byte(*c.Entrypoint), &entrypoint); err != nil {
			entrypoint = append(entrypoint, *c.Entrypoint)
		}
		s.Entrypoint = entrypoint
	}

	if len(inputCommand) > 0 {
		s.Command = inputCommand
	}

	// SHM Size
	if c.ShmSize != "" {
		val, err := units.RAMInBytes(c.ShmSize)

		if err != nil {
			return fmt.Errorf("unable to translate --shm-size: %w", err)
		}

		s.ShmSize = &val
	}

	// SHM Size Systemd
	if c.ShmSizeSystemd != "" {
		val, err := units.RAMInBytes(c.ShmSizeSystemd)
		if err != nil {
			return fmt.Errorf("unable to translate --shm-size-systemd: %w", err)
		}

		s.ShmSizeSystemd = &val
	}

	if c.Net != nil {
		s.Networks = c.Net.Networks
	}

	if c.Net != nil {
		s.HostAdd = c.Net.AddHosts
		s.BaseHostsFile = c.Net.HostsFile
		s.UseImageResolvConf = &c.Net.UseImageResolvConf
		s.DNSServers = c.Net.DNSServers
		s.DNSSearch = c.Net.DNSSearch
		s.DNSOptions = c.Net.DNSOptions
		s.NetworkOptions = c.Net.NetworkOptions
		s.UseImageHostname = &c.Net.NoHostname
		s.UseImageHosts = &c.Net.NoHosts
	}
	if len(s.HostUsers) == 0 || len(c.HostUsers) != 0 {
		s.HostUsers = c.HostUsers
	}
	if len(c.ImageVolume) != 0 {
		if len(s.ImageVolumeMode) == 0 {
			s.ImageVolumeMode = c.ImageVolume
		}
	}
	if s.ImageVolumeMode == define.TypeBind {
		s.ImageVolumeMode = "anonymous"
	}

	if len(s.Systemd) == 0 || len(c.Systemd) != 0 {
		s.Systemd = strings.ToLower(c.Systemd)
	}
	if len(s.SdNotifyMode) == 0 || len(c.SdNotifyMode) != 0 {
		s.SdNotifyMode = c.SdNotifyMode
	}
	if s.ResourceLimits == nil {
		s.ResourceLimits = &specs.LinuxResources{}
	}

	s.ResourceLimits, err = GetResources(s, c)
	if err != nil {
		return err
	}

	if s.LogConfiguration == nil {
		s.LogConfiguration = &specgen.LogConfig{}
	}

	if ld := c.LogDriver; len(ld) > 0 {
		s.LogConfiguration.Driver = ld
	}
	if len(s.CgroupParent) == 0 || len(c.CgroupParent) != 0 {
		s.CgroupParent = c.CgroupParent
	}
	if len(s.CgroupsMode) == 0 {
		s.CgroupsMode = c.CgroupsMode
	}

	if len(s.Groups) == 0 || len(c.GroupAdd) != 0 {
		s.Groups = c.GroupAdd
	}

	if len(s.Hostname) == 0 || len(c.Hostname) != 0 {
		s.Hostname = c.Hostname
	}
	sysctl := map[string]string{}
	if ctl := c.Sysctl; len(ctl) > 0 {
		sysctl, err = util.ValidateSysctls(ctl)
		if err != nil {
			return err
		}
	}
	if len(s.Sysctl) == 0 || len(c.Sysctl) != 0 {
		s.Sysctl = sysctl
	}

	if len(s.CapAdd) == 0 || len(c.CapAdd) != 0 {
		s.CapAdd = c.CapAdd
	}
	if len(s.CapDrop) == 0 || len(c.CapDrop) != 0 {
		s.CapDrop = c.CapDrop
	}
	if s.Privileged == nil {
		s.Privileged = &c.Privileged
	}
	if s.ReadOnlyFilesystem == nil {
		s.ReadOnlyFilesystem = &c.ReadOnly
	}
	if len(s.ConmonPidFile) == 0 || len(c.ConmonPIDFile) != 0 {
		s.ConmonPidFile = c.ConmonPIDFile
	}

	if len(s.DependencyContainers) == 0 || len(c.Requires) != 0 {
		s.DependencyContainers = c.Requires
	}

	// Only add ReadWrite tmpfs mounts iff the container is
	// being run ReadOnly and ReadWriteTmpFS is not disabled,
	// (user specifying --read-only-tmpfs=false.)
	localRWTmpfs := c.ReadOnly && c.ReadWriteTmpFS
	s.ReadWriteTmpfs = &localRWTmpfs

	//  TODO convert to map?
	// check if key=value and convert
	sysmap := make(map[string]string)
	for _, ctl := range c.Sysctl {
		key, val, hasVal := strings.Cut(ctl, "=")
		if !hasVal {
			return fmt.Errorf("invalid sysctl value %q", ctl)
		}
		sysmap[key] = val
	}
	if len(s.Sysctl) == 0 || len(c.Sysctl) != 0 {
		s.Sysctl = sysmap
	}

	if c.CIDFile != "" {
		s.Annotations[define.InspectAnnotationCIDFile] = c.CIDFile
	}

	for _, opt := range c.SecurityOpt {
		// Docker deprecated the ":" syntax but still supports it,
		// so we need to as well
		var key, val string
		var hasVal bool
		if strings.Contains(opt, "=") {
			key, val, hasVal = strings.Cut(opt, "=")
		} else {
			key, val, hasVal = strings.Cut(opt, ":")
		}
		if !hasVal &&
			key != "no-new-privileges" {
			return fmt.Errorf("invalid --security-opt 1: %q", opt)
		}
		switch key {
		case "apparmor":
			s.ContainerSecurityConfig.ApparmorProfile = val
			s.Annotations[define.InspectAnnotationApparmor] = val
		case "label":
			if val == "nested" {
				localTrue := true
				s.ContainerSecurityConfig.LabelNested = &localTrue
				continue
			}
			// TODO selinux opts and label opts are the same thing
			s.ContainerSecurityConfig.SelinuxOpts = append(s.ContainerSecurityConfig.SelinuxOpts, val)
			s.Annotations[define.InspectAnnotationLabel] = strings.Join(s.ContainerSecurityConfig.SelinuxOpts, ",label=")
		case "mask":
			s.ContainerSecurityConfig.Mask = append(s.ContainerSecurityConfig.Mask, strings.Split(val, ":")...)
		case "proc-opts":
			s.ProcOpts = strings.Split(val, ",")
		case "seccomp":
			convertedPath := val
			// Do not try to convert special value "unconfined",
			// https://github.com/containers/podman/issues/26855
			if val != "unconfined" {
				convertedPath, err = specgen.ConvertWinMountPath(val)
				if err != nil {
					// If the conversion fails, use the original path
					convertedPath = val
				}
			}
			s.SeccompProfilePath = convertedPath
			s.Annotations[define.InspectAnnotationSeccomp] = convertedPath
			// this option is for docker compatibility, it is the same as unmask=ALL
		case "systempaths":
			if val == "unconfined" {
				s.ContainerSecurityConfig.Unmask = append(s.ContainerSecurityConfig.Unmask, []string{"ALL"}...)
			} else {
				return fmt.Errorf("invalid systempaths option %q, only `unconfined` is supported", val)
			}
		case "unmask":
			s.ContainerSecurityConfig.Unmask = append(s.ContainerSecurityConfig.Unmask, strings.Split(val, ":")...)
		case "no-new-privileges":
			noNewPrivileges := true
			if hasVal {
				noNewPrivileges, err = strconv.ParseBool(val)
				if err != nil {
					return fmt.Errorf("invalid --security-opt 2: %q", opt)
				}
			}
			s.ContainerSecurityConfig.NoNewPrivileges = &noNewPrivileges
		default:
			return fmt.Errorf("invalid --security-opt 2: %q", opt)
		}
	}

	if len(s.SeccompPolicy) == 0 || len(c.SeccompPolicy) != 0 {
		s.SeccompPolicy = c.SeccompPolicy
	}

	if len(s.VolumesFrom) == 0 || len(c.VolumesFrom) != 0 {
		s.VolumesFrom = c.VolumesFrom
	}

	// Only add read-only tmpfs mounts in case that we are read-only and the
	// read-only tmpfs flag has been set.
	containerMounts, err := parseVolumes(rtc, c.Volume, c.Mount, c.TmpFS)
	if err != nil {
		return err
	}
	if len(s.Mounts) == 0 || len(c.Mount) != 0 {
		s.Mounts = containerMounts.mounts
	}
	if len(s.Volumes) == 0 || len(c.Volume) != 0 {
		s.Volumes = containerMounts.volumes
	}

	if s.LabelNested != nil && *s.LabelNested {
		// Need to unmask the SELinux file system
		s.Unmask = append(s.Unmask, "/sys/fs/selinux", "/proc")
		s.Mounts = append(s.Mounts, specs.Mount{
			Source:      "/sys/fs/selinux",
			Destination: "/sys/fs/selinux",
			Type:        define.TypeBind,
		})
		s.Annotations[define.RunOCIMountContextType] = "rootcontext"
	}
	// TODO make sure these work in clone
	if len(s.OverlayVolumes) == 0 {
		s.OverlayVolumes = containerMounts.overlayVolumes
	}
	if len(s.ImageVolumes) == 0 {
		s.ImageVolumes = containerMounts.imageVolumes
	}
	if len(s.ArtifactVolumes) == 0 {
		s.ArtifactVolumes = containerMounts.artifactVolumes
	}

	devices := c.Devices
	for _, gpu := range c.GPUs {
		devices = append(devices, "nvidia.com/gpu="+gpu)
	}

	for _, dev := range devices {
		s.Devices = append(s.Devices, specs.LinuxDevice{Path: dev})
	}

	for _, rule := range c.DeviceCgroupRule {
		dev, err := parseLinuxResourcesDeviceAccess(rule)
		if err != nil {
			return err
		}
		s.DeviceCgroupRule = append(s.DeviceCgroupRule, dev)
	}

	if s.Init == nil {
		s.Init = &c.Init
	}
	if len(s.InitPath) == 0 || len(c.InitPath) != 0 {
		s.InitPath = c.InitPath
	}
	if s.Stdin == nil {
		s.Stdin = &c.Interactive
	}
	// quiet
	// DeviceCgroupRules: c.StringSlice("device-cgroup-rule"),

	// Rlimits/Ulimits
	s.Rlimits, err = GenRlimits(c.Ulimit)
	if err != nil {
		return err
	}

	if rtc.Containers.LogPath != "" {
		s.LogConfiguration.Path = rtc.Containers.LogPath
	}

	logOpts := make(map[string]string)
	for _, o := range c.LogOptions {
		key, val, hasVal := strings.Cut(o, "=")
		if !hasVal {
			return fmt.Errorf("invalid log option %q", o)
		}
		switch strings.ToLower(key) {
		case "driver":
			s.LogConfiguration.Driver = val
		case "path":
			s.LogConfiguration.Path = val
		case "max-size":
			logSize, err := units.FromHumanSize(val)
			if err != nil {
				return err
			}
			s.LogConfiguration.Size = logSize
		default:
			logOpts[key] = val
		}
	}

	if len(s.LogConfiguration.Options) == 0 || len(c.LogOptions) != 0 {
		s.LogConfiguration.Options = logOpts
	}
	if len(s.Name) == 0 || len(c.Name) != 0 {
		s.Name = c.Name
	}

	if c.PreserveFDs != 0 && c.PreserveFD != nil {
		return errors.New("cannot specify both --preserve-fds and --preserve-fd")
	}

	if s.PreserveFDs == 0 || c.PreserveFDs != 0 {
		s.PreserveFDs = c.PreserveFDs
	}
	if s.PreserveFD == nil || c.PreserveFD != nil {
		s.PreserveFD = c.PreserveFD
	}

	if s.OOMScoreAdj == nil || c.OOMScoreAdj != nil {
		s.OOMScoreAdj = c.OOMScoreAdj
	}
	if c.Restart != "" {
		policy, retries, err := util.ParseRestartPolicy(c.Restart)
		if err != nil {
			return err
		}
		s.RestartPolicy = policy
		s.RestartRetries = &retries
	}

	if len(s.Secrets) == 0 || len(c.Secrets) != 0 {
		s.Secrets, s.EnvSecrets, err = parseSecrets(c.Secrets)
		if err != nil {
			return err
		}
	}

	if c.Personality != "" {
		s.Personality = &specs.LinuxPersonality{}
		s.Personality.Domain = specs.LinuxPersonalityDomain(c.Personality)
	}

	if s.Remove == nil {
		s.Remove = &c.Rm
	}
	if s.StopTimeout == nil || c.StopTimeout != 0 {
		s.StopTimeout = &c.StopTimeout
	}
	if s.Timeout == 0 || c.Timeout != 0 {
		s.Timeout = c.Timeout
	}
	if len(s.Timezone) == 0 || len(c.Timezone) != 0 {
		s.Timezone = c.Timezone
	}
	if len(s.Umask) == 0 || len(c.Umask) != 0 {
		s.Umask = c.Umask
	}
	if len(s.PidFile) == 0 || len(c.PidFile) != 0 {
		s.PidFile = c.PidFile
	}
	if s.Volatile == nil {
		s.Volatile = &c.Rm
	}
	if len(s.EnvMerge) == 0 || len(c.EnvMerge) != 0 {
		s.EnvMerge = c.EnvMerge
	}
	if len(s.UnsetEnv) == 0 || len(c.UnsetEnv) != 0 {
		s.UnsetEnv = c.UnsetEnv
	}
	if s.UnsetEnvAll == nil {
		s.UnsetEnvAll = &c.UnsetEnvAll
	}
	if len(s.ChrootDirs) == 0 || len(c.ChrootDirs) != 0 {
		s.ChrootDirs = c.ChrootDirs
	}

	// Initcontainers
	if len(s.InitContainerType) == 0 || len(c.InitContainerType) != 0 {
		s.InitContainerType = c.InitContainerType
	}

	t := true
	if s.Passwd == nil {
		s.Passwd = &t
	}

	if len(s.PasswdEntry) == 0 || len(c.PasswdEntry) != 0 {
		s.PasswdEntry = c.PasswdEntry
	}

	if len(s.GroupEntry) == 0 || len(c.GroupEntry) != 0 {
		s.GroupEntry = c.GroupEntry
	}

	return nil
}

func MakeHealthCheckFromCli(inCmd, interval string, retries uint, timeout, startPeriod string, isStartup bool) (*manifest.Schema2HealthConfig, error) {
	cmdArr := []string{}
	isArr := true
	err := json.Unmarshal([]byte(inCmd), &cmdArr) // array unmarshalling
	if err != nil {
		cmdArr = strings.SplitN(inCmd, " ", 2) // default for compat
		isArr = false
	}
	// Every healthcheck requires a command
	if len(cmdArr) == 0 {
		return nil, errors.New("must define a healthcheck command for all healthchecks")
	}

	var concat string
	if strings.ToUpper(cmdArr[0]) == define.HealthConfigTestCmd || strings.ToUpper(cmdArr[0]) == define.HealthConfigTestNone { // this is for compat, we are already split properly for most compat cases
		cmdArr = strings.Fields(inCmd)
	} else if strings.ToUpper(cmdArr[0]) != define.HealthConfigTestCmdShell { // this is for podman side of things, won't contain the keywords
		if isArr && len(cmdArr) > 1 { // an array of consecutive commands
			cmdArr = append([]string{define.HealthConfigTestCmd}, cmdArr...)
		} else { // one singular command
			if len(cmdArr) == 1 {
				concat = cmdArr[0]
			} else {
				concat = strings.Join(cmdArr[0:], " ")
			}
			cmdArr = append([]string{define.HealthConfigTestCmdShell}, concat)
		}
	}

	if strings.ToUpper(cmdArr[0]) == define.HealthConfigTestNone { // if specified to remove healtcheck
		cmdArr = []string{define.HealthConfigTestNone}
	}

	// healthcheck is by default an array, so we simply pass the user input
	hc := manifest.Schema2HealthConfig{
		Test: cmdArr,
	}

	if interval == "disable" {
		interval = "0"
	}
	intervalDuration, err := time.ParseDuration(interval)
	if err != nil {
		return nil, fmt.Errorf("invalid healthcheck-interval: %w", err)
	}

	hc.Interval = intervalDuration

	if retries < 1 && !isStartup {
		return nil, errors.New("healthcheck-retries must be greater than 0")
	}
	hc.Retries = int(retries)
	timeoutDuration, err := time.ParseDuration(timeout)
	if err != nil {
		return nil, fmt.Errorf("invalid healthcheck-timeout: %w", err)
	}
	if timeoutDuration < time.Duration(1) {
		return nil, errors.New("healthcheck-timeout must be at least 1 second")
	}
	hc.Timeout = timeoutDuration

	startPeriodDuration, err := time.ParseDuration(startPeriod)
	if err != nil {
		return nil, fmt.Errorf("invalid healthcheck-start-period: %w", err)
	}
	if startPeriodDuration < time.Duration(0) {
		return nil, errors.New("healthcheck-start-period must be 0 seconds or greater")
	}
	hc.StartPeriod = startPeriodDuration
	return &hc, nil
}

func parseWeightDevices(weightDevs []string) (map[string]specs.LinuxWeightDevice, error) {
	wd := make(map[string]specs.LinuxWeightDevice)
	for _, dev := range weightDevs {
		key, val, hasVal := strings.Cut(dev, ":")
		if !hasVal {
			return nil, fmt.Errorf("bad format: %s", dev)
		}
		if !strings.HasPrefix(key, "/dev/") {
			return nil, fmt.Errorf("bad format for device path: %s", dev)
		}
		weight, err := strconv.ParseUint(val, 10, 0)
		if err != nil {
			return nil, fmt.Errorf("invalid weight for device: %s", dev)
		}
		if weight > 0 && (weight < 10 || weight > 1000) {
			return nil, fmt.Errorf("invalid weight for device: %s", dev)
		}
		w := uint16(weight)
		wd[key] = specs.LinuxWeightDevice{
			Weight:     &w,
			LeafWeight: nil,
		}
	}
	return wd, nil
}

func parseThrottleBPSDevices(bpsDevices []string) (map[string]specs.LinuxThrottleDevice, error) {
	td := make(map[string]specs.LinuxThrottleDevice)
	for _, dev := range bpsDevices {
		key, val, hasVal := strings.Cut(dev, ":")
		if !hasVal {
			return nil, fmt.Errorf("bad format: %s", dev)
		}
		if !strings.HasPrefix(key, "/dev/") {
			return nil, fmt.Errorf("bad format for device path: %s", dev)
		}
		rate, err := units.RAMInBytes(val)
		if err != nil {
			return nil, fmt.Errorf("invalid rate for device: %s. The correct format is <device-path>:<number>[<unit>]. Number must be a positive integer. Unit is optional and can be kb, mb, or gb", dev)
		}
		if rate < 0 {
			return nil, fmt.Errorf("invalid rate for device: %s. The correct format is <device-path>:<number>[<unit>]. Number must be a positive integer. Unit is optional and can be kb, mb, or gb", dev)
		}
		td[key] = specs.LinuxThrottleDevice{Rate: uint64(rate)}
	}
	return td, nil
}

func parseThrottleIOPsDevices(iopsDevices []string) (map[string]specs.LinuxThrottleDevice, error) {
	td := make(map[string]specs.LinuxThrottleDevice)
	for _, dev := range iopsDevices {
		key, val, hasVal := strings.Cut(dev, ":")
		if !hasVal {
			return nil, fmt.Errorf("bad format: %s", dev)
		}
		if !strings.HasPrefix(key, "/dev/") {
			return nil, fmt.Errorf("bad format for device path: %s", dev)
		}
		rate, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid rate for device: %s. The correct format is <device-path>:<number>. Number must be a positive integer", dev)
		}
		td[key] = specs.LinuxThrottleDevice{Rate: rate}
	}
	return td, nil
}

func parseSecrets(secrets []string) ([]specgen.Secret, map[string]string, error) {
	secretParseError := errors.New("parsing secret")
	var mount []specgen.Secret
	envs := make(map[string]string)
	for _, val := range secrets {
		// mount only tells if user has set an option that can only be used with mount secret type
		mountOnly := false
		source := ""
		secretType := ""
		target := ""
		var uid, gid uint32
		// default mode 444 octal = 292 decimal
		var mode uint32 = 292
		split := strings.Split(val, ",")

		// --secret mysecret
		if len(split) == 1 {
			mountSecret := specgen.Secret{
				Source: val,
				Target: target,
				UID:    uid,
				GID:    gid,
				Mode:   mode,
			}
			mount = append(mount, mountSecret)
			continue
		}
		// --secret mysecret,opt=opt
		if !strings.Contains(split[0], "=") {
			source = split[0]
			split = split[1:]
		}

		for _, val := range split {
			name, value, hasValue := strings.Cut(val, "=")
			if !hasValue {
				return nil, nil, fmt.Errorf("option %s must be in form option=value: %w", val, secretParseError)
			}
			switch name {
			case "source":
				source = value
			case "type":
				if secretType != "" {
					return nil, nil, fmt.Errorf("cannot set more than one secret type: %w", secretParseError)
				}
				if value != "mount" && value != "env" {
					return nil, nil, fmt.Errorf("type %s is invalid: %w", value, secretParseError)
				}
				secretType = value
			case "target":
				target = value
			case "mode":
				mountOnly = true
				mode64, err := strconv.ParseUint(value, 8, 32)
				if err != nil {
					return nil, nil, fmt.Errorf("mode %s invalid: %w", value, secretParseError)
				}
				mode = uint32(mode64)
			case "uid", "UID":
				mountOnly = true
				uid64, err := strconv.ParseUint(value, 10, 32)
				if err != nil {
					return nil, nil, fmt.Errorf("UID %s invalid: %w", value, secretParseError)
				}
				uid = uint32(uid64)
			case "gid", "GID":
				mountOnly = true
				gid64, err := strconv.ParseUint(value, 10, 32)
				if err != nil {
					return nil, nil, fmt.Errorf("GID %s invalid: %w", value, secretParseError)
				}
				gid = uint32(gid64)

			default:
				return nil, nil, fmt.Errorf("option %s invalid: %w", val, secretParseError)
			}
		}

		if secretType == "" {
			secretType = "mount"
		}
		if source == "" {
			return nil, nil, fmt.Errorf("no source found %s: %w", val, secretParseError)
		}
		if secretType == "mount" {
			mountSecret := specgen.Secret{
				Source: source,
				Target: target,
				UID:    uid,
				GID:    gid,
				Mode:   mode,
			}
			mount = append(mount, mountSecret)
		}
		if secretType == "env" {
			if mountOnly {
				return nil, nil, fmt.Errorf("UID, GID, Mode options cannot be set with secret type env: %w", secretParseError)
			}
			if target == "" {
				target = source
			}
			envs[target] = source
		}
	}
	return mount, envs, nil
}

var cgroupDeviceType = map[string]bool{
	"a": true, // all
	"b": true, // block device
	"c": true, // character device
}

var cgroupDeviceAccess = map[string]bool{
	"r": true, // read
	"w": true, // write
	"m": true, // mknod
}

// parseLinuxResourcesDeviceAccess parses the raw string passed with the --device-access-add flag
func parseLinuxResourcesDeviceAccess(device string) (specs.LinuxDeviceCgroup, error) {
	var devType, access string
	var major, minor *int64

	value := strings.Split(device, " ")
	if len(value) != 3 {
		return specs.LinuxDeviceCgroup{}, fmt.Errorf("invalid device cgroup rule requires type, major:Minor, and access rules: %q", device)
	}

	devType = value[0]
	if !cgroupDeviceType[devType] {
		return specs.LinuxDeviceCgroup{}, fmt.Errorf("invalid device type in device-access-add: %s", devType)
	}

	majorNumber, minorNumber, hasMinor := strings.Cut(value[1], ":")
	if majorNumber != "*" {
		i, err := strconv.ParseUint(majorNumber, 10, 64)
		if err != nil {
			return specs.LinuxDeviceCgroup{}, err
		}
		m := int64(i)
		major = &m
	}
	if hasMinor && minorNumber != "*" {
		i, err := strconv.ParseUint(minorNumber, 10, 64)
		if err != nil {
			return specs.LinuxDeviceCgroup{}, err
		}
		m := int64(i)
		minor = &m
	}
	access = value[2]
	for c := range strings.SplitSeq(access, "") {
		if !cgroupDeviceAccess[c] {
			return specs.LinuxDeviceCgroup{}, fmt.Errorf("invalid device access in device-access-add: %s", c)
		}
	}
	return specs.LinuxDeviceCgroup{
		Allow:  true,
		Type:   devType,
		Major:  major,
		Minor:  minor,
		Access: access,
	}, nil
}

func GetResources(s *specgen.SpecGenerator, c *entities.ContainerCreateOptions) (*specs.LinuxResources, error) {
	var err error
	if s.ResourceLimits.Memory == nil || (len(c.Memory) != 0 || len(c.MemoryReservation) != 0 || len(c.MemorySwap) != 0 || c.MemorySwappiness != 0) {
		s.ResourceLimits.Memory, err = getMemoryLimits(c)
		if err != nil {
			return nil, err
		}
	}
	if s.ResourceLimits.BlockIO == nil || (len(c.BlkIOWeight) != 0 || len(c.BlkIOWeightDevice) != 0 || len(c.DeviceReadBPs) != 0 || len(c.DeviceWriteBPs) != 0) {
		s.ResourceLimits.BlockIO, err = getIOLimits(s, c)
		if err != nil {
			return nil, err
		}
	}
	if c.PIDsLimit != nil {
		pids := specs.LinuxPids{
			Limit: *c.PIDsLimit,
		}

		s.ResourceLimits.Pids = &pids
	}

	if s.ResourceLimits.CPU == nil || (c.CPUPeriod != 0 || c.CPUQuota != 0 || c.CPURTPeriod != 0 || c.CPURTRuntime != 0 || c.CPUS != 0 || len(c.CPUSetCPUs) != 0 || len(c.CPUSetMems) != 0 || c.CPUShares != 0) {
		s.ResourceLimits.CPU = getCPULimits(c)
	}

	unifieds := make(map[string]string)
	for _, unified := range c.CgroupConf {
		key, val, hasVal := strings.Cut(unified, "=")
		if !hasVal {
			return nil, errors.New("--cgroup-conf must be formatted KEY=VALUE")
		}
		unifieds[key] = val
	}
	if len(unifieds) > 0 {
		s.ResourceLimits.Unified = unifieds
	}

	if s.ResourceLimits.CPU == nil && s.ResourceLimits.Pids == nil && s.ResourceLimits.BlockIO == nil && s.ResourceLimits.Memory == nil && s.ResourceLimits.Unified == nil {
		s.ResourceLimits = nil
	}
	return s.ResourceLimits, nil
}

func UpdateMajorAndMinorNumbers(resources *specs.LinuxResources, devicesLimits *define.UpdateContainerDevicesLimits) (*specs.LinuxResources, error) {
	spec := specgen.SpecGenerator{}
	spec.ResourceLimits = &specs.LinuxResources{}
	if resources != nil {
		spec.ResourceLimits = resources
	}

	spec.WeightDevice = devicesLimits.GetMapOfLinuxWeightDevice()
	spec.ThrottleReadBpsDevice = devicesLimits.GetMapOfDeviceReadBPs()
	spec.ThrottleWriteBpsDevice = devicesLimits.GetMapOfDeviceWriteBPs()
	spec.ThrottleReadIOPSDevice = devicesLimits.GetMapOfDeviceReadIOPs()
	spec.ThrottleWriteIOPSDevice = devicesLimits.GetMapOfDeviceWriteIOPs()

	err := specgen.WeightDevices(&spec)
	if err != nil {
		return nil, err
	}
	err = specgen.FinishThrottleDevices(&spec)
	if err != nil {
		return nil, err
	}
	return spec.ResourceLimits, nil
}
