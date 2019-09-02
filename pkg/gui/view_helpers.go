package gui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/spkg/bom"
)

func (gui *Gui) refreshSidePanels(g *gocui.Gui) error {
	// not refreshing containers and services here given that we do it every few milliseconds anyway
	if err := gui.refreshImages(); err != nil {
		return err
	}

	return nil
}

func (gui *Gui) nextView(g *gocui.Gui, v *gocui.View) error {
	var focusedViewName string
	if v == nil || v.Name() == gui.CyclableViews[len(gui.CyclableViews)-1] {
		focusedViewName = gui.CyclableViews[0]
	} else {
		viewName := v.Name()
		for i := range gui.CyclableViews {
			if viewName == gui.CyclableViews[i] {
				focusedViewName = gui.CyclableViews[i+1]
				break
			}
			if i == len(gui.CyclableViews)-1 {
				gui.Log.Info("not in list of views")
				return nil
			}
		}
	}
	focusedView, err := g.View(focusedViewName)
	if err != nil {
		panic(err)
	}
	gui.resetMainView()
	gui.popPreviousView()
	return gui.switchFocus(g, v, focusedView, false)
}

func (gui *Gui) previousView(g *gocui.Gui, v *gocui.View) error {
	var focusedViewName string
	if v == nil || v.Name() == gui.CyclableViews[0] {
		focusedViewName = gui.CyclableViews[len(gui.CyclableViews)-1]
	} else {
		viewName := v.Name()
		for i := range gui.CyclableViews {
			if viewName == gui.CyclableViews[i] {
				focusedViewName = gui.CyclableViews[i-1]
				break
			}
			if i == len(gui.CyclableViews)-1 {
				gui.Log.Info("not in list of views")
				return nil
			}
		}
	}
	focusedView, err := g.View(focusedViewName)
	if err != nil {
		panic(err)
	}
	gui.resetMainView()
	gui.popPreviousView()
	return gui.switchFocus(g, v, focusedView, false)
}

func (gui *Gui) resetMainView() {
	gui.State.Panels.Main.ObjectKey = ""
	gui.getMainView().Wrap = gui.Config.UserConfig.Gui.WrapMainPanel
}

func (gui *Gui) newLineFocused(v *gocui.View) error {
	if v == nil {
		return nil
	}

	switch v.Name() {
	case "menu":
		return gui.handleMenuSelect(gui.g, v)
	case "project":
		return gui.handleProjectSelect(gui.g, v)
	case "services":
		return gui.handleServiceSelect(gui.g, v)
	case "containers":
		return gui.handleContainerSelect(gui.g, v)
	case "images":
		return gui.handleImageSelect(gui.g, v)
	case "volumes":
		return gui.handleVolumeSelect(gui.g, v)
	case "confirmation":
		return nil
	case "main":
		v.Highlight = false
		return nil
	default:
		panic(gui.Tr.NoViewMachingNewLineFocusedSwitchStatement)
	}
}

func (gui *Gui) popPreviousView() string {
	if gui.State.PreviousViews.Len() > 0 {
		return gui.State.PreviousViews.Pop().(string)
	}

	return ""
}

func (gui *Gui) peekPreviousView() string {
	if gui.State.PreviousViews.Len() > 0 {
		return gui.State.PreviousViews.Peek().(string)
	}

	return ""
}

func (gui *Gui) pushPreviousView(name string) {
	gui.State.PreviousViews.Push(name)
}

func (gui *Gui) returnFocus(g *gocui.Gui, v *gocui.View) error {
	previousViewName := gui.popPreviousView()
	previousView, err := g.View(previousViewName)
	if err != nil {
		// always fall back to services view if there's no 'previous' view stored
		previousView, err = g.View(gui.initiallyFocusedViewName())
		if err != nil {
			gui.Log.Error(err)
		}
	}
	return gui.switchFocus(g, v, previousView, true)
}

// pass in oldView = nil if you don't want to be able to return to your old view
// TODO: move some of this logic into our onFocusLost and onFocus hooks
func (gui *Gui) switchFocus(g *gocui.Gui, oldView, newView *gocui.View, returning bool) error {
	// we assume we'll never want to return focus to a popup panel i.e.
	// we should never stack popup panels
	if oldView != nil && !gui.isPopupPanel(oldView.Name()) && !returning {
		gui.pushPreviousView(oldView.Name())
	}

	gui.Log.Info("setting highlight to true for view " + newView.Name())
	gui.Log.Info("new focused view is " + newView.Name())
	if _, err := g.SetCurrentView(newView.Name()); err != nil {
		return err
	}
	if _, err := g.SetViewOnTop(newView.Name()); err != nil {
		return err
	}

	g.Cursor = newView.Editable

	if err := gui.renderPanelOptions(); err != nil {
		return err
	}

	return gui.newLineFocused(newView)
}

// if the cursor down past the last item, move it to the last line
func (gui *Gui) focusPoint(selectedX int, selectedY int, lineCount int, v *gocui.View) error {
	if selectedY < 0 || selectedY > lineCount {
		return nil
	}
	ox, oy := v.Origin()
	originalOy := oy
	cx, cy := v.Cursor()
	originalCy := cy
	_, height := v.Size()

	ly := utils.Max(height-1, 0)

	windowStart := oy
	windowEnd := oy + ly

	if selectedY < windowStart {
		oy = utils.Max(oy-(windowStart-selectedY), 0)
	} else if selectedY > windowEnd {
		oy += (selectedY - windowEnd)
	}

	if windowEnd > lineCount-1 {
		shiftAmount := (windowEnd - (lineCount - 1))
		oy = utils.Max(oy-shiftAmount, 0)
	}

	if originalOy != oy {
		_ = v.SetOrigin(ox, oy)
	}

	cy = selectedY - oy
	if originalCy != cy {
		_ = v.SetCursor(cx, selectedY-oy)
	}
	return nil
}

func (gui *Gui) cleanString(s string) string {
	output := string(bom.Clean([]byte(s)))
	return utils.NormalizeLinefeeds(output)
}

func (gui *Gui) setViewContent(g *gocui.Gui, v *gocui.View, s string) error {
	v.Clear()
	fmt.Fprint(v, gui.cleanString(s))
	return nil
}

// renderString resets the origin of a view and sets its content
func (gui *Gui) renderString(g *gocui.Gui, viewName, s string) error {
	g.Update(func(*gocui.Gui) error {
		v, err := g.View(viewName)
		if err != nil {
			return nil // return gracefully if view has been deleted
		}
		if err := v.SetOrigin(0, 0); err != nil {
			return err
		}
		if err := v.SetCursor(0, 0); err != nil {
			return err
		}
		return gui.setViewContent(gui.g, v, s)
	})
	return nil
}

// reRenderString sets the view's content, without changing its origin
func (gui *Gui) reRenderString(g *gocui.Gui, viewName, s string) error {
	g.Update(func(*gocui.Gui) error {
		v, err := g.View(viewName)
		if err != nil {
			return nil // return gracefully if view has been deleted
		}
		return gui.setViewContent(gui.g, v, s)
	})
	return nil
}

func (gui *Gui) optionsMapToString(optionsMap map[string]string) string {
	optionsArray := make([]string, 0)
	for key, description := range optionsMap {
		optionsArray = append(optionsArray, key+": "+description)
	}
	sort.Strings(optionsArray)
	return strings.Join(optionsArray, ", ")
}

func (gui *Gui) renderOptionsMap(optionsMap map[string]string) error {
	return gui.renderString(gui.g, "options", gui.optionsMapToString(optionsMap))
}

func (gui *Gui) getProjectView() *gocui.View {
	v, _ := gui.g.View("project")
	return v
}

func (gui *Gui) getServicesView() *gocui.View {
	v, _ := gui.g.View("services")
	return v
}

func (gui *Gui) getContainersView() *gocui.View {
	v, _ := gui.g.View("containers")
	return v
}

func (gui *Gui) getImagesView() *gocui.View {
	v, _ := gui.g.View("images")
	return v
}

func (gui *Gui) getVolumesView() *gocui.View {
	v, _ := gui.g.View("volumes")
	return v
}

func (gui *Gui) getMainView() *gocui.View {
	v, _ := gui.g.View("main")
	return v
}

func (gui *Gui) trimmedContent(v *gocui.View) string {
	return strings.TrimSpace(v.Buffer())
}

func (gui *Gui) currentViewName() string {
	currentView := gui.g.CurrentView()
	return currentView.Name()
}

func (gui *Gui) resizeCurrentPopupPanel(g *gocui.Gui) error {
	v := g.CurrentView()
	if gui.isPopupPanel(v.Name()) {
		return gui.resizePopupPanel(g, v)
	}
	return nil
}

func (gui *Gui) resizePopupPanel(g *gocui.Gui, v *gocui.View) error {
	// If the confirmation panel is already displayed, just resize the width,
	// otherwise continue
	content := v.Buffer()
	x0, y0, x1, y1 := gui.getConfirmationPanelDimensions(g, v.Wrap, content)
	vx0, vy0, vx1, vy1 := v.Dimensions()
	if vx0 == x0 && vy0 == y0 && vx1 == x1 && vy1 == y1 {
		return nil
	}
	_, err := g.SetView(v.Name(), x0, y0, x1, y1, 0)
	return err
}

func (gui *Gui) changeSelectedLine(line *int, total int, up bool) {
	if up {
		if *line == -1 || *line == 0 {
			return
		}

		*line -= 1
	} else {
		if *line == -1 || *line == total-1 {
			return
		}

		*line += 1
	}
}

func (gui *Gui) renderPanelOptions() error {
	currentView := gui.g.CurrentView()
	switch currentView.Name() {
	case "menu":
		return gui.renderMenuOptions()
	case "confirmation":
		return gui.renderConfirmationOptions()
	}
	return gui.renderGlobalOptions()
}

func (gui *Gui) isPopupPanel(viewName string) bool {
	return viewName == "confirmation" || viewName == "menu"
}

func (gui *Gui) popupPanelFocused() bool {
	return gui.isPopupPanel(gui.currentViewName())
}

func (gui *Gui) clearMainView() {
	mainView := gui.getMainView()
	mainView.Clear()
	mainView.SetOrigin(0, 0)
	mainView.SetCursor(0, 0)
}

func (gui *Gui) handleClick(v *gocui.View, itemCount int, selectedLine *int, handleSelect func(*gocui.Gui, *gocui.View) error) error {
	if gui.popupPanelFocused() && v != nil && !gui.isPopupPanel(v.Name()) {
		return nil
	}

	if _, err := gui.g.SetCurrentView(v.Name()); err != nil {
		return err
	}

	_, cy := v.Cursor()
	_, oy := v.Origin()

	newSelectedLine := cy - oy

	if newSelectedLine < 0 {
		newSelectedLine = 0
	}

	if newSelectedLine > itemCount-1 {
		newSelectedLine = itemCount - 1
	}

	*selectedLine = newSelectedLine

	return handleSelect(gui.g, v)
}
