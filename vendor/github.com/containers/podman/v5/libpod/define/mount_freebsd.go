//go:build freebsd

package define

const (
	// TypeBind is the type for mounting host dir
	TypeBind = "nullfs"
)

var (
	// Mount potions for bind
	BindOptions = []string{}
)
