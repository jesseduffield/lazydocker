package gui

import (
	"math"

	"github.com/jesseduffield/gocui"
)

func (gui *Gui) scrollUpMain(g *gocui.Gui, v *gocui.View) error {
	mainView, _ := g.View("main")
	mainView.Autoscroll = false
	ox, oy := mainView.Origin()
	newOy := int(math.Max(0, float64(oy-gui.Config.UserConfig.Gui.ScrollHeight)))
	return mainView.SetOrigin(ox, newOy)
}

func (gui *Gui) scrollDownMain(g *gocui.Gui, v *gocui.View) error {
	mainView, _ := g.View("main")
	ox, oy := mainView.Origin()
	y := oy
	if !gui.Config.UserConfig.Gui.ScrollPastBottom {
		_, sy := mainView.Size()
		y += sy
	}
	// for some reason we can't work out whether we've hit the bottomq
	// there is a large discrepancy in the origin's y value and the length of BufferLines
	return mainView.SetOrigin(ox, oy+gui.Config.UserConfig.Gui.ScrollHeight)
}

func (gui *Gui) autoScrollMain(g *gocui.Gui, v *gocui.View) error {
	gui.getMainView().Autoscroll = true
	return nil
}
