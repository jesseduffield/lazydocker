package gui

import (
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazycontainer/pkg/gui/panels"
)

func (gui *Gui) intoInterface() panels.IGui {
	return gui
}

func (gui *Gui) FilterString(view *gocui.View) string {
	if gui.State.Filter.active && gui.State.Filter.panel != nil && gui.State.Filter.panel.GetView() == view {
		return gui.State.Filter.needle
	}
	return ""
}
