package types

import "github.com/jesseduffield/lazydocker/pkg/config"

// KeybindingsOpts contains the options for creating keybindings
type KeybindingsOpts struct {
	// GetKey is the function to parse string keys to gocui key types
	GetKey func(string) (interface{}, error)

	// Config is the keybinding configuration
	Config config.KeybindingConfig
}
