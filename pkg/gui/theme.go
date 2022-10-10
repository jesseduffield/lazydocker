package gui

import (
	"github.com/jesseduffield/gocui"
)

// GetOptionsPanelTextColor gets the color of the options panel text
func (gui *Gui) GetOptionsPanelTextColor() gocui.Attribute {
	return GetGocuiStyle(gui.Config.UserConfig.Gui.Theme.OptionsTextColor)
}

// SetColorScheme sets the color scheme for the app based on the user config
func (gui *Gui) SetColorScheme() error {
	gui.g.FgColor = GetGocuiStyle(gui.Config.UserConfig.Gui.Theme.InactiveBorderColor)
	gui.g.SelFgColor = GetGocuiStyle(gui.Config.UserConfig.Gui.Theme.ActiveBorderColor)
	gui.g.FrameColor = gui.g.FgColor
	gui.g.SelFrameColor = gui.g.SelFgColor
	return nil
}
