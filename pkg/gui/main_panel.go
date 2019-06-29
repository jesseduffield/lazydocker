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

func (gui *Gui) onMainTabClick(tabIndex int) error {
	gui.Log.Warn(tabIndex)

	viewName := gui.currentViewName()

	switch viewName {
	case "services":
		gui.State.Panels.Services.ContextIndex = tabIndex
		return gui.handleServiceSelect(gui.g, gui.getServicesView())
	case "containers":
		gui.State.Panels.Containers.ContextIndex = tabIndex
		return gui.handleContainerSelect(gui.g, gui.getContainersView())
	case "images":
		gui.State.Panels.Images.ContextIndex = tabIndex
		return gui.handleImageSelect(gui.g, gui.getImagesView())
	case "volumes":
		gui.State.Panels.Volumes.ContextIndex = tabIndex
		return gui.handleVolumeSelect(gui.g, gui.getVolumesView())
	case "status":
		gui.State.Panels.Status.ContextIndex = tabIndex
		return gui.handleStatusSelect(gui.g, gui.getStatusView())
	}

	return nil
}
