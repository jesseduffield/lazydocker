//go:build darwin || windows || netbsd

package define

const (
	// TypeBind is the type for mounting host dir
	TypeBind = "bind"

	// TempDir is the default for storing temporary files
	TempDir = "/var/tmp"
)

// Mount potions for bind
var BindOptions = []string{""}
