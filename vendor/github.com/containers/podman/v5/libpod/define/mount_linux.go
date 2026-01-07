//go:build linux

package define

const (
	// TypeBind is the type for mounting host dir
	TypeBind = "bind"
)

var (
	// Mount potions for bind
	BindOptions = []string{TypeBind}
)
