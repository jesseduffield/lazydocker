package gui

import (
	"math"

	"github.com/jesseduffield/gocui"
)

func (gui *Gui) scrollUpMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.getMainView()
	mainView.Autoscroll = false
	ox, oy := mainView.Origin()
	newOy := int(math.Max(0, float64(oy-gui.Config.UserConfig.Gui.ScrollHeight)))
	return mainView.SetOrigin(ox, newOy)
}

func (gui *Gui) scrollDownMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.getMainView()
	mainView.Autoscroll = false
	ox, oy := mainView.Origin()

	reservedLines := 0
	if !gui.Config.UserConfig.Gui.ScrollPastBottom {
		_, sizeY := mainView.Size()
		reservedLines = sizeY
	}

	totalLines := mainView.ViewLinesHeight()
	if oy+reservedLines >= totalLines {
		return nil
	}

	return mainView.SetOrigin(ox, oy+gui.Config.UserConfig.Gui.ScrollHeight)
}

func (gui *Gui) scrollLeftMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.getMainView()
	ox, oy := mainView.Origin()
	newOx := int(math.Max(0, float64(ox-gui.Config.UserConfig.Gui.ScrollHeight)))

	return mainView.SetOrigin(newOx, oy)
}

func (gui *Gui) scrollRightMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.getMainView()
	ox, oy := mainView.Origin()

	content := mainView.ViewBufferLines()
	var largestNumberOfCharacters int
	for _, txt := range content {
		if len(txt) > largestNumberOfCharacters {
			largestNumberOfCharacters = len(txt)
		}
	}

	sizeX, _ := mainView.Size()
	if ox+sizeX >= largestNumberOfCharacters {
		return nil
	}

	return mainView.SetOrigin(ox+gui.Config.UserConfig.Gui.ScrollHeight, oy)
}

func (gui *Gui) autoScrollMain(g *gocui.Gui, v *gocui.View) error {
	gui.getMainView().Autoscroll = true
	return nil
}

func (gui *Gui) jumpToTopMain(g *gocui.Gui, v *gocui.View) error {
	gui.getMainView().Autoscroll = false
	_ = gui.getMainView().SetOrigin(0, 0)
	_ = gui.getMainView().SetCursor(0, 0)
	return nil
}

func (gui *Gui) onMainTabClick(tabIndex int) error {
	gui.Log.Warn(tabIndex)

	viewName := gui.currentViewName()

	mainView := gui.getMainView()
	if viewName == "main" && mainView.ParentView != nil {
		viewName = mainView.ParentView.Name()
	}

	currentSidePanel, ok := gui.currentSidePanel()

	if !ok {
		return nil
	}

	currentSidePanel.SetContextIndex(tabIndex)
	return currentSidePanel.HandleSelect()
}

func (gui *Gui) handleEnterMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.getMainView()
	mainView.ParentView = v

	return gui.switchFocus(mainView)
}

func (gui *Gui) handleExitMain(g *gocui.Gui, v *gocui.View) error {
	v.ParentView = nil
	return gui.returnFocus()
}

func (gui *Gui) handleMainClick(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	currentView := gui.g.CurrentView()

	if currentView != nil && currentView.Name() == "main" {
		currentView = nil
	} else {
		v.ParentView = currentView
	}

	return gui.switchFocus(v)
}
