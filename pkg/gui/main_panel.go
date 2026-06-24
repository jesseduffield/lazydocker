package gui

import (
	"math"
	"time"

	"github.com/jesseduffield/gocui"
)

// gPressTimeout is the window within which two consecutive 'g' presses are
// treated as the vim-style "gg" command to jump to the top of the main panel.
const gPressTimeout = 250 * time.Millisecond

// isDoublePress reports whether `now` follows `previous` closely enough to be
// considered the second half of a double key press.
func isDoublePress(previous, now time.Time, timeout time.Duration) bool {
	return !previous.IsZero() && now.Sub(previous) <= timeout
}

// gotoBottomOriginY returns the vertical origin that scrolls the main panel to
// its final line. When scrollPastBottom is false the last page of content is
// kept on screen rather than scrolling it off the top.
func gotoBottomOriginY(totalLines, viewHeight int, scrollPastBottom bool) int {
	reservedLines := 0
	if !scrollPastBottom {
		reservedLines = viewHeight
	}

	return int(math.Max(0, float64(totalLines-reservedLines)))
}

func (gui *Gui) scrollUpMain() error {
	mainView := gui.Views.Main
	mainView.Autoscroll = false
	ox, oy := mainView.Origin()
	newOy := int(math.Max(0, float64(oy-gui.Config.UserConfig.Gui.ScrollHeight)))
	return mainView.SetOrigin(ox, newOy)
}

func (gui *Gui) scrollDownMain() error {
	mainView := gui.Views.Main
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
	mainView := gui.Views.Main
	ox, oy := mainView.Origin()
	newOx := int(math.Max(0, float64(ox-gui.Config.UserConfig.Gui.ScrollHeight)))

	return mainView.SetOrigin(newOx, oy)
}

func (gui *Gui) scrollRightMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.Views.Main
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
	gui.Views.Main.Autoscroll = true
	return nil
}

func (gui *Gui) jumpToTopMain(g *gocui.Gui, v *gocui.View) error {
	gui.Views.Main.Autoscroll = false
	_ = gui.Views.Main.SetOrigin(0, 0)
	_ = gui.Views.Main.SetCursor(0, 0)
	return nil
}

// gotoTopMain implements the vim-style "gg" command: the first 'g' arms the
// sequence and the second 'g' pressed within gPressTimeout jumps to the top of
// the main panel.
func (gui *Gui) gotoTopMain(g *gocui.Gui, v *gocui.View) error {
	now := time.Now()
	if !isDoublePress(gui.State.lastGPressedAt, now, gPressTimeout) {
		gui.State.lastGPressedAt = now
		return nil
	}

	gui.State.lastGPressedAt = time.Time{}
	return gui.jumpToTopMain(g, v)
}

// gotoBottomMain implements the vim-style "G" command, jumping to the bottom of
// the main panel.
func (gui *Gui) gotoBottomMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.Views.Main
	mainView.Autoscroll = false

	_, viewHeight := mainView.Size()
	ox, _ := mainView.Origin()
	newOy := gotoBottomOriginY(mainView.ViewLinesHeight(), viewHeight, gui.Config.UserConfig.Gui.ScrollPastBottom)

	return mainView.SetOrigin(ox, newOy)
}

func (gui *Gui) onMainTabClick(tabIndex int) error {
	gui.Log.Warn(tabIndex)

	currentSidePanel, ok := gui.currentSidePanel()

	if !ok {
		return nil
	}

	currentSidePanel.SetMainTabIndex(tabIndex)
	return currentSidePanel.HandleSelect()
}

func (gui *Gui) handleEnterMain(g *gocui.Gui, v *gocui.View) error {
	mainView := gui.Views.Main
	mainView.ParentView = v

	return gui.switchFocus(mainView)
}

func (gui *Gui) handleExitMain(g *gocui.Gui, v *gocui.View) error {
	v.ParentView = nil
	return gui.returnFocus()
}

func (gui *Gui) handleMainClick() error {
	if gui.popupPanelFocused() {
		return nil
	}

	currentView := gui.g.CurrentView()

	if currentView.Name() != "main" {
		gui.Views.Main.ParentView = currentView
	}

	return gui.switchFocus(gui.Views.Main)
}
