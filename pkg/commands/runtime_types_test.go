package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test ContainerSummary type fields
func TestContainerSummaryFields(t *testing.T) {
	now := time.Now()
	cs := ContainerSummary{
		ID:         "abc123",
		Names:      []string{"/my-container"},
		Image:      "alpine:latest",
		ImageID:    "sha256:abc123",
		Command:    `["sleep","infinity"]`,
		Created:    now.Unix(),
		State:      "running",
		Status:     "Up 5 minutes",
		Ports:      []PortMapping{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
		Labels:     map[string]string{"app": "test"},
		SizeRw:     1024,
		SizeRootFs: 4096,
		Pod:        "pod123",
		PodName:    "my-pod",
	}

	assert.Equal(t, "abc123", cs.ID)
	assert.Equal(t, []string{"/my-container"}, cs.Names)
	assert.Equal(t, "alpine:latest", cs.Image)
	assert.Equal(t, "running", cs.State)
	assert.Len(t, cs.Ports, 1)
	assert.Equal(t, uint16(80), cs.Ports[0].PrivatePort)
	assert.Equal(t, uint16(8080), cs.Ports[0].PublicPort)
	assert.Equal(t, "pod123", cs.Pod)
	assert.Equal(t, "my-pod", cs.PodName)
}

// Test ContainerDetails with all nested types
func TestContainerDetailsFields(t *testing.T) {
	now := time.Now()
	cd := ContainerDetails{
		ID:      "abc123",
		Name:    "/my-container",
		Created: now,
		Path:    "/bin/sh",
		Args:    []string{"-c", "sleep infinity"},
		State: &ContainerState{
			Status:     "running",
			Running:    true,
			Paused:     false,
			Restarting: false,
			OOMKilled:  false,
			Dead:       false,
			Pid:        12345,
			ExitCode:   0,
			StartedAt:  now,
			Health: &HealthState{
				Status:        "healthy",
				FailingStreak: 0,
				Log: []HealthLog{{
					Start:    now,
					End:      now.Add(time.Second),
					ExitCode: 0,
					Output:   "OK",
				}},
			},
		},
		Image:   "alpine:latest",
		ImageID: "sha256:abc123",
		Config: &ContainerConfig{
			Hostname:     "myhost",
			User:         "root",
			Tty:          true,
			Env:          []string{"PATH=/usr/bin"},
			Cmd:          []string{"sleep", "infinity"},
			WorkingDir:   "/app",
			Entrypoint:   []string{"/entrypoint.sh"},
			Labels:       map[string]string{"version": "1.0"},
			StopSignal:   "SIGTERM",
		},
		NetworkSettings: &NetworkSettings{
			Bridge: "bridge0",
			Ports: map[string][]PortBinding{
				"80/tcp": {{HostIP: "0.0.0.0", HostPort: "8080"}},
			},
			Networks: map[string]*EndpointSettings{
				"bridge": {
					NetworkID:  "net123",
					IPAddress:  "172.17.0.2",
					MacAddress: "02:42:ac:11:00:02",
				},
			},
		},
		Mounts: []Mount{{
			Type:        "volume",
			Name:        "mydata",
			Source:      "/var/lib/containers/storage/volumes/mydata/_data",
			Destination: "/data",
			RW:          true,
		}},
	}

	assert.Equal(t, "abc123", cd.ID)
	assert.Equal(t, "/my-container", cd.Name)
	assert.NotNil(t, cd.State)
	assert.True(t, cd.State.Running)
	assert.NotNil(t, cd.State.Health)
	assert.Equal(t, "healthy", cd.State.Health.Status)
	assert.NotNil(t, cd.Config)
	assert.Equal(t, "myhost", cd.Config.Hostname)
	assert.NotNil(t, cd.NetworkSettings)
	assert.Len(t, cd.NetworkSettings.Ports, 1)
	assert.Len(t, cd.Mounts, 1)
}

// Test ContainerStatsEntry with all stat types
func TestContainerStatsEntryFields(t *testing.T) {
	now := time.Now()
	stats := ContainerStatsEntry{
		Read:    now,
		PreRead: now.Add(-time.Second),
		CPUStats: CPUStats{
			CPUUsage: CPUUsage{
				TotalUsage:        1000000000,
				PercpuUsage:       []int64{500000000, 500000000},
				UsageInKernelmode: 100000000,
				UsageInUsermode:   900000000,
			},
			SystemCPUUsage: 10000000000,
			OnlineCpus:     2,
			ThrottlingData: ThrottlingData{
				Periods:          100,
				ThrottledPeriods: 5,
				ThrottledTime:    50000000,
			},
		},
		PreCPUStats: CPUStats{
			CPUUsage: CPUUsage{
				TotalUsage: 900000000,
			},
			SystemCPUUsage: 9000000000,
			OnlineCpus:     2,
		},
		MemoryStats: MemoryStats{
			Usage:    104857600, // 100 MiB
			MaxUsage: 209715200, // 200 MiB
			Limit:    536870912, // 512 MiB
			Stats: MemoryStatsDetails{
				ActiveAnon: 52428800,
				Cache:      52428800,
				Rss:        52428800,
			},
		},
		PidsStats: PidsStats{
			Current: 10,
			Limit:   1000,
		},
		Networks: map[string]NetworkStats{
			"eth0": {
				RxBytes:   1000000,
				RxPackets: 1000,
				TxBytes:   500000,
				TxPackets: 500,
			},
		},
		BlkioStats: BlkioStats{
			IoServiceBytesRecursive: []BlkioStatEntry{
				{Major: 8, Minor: 0, Op: "Read", Value: 1048576},
				{Major: 8, Minor: 0, Op: "Write", Value: 524288},
			},
		},
		Name: "my-container",
		ID:   "abc123",
	}

	assert.Equal(t, "my-container", stats.Name)
	assert.Equal(t, "abc123", stats.ID)
	assert.Equal(t, int64(1000000000), stats.CPUStats.CPUUsage.TotalUsage)
	assert.Equal(t, 2, stats.CPUStats.OnlineCpus)
	assert.Equal(t, int64(104857600), stats.MemoryStats.Usage)
	assert.Equal(t, int64(536870912), stats.MemoryStats.Limit)
	assert.Equal(t, 10, stats.PidsStats.Current)
	assert.Len(t, stats.Networks, 1)
	assert.Equal(t, int64(1000000), stats.Networks["eth0"].RxBytes)
	assert.Len(t, stats.BlkioStats.IoServiceBytesRecursive, 2)
}

// Test ImageSummary fields
func TestImageSummaryFields(t *testing.T) {
	now := time.Now()
	img := ImageSummary{
		ID:          "sha256:abc123",
		ParentID:    "sha256:parent123",
		RepoTags:    []string{"alpine:latest", "alpine:3.19"},
		RepoDigests: []string{"alpine@sha256:digest1"},
		Created:     now.Unix(),
		Size:        5242880, // 5 MiB
		SharedSize:  1048576, // 1 MiB
		VirtualSize: 6291456, // 6 MiB
		Labels:      map[string]string{"maintainer": "test@example.com"},
		Containers:  3,
	}

	assert.Equal(t, "sha256:abc123", img.ID)
	assert.Len(t, img.RepoTags, 2)
	assert.Equal(t, int64(5242880), img.Size)
	assert.Equal(t, int64(3), img.Containers)
}

// Test ImageDetails fields
func TestImageDetailsFields(t *testing.T) {
	now := time.Now()
	img := ImageDetails{
		ID:            "sha256:abc123",
		RepoTags:      []string{"alpine:latest"},
		RepoDigests:   []string{"alpine@sha256:digest1"},
		Parent:        "sha256:parent123",
		Comment:       "Test image",
		Created:       now,
		DockerVersion: "20.10.0",
		Author:        "test@example.com",
		Config: &ContainerConfig{
			Cmd:        []string{"/bin/sh"},
			Env:        []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			WorkingDir: "/",
		},
		Architecture: "amd64",
		Os:           "linux",
		Size:         5242880,
		VirtualSize:  5242880,
		RootFS: RootFS{
			Type:   "layers",
			Layers: []string{"sha256:layer1", "sha256:layer2"},
		},
		Metadata: ImageMetadata{
			LastTagTime: now,
		},
	}

	assert.Equal(t, "sha256:abc123", img.ID)
	assert.Equal(t, "amd64", img.Architecture)
	assert.Equal(t, "linux", img.Os)
	assert.Len(t, img.RootFS.Layers, 2)
	assert.NotNil(t, img.Config)
}

// Test ImageHistoryEntry fields
func TestImageHistoryEntryFields(t *testing.T) {
	now := time.Now()
	entry := ImageHistoryEntry{
		ID:        "sha256:abc123",
		Created:   now.Unix(),
		CreatedBy: "/bin/sh -c #(nop) CMD [\"/bin/sh\"]",
		Tags:      []string{"alpine:latest"},
		Size:      0,
		Comment:   "",
	}

	assert.Equal(t, "sha256:abc123", entry.ID)
	assert.Contains(t, entry.CreatedBy, "CMD")
}

// Test VolumeSummary fields
func TestVolumeSummaryFields(t *testing.T) {
	now := time.Now()
	vol := VolumeSummary{
		Name:       "mydata",
		Driver:     "local",
		Mountpoint: "/var/lib/containers/storage/volumes/mydata/_data",
		CreatedAt:  now,
		Status:     map[string]interface{}{"state": "available"},
		Labels:     map[string]string{"app": "test"},
		Scope:      "local",
		Options:    map[string]string{"type": "tmpfs"},
		UsageData: &VolumeUsageData{
			Size:     1048576,
			RefCount: 2,
		},
	}

	assert.Equal(t, "mydata", vol.Name)
	assert.Equal(t, "local", vol.Driver)
	assert.NotNil(t, vol.UsageData)
	assert.Equal(t, int64(1048576), vol.UsageData.Size)
	assert.Equal(t, int64(2), vol.UsageData.RefCount)
}

// Test NetworkSummary fields
func TestNetworkSummaryFields(t *testing.T) {
	now := time.Now()
	nw := NetworkSummary{
		Name:       "my-network",
		ID:         "net123",
		Created:    now,
		Scope:      "local",
		Driver:     "bridge",
		EnableIPv6: false,
		IPAM: IPAM{
			Driver: "default",
			Config: []IPAMConfig{{
				Subnet:  "172.18.0.0/16",
				Gateway: "172.18.0.1",
			}},
		},
		Internal:   false,
		Attachable: true,
		Ingress:    false,
		Containers: map[string]EndpointResource{
			"container1": {
				Name:        "my-container",
				EndpointID:  "ep123",
				MacAddress:  "02:42:ac:12:00:02",
				IPv4Address: "172.18.0.2/16",
			},
		},
		Options: map[string]string{"com.docker.network.bridge.name": "br-net123"},
		Labels:  map[string]string{"env": "test"},
	}

	assert.Equal(t, "my-network", nw.Name)
	assert.Equal(t, "bridge", nw.Driver)
	assert.Len(t, nw.IPAM.Config, 1)
	assert.Equal(t, "172.18.0.0/16", nw.IPAM.Config[0].Subnet)
	assert.Len(t, nw.Containers, 1)
	assert.Equal(t, "my-container", nw.Containers["container1"].Name)
}

// Test Event fields
func TestEventFields(t *testing.T) {
	now := time.Now()
	event := Event{
		Type:   "container",
		Action: "start",
		Actor: EventActor{
			ID: "abc123",
			Attributes: map[string]string{
				"name":  "my-container",
				"image": "alpine:latest",
			},
		},
		Time: now.Unix(),
	}

	assert.Equal(t, "container", event.Type)
	assert.Equal(t, "start", event.Action)
	assert.Equal(t, "abc123", event.Actor.ID)
	assert.Equal(t, "my-container", event.Actor.Attributes["name"])
}

// Test PortMapping fields
func TestPortMappingFields(t *testing.T) {
	pm := PortMapping{
		IP:          "0.0.0.0",
		PrivatePort: 80,
		PublicPort:  8080,
		Type:        "tcp",
	}

	assert.Equal(t, "0.0.0.0", pm.IP)
	assert.Equal(t, uint16(80), pm.PrivatePort)
	assert.Equal(t, uint16(8080), pm.PublicPort)
	assert.Equal(t, "tcp", pm.Type)
}

// Test PortBinding fields
func TestPortBindingFields(t *testing.T) {
	pb := PortBinding{
		HostIP:   "0.0.0.0",
		HostPort: "8080",
	}

	assert.Equal(t, "0.0.0.0", pb.HostIP)
	assert.Equal(t, "8080", pb.HostPort)
}

// Test EndpointSettings fields
func TestEndpointSettingsFields(t *testing.T) {
	es := EndpointSettings{
		IPAMConfig: &EndpointIPAMConfig{
			IPv4Address:  "172.17.0.5",
			IPv6Address:  "2001:db8::5",
			LinkLocalIPs: []string{"169.254.0.1"},
		},
		Links:               []string{"container1:alias1"},
		Aliases:             []string{"web", "frontend"},
		NetworkID:           "net123",
		EndpointID:          "ep123",
		Gateway:             "172.17.0.1",
		IPAddress:           "172.17.0.5",
		IPPrefixLen:         16,
		IPv6Gateway:         "2001:db8::1",
		GlobalIPv6Address:   "2001:db8::5",
		GlobalIPv6PrefixLen: 64,
		MacAddress:          "02:42:ac:11:00:05",
		DriverOpts:          map[string]string{"opt1": "value1"},
	}

	assert.NotNil(t, es.IPAMConfig)
	assert.Equal(t, "172.17.0.5", es.IPAddress)
	assert.Equal(t, 16, es.IPPrefixLen)
	assert.Equal(t, "02:42:ac:11:00:05", es.MacAddress)
}

// Test Mount fields
func TestMountFields(t *testing.T) {
	m := Mount{
		Type:        "volume",
		Name:        "mydata",
		Source:      "/var/lib/containers/storage/volumes/mydata/_data",
		Destination: "/data",
		Driver:      "local",
		Mode:        "rw",
		RW:          true,
		Propagation: "rprivate",
	}

	assert.Equal(t, "volume", m.Type)
	assert.Equal(t, "mydata", m.Name)
	assert.True(t, m.RW)
}

// Test empty/nil handling
func TestEmptyContainerSummary(t *testing.T) {
	cs := ContainerSummary{}
	assert.Empty(t, cs.ID)
	assert.Nil(t, cs.Names)
	assert.Nil(t, cs.Ports)
	assert.Nil(t, cs.Labels)
}

func TestNilContainerState(t *testing.T) {
	cd := ContainerDetails{
		ID:    "abc123",
		State: nil,
	}
	assert.Nil(t, cd.State)
}

func TestNilHealthState(t *testing.T) {
	state := ContainerState{
		Status: "running",
		Health: nil,
	}
	assert.Nil(t, state.Health)
}

func TestEmptyNetworkStats(t *testing.T) {
	stats := ContainerStatsEntry{
		Networks: make(map[string]NetworkStats),
	}
	assert.Empty(t, stats.Networks)
}

func TestEmptyBlkioStats(t *testing.T) {
	stats := ContainerStatsEntry{
		BlkioStats: BlkioStats{
			IoServiceBytesRecursive: nil,
		},
	}
	assert.Nil(t, stats.BlkioStats.IoServiceBytesRecursive)
}

func TestNilVolumeUsageData(t *testing.T) {
	vol := VolumeSummary{
		Name:      "mydata",
		UsageData: nil,
	}
	assert.Nil(t, vol.UsageData)
}

func TestEmptyIPAMConfig(t *testing.T) {
	nw := NetworkSummary{
		IPAM: IPAM{
			Config: []IPAMConfig{},
		},
	}
	assert.Empty(t, nw.IPAM.Config)
}

// Test edge cases for numeric types
func TestZeroValues(t *testing.T) {
	stats := ContainerStatsEntry{
		CPUStats: CPUStats{
			CPUUsage: CPUUsage{
				TotalUsage: 0,
			},
			SystemCPUUsage: 0,
			OnlineCpus:     0,
		},
		MemoryStats: MemoryStats{
			Usage: 0,
			Limit: 0,
		},
	}
	assert.Equal(t, int64(0), stats.CPUStats.CPUUsage.TotalUsage)
	assert.Equal(t, int64(0), stats.MemoryStats.Usage)
}

// Test maximum values for int64 fields
func TestMaxInt64Values(t *testing.T) {
	const maxInt64 = int64(9223372036854775807)
	stats := ContainerStatsEntry{
		MemoryStats: MemoryStats{
			Limit: maxInt64,
		},
	}
	assert.Equal(t, maxInt64, stats.MemoryStats.Limit)
}

// Test ContainerConfig ExposedPorts and Volumes map types
func TestContainerConfigMaps(t *testing.T) {
	cfg := ContainerConfig{
		ExposedPorts: map[string]struct{}{
			"80/tcp":  {},
			"443/tcp": {},
		},
		Volumes: map[string]struct{}{
			"/data":  {},
			"/cache": {},
		},
	}

	_, exists := cfg.ExposedPorts["80/tcp"]
	assert.True(t, exists)
	_, exists = cfg.Volumes["/data"]
	assert.True(t, exists)
}

// Test TopResponse type
func TestTopResponseFields(t *testing.T) {
	top := TopResponse{
		Titles:    []string{"UID", "PID", "PPID", "C", "STIME", "TTY", "TIME", "CMD"},
		Processes: [][]string{{"root", "1", "0", "0", "10:00", "?", "00:00:01", "/bin/sh"}},
	}

	assert.Len(t, top.Titles, 8)
	assert.Equal(t, "PID", top.Titles[1])
	assert.Len(t, top.Processes, 1)
	assert.Equal(t, "1", top.Processes[0][1])
}

// Test BlkioStatEntry fields
func TestBlkioStatEntryFields(t *testing.T) {
	entry := BlkioStatEntry{
		Major: 8,
		Minor: 0,
		Op:    "Read",
		Value: 1048576,
	}

	assert.Equal(t, int64(8), entry.Major)
	assert.Equal(t, int64(0), entry.Minor)
	assert.Equal(t, "Read", entry.Op)
	assert.Equal(t, int64(1048576), entry.Value)
}

// Test MemoryStatsDetails all fields
func TestMemoryStatsDetailsFields(t *testing.T) {
	details := MemoryStatsDetails{
		ActiveAnon:              1024,
		ActiveFile:              2048,
		Cache:                   4096,
		Dirty:                   512,
		HierarchicalMemoryLimit: 536870912,
		HierarchicalMemswLimit:  1073741824,
		InactiveAnon:            256,
		InactiveFile:            1024,
		MappedFile:              2048,
		Pgfault:                 1000,
		Pgmajfault:              10,
		Pgpgin:                  500,
		Pgpgout:                 400,
		Rss:                     52428800,
		RssHuge:                 0,
		TotalActiveAnon:         1024,
		TotalActiveFile:         2048,
		TotalCache:              4096,
		TotalDirty:              512,
		TotalInactiveAnon:       256,
		TotalInactiveFile:       1024,
		TotalMappedFile:         2048,
		TotalPgfault:            1000,
		TotalPgmajfault:         10,
		TotalPgpgin:             500,
		TotalPgpgout:            400,
		TotalRss:                52428800,
		TotalRssHuge:            0,
		TotalUnevictable:        0,
		TotalWriteback:          0,
		Unevictable:             0,
		Writeback:               0,
	}

	assert.Equal(t, int64(4096), details.Cache)
	assert.Equal(t, int64(52428800), details.Rss)
	assert.Equal(t, int64(1000), details.Pgfault)
}

// Test ThrottlingData fields
func TestThrottlingDataFields(t *testing.T) {
	data := ThrottlingData{
		Periods:          100,
		ThrottledPeriods: 5,
		ThrottledTime:    50000000,
	}

	assert.Equal(t, 100, data.Periods)
	assert.Equal(t, 5, data.ThrottledPeriods)
	assert.Equal(t, int64(50000000), data.ThrottledTime)
}

// Test RootFS and ImageMetadata
func TestRootFSAndMetadata(t *testing.T) {
	now := time.Now()
	rootFS := RootFS{
		Type:   "layers",
		Layers: []string{"sha256:abc", "sha256:def", "sha256:ghi"},
	}
	metadata := ImageMetadata{
		LastTagTime: now,
	}

	assert.Equal(t, "layers", rootFS.Type)
	assert.Len(t, rootFS.Layers, 3)
	assert.Equal(t, now, metadata.LastTagTime)
}
