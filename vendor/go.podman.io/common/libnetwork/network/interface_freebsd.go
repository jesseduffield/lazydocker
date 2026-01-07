package network

const (
	// cniConfigDir is the directory where cni configuration is found
	cniConfigDir = "/usr/local/etc/cni/net.d/"
	// netavarkConfigDir is the config directory for the rootful network files
	netavarkConfigDir = "/usr/local/etc/containers/networks"
	// netavarkRunDir is the run directory for the rootful temporary network files such as the ipam db
	netavarkRunDir = "/var/run/containers/networks"
)
