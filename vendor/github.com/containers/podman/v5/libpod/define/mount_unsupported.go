//go:build !linux && !freebsd

package define

const (
	// TypeBind is the type for mounting host dir
	TypeBind = "bind"
)
