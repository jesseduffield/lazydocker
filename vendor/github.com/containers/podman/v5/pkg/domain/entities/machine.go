package entities

import "github.com/containers/podman/v5/libpod/define"

type ListReporter struct {
	Name               string
	Default            bool
	Created            string
	Running            bool
	Starting           bool
	LastUp             string
	Stream             string
	VMType             string
	CPUs               uint64
	Memory             string
	Swap               string
	DiskSize           string
	Port               int
	RemoteUsername     string
	IdentityPath       string
	UserModeNetworking bool
}

// MachineInfo contains info on the machine host and version info
type MachineInfo struct {
	Host    *MachineHostInfo `json:"Host"`
	Version define.Version   `json:"Version"`
}

// MachineHostInfo contains info on the machine host
type MachineHostInfo struct {
	Arch           string `json:"Arch"`
	CurrentMachine string `json:"CurrentMachine"`
	// TODO(6.0): Change `DefaultName` to `ActiveMachineConnection` to fix address
	// confusion as shown in https://github.com/containers/podman/issues/23353.
	// The name `DefaultMachine` can cause confusion with the user in thinking that
	// they can set a default podman machine via system connections. However,
	// regardless of which system connection is default, the default podman machine
	// will always be podman-machine-default.
	DefaultMachine   string `json:"DefaultMachine"`
	EventsDir        string `json:"EventsDir"`
	MachineConfigDir string `json:"MachineConfigDir"`
	MachineImageDir  string `json:"MachineImageDir"`
	MachineState     string `json:"MachineState"`
	NumberOfMachines int    `json:"NumberOfMachines"`
	OS               string `json:"OS"`
	VMType           string `json:"VMType"`
}
