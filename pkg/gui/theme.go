package gui

import (
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// GetColor bitwise OR's a list of attributes obtained via the given keys
func (gui *Gui) GetColor(keys []string) gocui.Attribute {
	var attribute gocui.Attribute
	for _, key := range keys {
		attribute |= utils.GetGocuiAttribute(key)
	}
	return attribute
}

// GetOptionsPanelTextColor gets the color of the options panel text
func (gui *Gui) GetOptionsPanelTextColor() (gocui.Attribute, error) {
	return gui.GetColor(gui.Config.UserConfig.Gui.Theme.OptionsTextColor), nil
}

// SetColorScheme sets the color scheme for the app based on the user config
func (gui *Gui) SetColorScheme() error {
	gui.g.FgColor = gui.GetColor(gui.Config.UserConfig.Gui.Theme.InactiveBorderColor)
	gui.g.SelFgColor = gui.GetColor(gui.Config.UserConfig.Gui.Theme.ActiveBorderColor)
	return nil
}
