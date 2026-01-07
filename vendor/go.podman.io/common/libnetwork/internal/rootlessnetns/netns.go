package rootlessnetns

type NetworkBackend int

const (
	Netavark NetworkBackend = iota
	CNI
)
