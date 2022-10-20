package gui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
	"github.com/spkg/bom"
)

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
	return gui.switchFocus(focusedView)
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
	return gui.switchFocus(focusedView)
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
		return gui.Panels.Images.HandleSelect()
	case "volumes":
		return gui.handleVolumeSelect(gui.g, v)
	case "confirmation":
		return nil
	case "main":
		v.Highlight = false
		return nil
	case "search":
		return nil
	default:
		panic(gui.Tr.NoViewMachingNewLineFocusedSwitchStatement)
	}
}

// TODO: move some of this logic into our onFocusLost and onFocus hooks
func (gui *Gui) switchFocus(newView *gocui.View) error {
	gui.Mutexes.ViewStackMutex.Lock()
	defer gui.Mutexes.ViewStackMutex.Unlock()

	return gui.switchFocusAux(newView)
}

func (gui *Gui) switchFocusAux(newView *gocui.View) error {
	gui.pushView(newView.Name())
	gui.Log.Info("setting highlight to true for view " + newView.Name())
	gui.Log.Info("new focused view is " + newView.Name())
	if _, err := gui.g.SetCurrentView(newView.Name()); err != nil {
		return err
	}

	gui.g.Cursor = newView.Editable

	if err := gui.renderPanelOptions(); err != nil {
		return err
	}

	return gui.newLineFocused(newView)
}

func (gui *Gui) returnFocus() error {
	gui.Mutexes.ViewStackMutex.Lock()
	defer gui.Mutexes.ViewStackMutex.Unlock()

	if len(gui.State.ViewStack) <= 1 {
		return nil
	}

	previousViewName := gui.State.ViewStack[len(gui.State.ViewStack)-2]
	previousView, err := gui.g.View(previousViewName)
	if err != nil {
		return err
	}
	return gui.switchFocusAux(previousView)
}

// Not to be called directly. Use `switchFocus` instead
func (gui *Gui) pushView(name string) {
	// No matter what view we're pushing, we first remove all popup panels from the stack
	gui.State.ViewStack = lo.Filter(gui.State.ViewStack, func(viewName string, _ int) bool {
		return viewName != "confirmation" && viewName != "menu"
	})

	// If we're pushing a side panel, we remove all other panels
	if lo.Contains(gui.sideViewNames(), name) {
		gui.State.ViewStack = []string{}
	}

	// If we're pushing a panel that's already in the stack, we remove it
	gui.State.ViewStack = lo.Filter(gui.State.ViewStack, func(viewName string, _ int) bool {
		return viewName != name
	})

	gui.State.ViewStack = append(gui.State.ViewStack, name)
}

// excludes popups
func (gui *Gui) currentStaticViewName() string {
	gui.Mutexes.ViewStackMutex.Lock()
	defer gui.Mutexes.ViewStackMutex.Unlock()

	for i := len(gui.State.ViewStack) - 1; i >= 0; i-- {
		if !lo.Contains(gui.popupViewNames(), gui.State.ViewStack[i]) {
			return gui.State.ViewStack[i]
		}
	}

	return gui.initiallyFocusedViewName()
}

func (gui *Gui) currentSideViewName() string {
	gui.Mutexes.ViewStackMutex.Lock()
	defer gui.Mutexes.ViewStackMutex.Unlock()

	// we expect that there is a side window somewhere in the view stack, so we will search from top to bottom
	for idx := range gui.State.ViewStack {
		reversedIdx := len(gui.State.ViewStack) - 1 - idx
		viewName := gui.State.ViewStack[reversedIdx]
		if lo.Contains(gui.sideViewNames(), viewName) {
			return viewName
		}
	}

	return gui.initiallyFocusedViewName()
}

// if the cursor down past the last item, move it to the last line
// nolint:unparam
func (gui *Gui) focusPoint(selectedX int, selectedY int, lineCount int, v *gocui.View) {
	if selectedY < 0 || selectedY > lineCount {
		return
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
}

func (gui *Gui) focusY(selectedY int, lineCount int, v *gocui.View) {
	gui.focusPoint(0, selectedY, lineCount, v)
}

// TODO: combine with above
func (gui *Gui) FocusY(selectedY int, lineCount int, v *gocui.View) {
	gui.focusY(selectedY, lineCount, v)
}

func (gui *Gui) cleanString(s string) string {
	output := string(bom.Clean([]byte(s)))
	return utils.NormalizeLinefeeds(output)
}

func (gui *Gui) setViewContent(v *gocui.View, s string) error {
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
		return gui.setViewContent(v, s)
	})
	return nil
}

func (gui *Gui) renderStringMain(s string) error {
	return gui.renderString(gui.g, "main", s)
}

// TODO: merge with above
func (gui *Gui) RenderStringMain(s string) error {
	return gui.renderStringMain(s)
}

// reRenderString sets the main view's content, without changing its origin
func (gui *Gui) reRenderStringMain(s string) {
	gui.reRenderString("main", s)
}

// reRenderString sets the view's content, without changing its origin
func (gui *Gui) reRenderString(viewName, s string) {
	gui.g.Update(func(*gocui.Gui) error {
		v, err := gui.g.View(viewName)
		if err != nil {
			return nil // return gracefully if view has been deleted
		}
		return gui.setViewContent(v, s)
	})
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
	return gui.Views.Main
}

func (gui *Gui) GetMainView() *gocui.View {
	return gui.getMainView()
}

func (gui *Gui) trimmedContent(v *gocui.View) string {
	return strings.TrimSpace(v.Buffer())
}

func (gui *Gui) currentViewName() string {
	currentView := gui.g.CurrentView()
	// this can happen when the app is first starting up
	if currentView == nil {
		return gui.initiallyFocusedViewName()
	}
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

// TODO: merge into above
func (gui *Gui) PopupPanelFocused() bool {
	return gui.popupPanelFocused()
}

func (gui *Gui) clearMainView() {
	mainView := gui.getMainView()
	mainView.Clear()
	_ = mainView.SetOrigin(0, 0)
	_ = mainView.SetCursor(0, 0)
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

	newSelectedLine := cy + oy

	if newSelectedLine < 0 {
		newSelectedLine = 0
	}

	if newSelectedLine > itemCount-1 {
		newSelectedLine = itemCount - 1
	}

	*selectedLine = newSelectedLine

	return handleSelect(gui.g, v)
}

// TODO: combine with above
func (gui *Gui) HandleClick(v *gocui.View, itemCount int, selectedLine *int, handleSelect func() error) error {
	wrappedHandleSelect := func(g *gocui.Gui, v *gocui.View) error {
		return handleSelect()
	}
	return gui.handleClick(v, itemCount, selectedLine, wrappedHandleSelect)
}

func (gui *Gui) nextScreenMode() error {
	if gui.currentViewName() == "main" {
		gui.State.ScreenMode = prevIntInCycle([]WindowMaximisation{SCREEN_NORMAL, SCREEN_HALF, SCREEN_FULL}, gui.State.ScreenMode)

		return nil
	}

	gui.State.ScreenMode = nextIntInCycle([]WindowMaximisation{SCREEN_NORMAL, SCREEN_HALF, SCREEN_FULL}, gui.State.ScreenMode)

	return nil
}

func (gui *Gui) prevScreenMode() error {
	if gui.currentViewName() == "main" {
		gui.State.ScreenMode = nextIntInCycle([]WindowMaximisation{SCREEN_NORMAL, SCREEN_HALF, SCREEN_FULL}, gui.State.ScreenMode)

		return nil
	}

	gui.State.ScreenMode = prevIntInCycle([]WindowMaximisation{SCREEN_NORMAL, SCREEN_HALF, SCREEN_FULL}, gui.State.ScreenMode)

	return nil
}

func nextIntInCycle(sl []WindowMaximisation, current WindowMaximisation) WindowMaximisation {
	for i, val := range sl {
		if val == current {
			if i == len(sl)-1 {
				return sl[0]
			}
			return sl[i+1]
		}
	}
	return sl[0]
}

func prevIntInCycle(sl []WindowMaximisation, current WindowMaximisation) WindowMaximisation {
	for i, val := range sl {
		if val == current {
			if i > 0 {
				return sl[i-1]
			}
			return sl[len(sl)-1]
		}
	}
	return sl[len(sl)-1]
}

func (gui *Gui) CurrentView() *gocui.View {
	return gui.g.CurrentView()
}
