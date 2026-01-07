//go:build !remote

package libpod

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/containers/buildah"
	"github.com/containers/buildah/pkg/parse"
	"github.com/containers/buildah/pkg/util"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/linkmode"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/version"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/system"
)

// Info returns the store and host information
func (r *Runtime) info() (*define.Info, error) {
	info := define.Info{}
	versionInfo, err := define.GetVersion()
	if err != nil {
		return nil, fmt.Errorf("getting version info: %w", err)
	}
	info.Version = versionInfo
	// get host information
	hostInfo, err := r.hostInfo()
	if err != nil {
		return nil, fmt.Errorf("getting host info: %w", err)
	}
	info.Host = hostInfo

	// get store information
	storeInfo, err := r.storeInfo()
	if err != nil {
		return nil, fmt.Errorf("getting store info: %w", err)
	}
	info.Store = storeInfo
	registries := make(map[string]any)

	sys := r.SystemContext()
	data, err := sysregistriesv2.GetRegistries(sys)
	if err != nil {
		return nil, fmt.Errorf("getting registries: %w", err)
	}
	for _, reg := range data {
		registries[reg.Prefix] = reg
	}
	regs, err := sysregistriesv2.UnqualifiedSearchRegistries(sys)
	if err != nil {
		return nil, fmt.Errorf("getting registries: %w", err)
	}
	if len(regs) > 0 {
		registries["search"] = regs
	}
	volumePlugins := make([]string, 0, len(r.config.Engine.VolumePlugins)+1)
	// the local driver always exists
	volumePlugins = append(volumePlugins, "local")
	for plugin := range r.config.Engine.VolumePlugins {
		volumePlugins = append(volumePlugins, plugin)
	}
	info.Plugins.Volume = volumePlugins
	info.Plugins.Network = r.network.Drivers()
	info.Plugins.Log = logDrivers

	info.Registries = registries
	return &info, nil
}

// top-level "host" info
func (r *Runtime) hostInfo() (*define.HostInfo, error) {
	// let's say OS, arch, number of cpus, amount of memory, maybe os distribution/version, hostname, kernel version, uptime
	mi, err := system.ReadMemInfo()
	if err != nil {
		return nil, fmt.Errorf("reading memory info: %w", err)
	}

	hostDistributionInfo := r.GetHostDistributionInfo()

	kv, err := util.ReadKernelVersion()
	if err != nil {
		return nil, fmt.Errorf("reading kernel version: %w", err)
	}

	host, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("getting hostname: %w", err)
	}

	cpuUtil, err := getCPUUtilization()
	if err != nil {
		return nil, err
	}

	locksFree, err := r.lockManager.AvailableLocks()
	if err != nil {
		return nil, fmt.Errorf("getting free locks: %w", err)
	}

	info := define.HostInfo{
		Arch:               runtime.GOARCH,
		BuildahVersion:     buildah.Version,
		DatabaseBackend:    r.config.Engine.DBBackend,
		Linkmode:           linkmode.Linkmode(),
		CPUs:               runtime.NumCPU(),
		CPUUtilization:     cpuUtil,
		Distribution:       hostDistributionInfo,
		LogDriver:          r.config.Containers.LogDriver,
		EventLogger:        r.eventer.String(),
		FreeLocks:          locksFree,
		Hostname:           host,
		Kernel:             kv,
		MemFree:            mi.MemFree,
		MemTotal:           mi.MemTotal,
		NetworkBackend:     r.config.Network.NetworkBackend,
		NetworkBackendInfo: r.network.NetworkInfo(),
		OS:                 runtime.GOOS,
		RootlessNetworkCmd: r.config.Network.DefaultRootlessNetworkCmd,
		SwapFree:           mi.SwapFree,
		SwapTotal:          mi.SwapTotal,
	}
	platform := parse.DefaultPlatform()
	pArr := strings.Split(platform, "/")
	if len(pArr) == 3 {
		info.Variant = pArr[2]
	}
	if err := r.setPlatformHostInfo(&info); err != nil {
		return nil, err
	}

	conmonInfo, ociruntimeInfo, err := r.defaultOCIRuntime.RuntimeInfo()
	if err != nil {
		logrus.Errorf("Getting info on OCI runtime %s: %v", r.defaultOCIRuntime.Name(), err)
	} else {
		info.Conmon = conmonInfo
		info.OCIRuntime = ociruntimeInfo
	}

	duration, err := util.ReadUptime()
	if err != nil {
		return nil, fmt.Errorf("reading up time: %w", err)
	}

	uptime := struct {
		hours   float64
		minutes float64
		seconds float64
	}{
		hours:   duration.Truncate(time.Hour).Hours(),
		minutes: duration.Truncate(time.Minute).Minutes(),
		seconds: duration.Truncate(time.Second).Seconds(),
	}

	// Could not find a humanize-formatter for time.Duration
	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("%.0fh %.0fm %.2fs",
		uptime.hours,
		math.Mod(uptime.minutes, 60),
		math.Mod(uptime.seconds, 60),
	))
	if int64(uptime.hours) > 0 {
		buffer.WriteString(fmt.Sprintf(" (Approximately %.2f days)", uptime.hours/24))
	}
	info.Uptime = buffer.String()

	return &info, nil
}

func (r *Runtime) getContainerStoreInfo() (define.ContainerStore, error) {
	var paused, running, stopped int
	cs := define.ContainerStore{}
	cons, err := r.GetAllContainers()
	if err != nil {
		return cs, err
	}
	cs.Number = len(cons)
	for _, con := range cons {
		state, err := con.State()
		if err != nil {
			if errors.Is(err, define.ErrNoSuchCtr) {
				// container was probably removed
				cs.Number--
				continue
			}
			return cs, err
		}
		switch state {
		case define.ContainerStateRunning:
			running++
		case define.ContainerStatePaused:
			paused++
		default:
			stopped++
		}
	}
	cs.Paused = paused
	cs.Stopped = stopped
	cs.Running = running
	return cs, nil
}

// top-level "store" info
func (r *Runtime) storeInfo() (*define.StoreInfo, error) {
	// let's say storage driver in use, number of images, number of containers
	configFile, err := storage.DefaultConfigFile()
	if err != nil {
		return nil, err
	}
	images, err := r.store.Images()
	if err != nil {
		return nil, fmt.Errorf("getting number of images: %w", err)
	}
	conInfo, err := r.getContainerStoreInfo()
	if err != nil {
		return nil, err
	}
	imageInfo := define.ImageStore{Number: len(images)}

	var grStats syscall.Statfs_t
	if err := syscall.Statfs(r.store.GraphRoot(), &grStats); err != nil {
		return nil, fmt.Errorf("unable to collect graph root usage for %q: %w", r.store.GraphRoot(), err)
	}
	bsize := uint64(grStats.Bsize) //nolint:unconvert,nolintlint // Bsize is not always uint64 on Linux.
	allocated := bsize * grStats.Blocks
	info := define.StoreInfo{
		ImageStore:         imageInfo,
		ImageCopyTmpDir:    os.Getenv("TMPDIR"),
		ContainerStore:     conInfo,
		GraphRoot:          r.store.GraphRoot(),
		GraphRootAllocated: allocated,
		GraphRootUsed:      allocated - (bsize * grStats.Bfree),
		RunRoot:            r.store.RunRoot(),
		GraphDriverName:    r.store.GraphDriverName(),
		GraphOptions:       nil,
		VolumePath:         r.config.Engine.VolumePath,
		ConfigFile:         configFile,
		TransientStore:     r.store.TransientStore(),
	}

	graphOptions := map[string]any{}
	for _, o := range r.store.GraphOptions() {
		split := strings.SplitN(o, "=", 2)
		switch {
		case strings.HasSuffix(split[0], "mount_program"):
			ver, err := version.Program(split[1])
			if err != nil {
				logrus.Warnf("Failed to retrieve program version for %s: %v", split[1], err)
			}
			program := map[string]any{}
			program["Executable"] = split[1]
			program["Version"] = ver
			program["Package"] = version.Package(split[1])
			graphOptions[split[0]] = program
		case strings.HasSuffix(split[0], "imagestore"):
			key := strings.ReplaceAll(split[0], "imagestore", "additionalImageStores")
			if graphOptions[key] == nil {
				graphOptions[key] = []string{split[1]}
			} else {
				graphOptions[key] = append(graphOptions[key].([]string), split[1])
			}
			// Fallthrough to include the `imagestore` key to avoid breaking
			// Podman v5 API. Should be removed in Podman v6.0.0.
			fallthrough
		default:
			graphOptions[split[0]] = split[1]
		}
	}
	info.GraphOptions = graphOptions

	statusPairs, err := r.store.Status()
	if err != nil {
		return nil, err
	}
	status := map[string]string{}
	for _, pair := range statusPairs {
		status[pair[0]] = pair[1]
	}
	info.GraphStatus = status
	return &info, nil
}

// GetHostDistributionInfo returns a map containing the host's distribution and version
func (r *Runtime) GetHostDistributionInfo() define.DistributionInfo {
	// Populate values in case we cannot find the values
	// or the file
	dist := define.DistributionInfo{
		Distribution: "unknown",
		Version:      "unknown",
	}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return dist
	}
	defer f.Close()

	l := bufio.NewScanner(f)
	for l.Scan() {
		if after, ok := strings.CutPrefix(l.Text(), "ID="); ok {
			dist.Distribution = strings.Trim(after, "\"")
		}
		if after, ok := strings.CutPrefix(l.Text(), "VARIANT_ID="); ok {
			dist.Variant = strings.Trim(after, "\"")
		}
		if after, ok := strings.CutPrefix(l.Text(), "VERSION_ID="); ok {
			dist.Version = strings.Trim(after, "\"")
		}
		if after, ok := strings.CutPrefix(l.Text(), "VERSION_CODENAME="); ok {
			dist.Codename = strings.Trim(after, "\"")
		}
	}
	return dist
}
