package commands

type AppleContainer struct {
	Configuration struct {
		ID        string `json:"id"`
		Name      string `json:"-"`
		Image     struct {
			Reference  string `json:"reference"`
			Descriptor struct {
				Digest    string `json:"digest"`
				Size      int64  `json:"size"`
				MediaType string `json:"mediaType"`
			} `json:"descriptor"`
		} `json:"image"`
		Labels    map[string]string `json:"labels"`
		Resources struct {
			CPUS          int   `json:"cpus"`
			MemoryInBytes int64 `json:"memoryInBytes"`
		} `json:"resources"`
		Platform struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		} `json:"platform"`
		InitProcess struct {
			Environment  []string `json:"environment"`
			Executable   string   `json:"executable"`
			Arguments    []string `json:"arguments"`
			WorkingDir   string   `json:"workingDirectory"`
			Terminal     bool     `json:"terminal"`
			User         struct {
				ID struct {
					UID int `json:"uid"`
					GID int `json:"gid"`
				} `json:"id"`
			} `json:"user"`
		} `json:"initProcess"`
		Mounts         []Mount `json:"mounts"`
		PublishedPorts []struct {
			HostIP        string `json:"hostIP"`
			HostPort      int    `json:"hostPort"`
			ContainerPort int    `json:"containerPort"`
			Protocol      string `json:"protocol"`
		} `json:"publishedPorts"`
		Networks []struct {
			Network string `json:"network"`
			Options struct {
				Hostname string `json:"hostname"`
			} `json:"options"`
		} `json:"networks"`
		RuntimeHandler string `json:"runtimeHandler"`
		Rosetta        bool   `json:"rosetta"`
		UseInit        bool   `json:"useInit"`
		Virtualization bool   `json:"virtualization"`
		ReadOnly       bool   `json:"readOnly"`
		SSH            bool   `json:"ssh"`
	} `json:"configuration"`
	Networks []struct {
		Network     string `json:"network"`
		IPv4Address string `json:"ipv4Address"`
		IPv6Address string `json:"ipv6Address"`
		IPv4Gateway string `json:"ipv4Gateway"`
		MACAddress  string `json:"macAddress"`
		Hostname    string `json:"hostname"`
	} `json:"networks"`
	Status      string  `json:"status"`
	StartedDate float64 `json:"startedDate"`
}

type Mount struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readonly"`
}

type AppleContainerStats struct {
	ID               string `json:"id"`
	CPUUsageUsec     int64  `json:"cpuUsageUsec"`
	MemoryUsageBytes int64  `json:"memoryUsageBytes"`
	MemoryLimitBytes int64  `json:"memoryLimitBytes"`
	NetworkRxBytes   int64  `json:"networkRxBytes"`
	NetworkTxBytes   int64  `json:"networkTxBytes"`
	BlockReadBytes   int64  `json:"blockReadBytes"`
	BlockWriteBytes  int64  `json:"blockWriteBytes"`
	NumProcesses     int    `json:"numProcesses"`
}

type AppleImage struct {
	Reference string `json:"reference"`
	Descriptor struct {
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
		MediaType string `json:"mediaType"`
	} `json:"descriptor"`
	FullSize string `json:"fullSize"`
}

type AppleVolume struct {
	Name       string `json:"name"`
	SizeInBytes int64 `json:"sizeInBytes"`
}

type AppleNetwork struct {
	ID     string `json:"id"`
	Config struct {
		Labels       map[string]string `json:"labels"`
		CreationDate float64           `json:"creationDate"`
		Mode         string            `json:"mode"`
		PluginInfo   struct {
			Variant string `json:"variant"`
			Plugin  string `json:"plugin"`
		} `json:"pluginInfo"`
	} `json:"config"`
	State string `json:"state"`
	Status struct {
		IPv4Subnet  string `json:"ipv4Subnet"`
		IPv6Subnet  string `json:"ipv6Subnet"`
		IPv4Gateway string `json:"ipv4Gateway"`
	} `json:"status"`
}

type SystemDF struct {
	Containers struct {
		Active      int   `json:"active"`
		Reclaimable int   `json:"reclaimable"`
		SizeInBytes int64 `json:"sizeInBytes"`
		Total       int   `json:"total"`
	} `json:"containers"`
	Images struct {
		Active      int   `json:"active"`
		Reclaimable int   `json:"reclaimable"`
		SizeInBytes int64 `json:"sizeInBytes"`
		Total       int   `json:"total"`
	} `json:"images"`
	Volumes struct {
		Active      int   `json:"active"`
		Reclaimable int   `json:"reclaimable"`
		SizeInBytes int64 `json:"sizeInBytes"`
		Total       int   `json:"total"`
	} `json:"volumes"`
}

type SystemStatus struct {
	Running bool
}

type CreateContainerOptions struct {
	Name       string
	Image      string
	Command    []string
	Env        []string
	WorkingDir string
	CPUS       int
	Memory     string
	Volumes    []string
	Networks   []string
	Ports      []string
	Labels     map[string]string
	Detach     bool
	Interactive bool
	TTY        bool
}

type ExecOptions struct {
	Command     []string
	Env         []string
	WorkingDir  string
	Interactive bool
	TTY         bool
	User        string
}
