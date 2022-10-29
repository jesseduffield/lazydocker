package gui

import (
	"github.com/jesseduffield/gocui"
	"github.com/samber/lo"
)

func (gui *Gui) newLineFocused(v *gocui.View) error {
	if v == nil {
		return nil
	}

	currentListPanel, ok := gui.currentListPanel()
	if ok {
		return currentListPanel.HandleSelect()
	}

	switch v.Name() {
	case "confirmation":
		return nil
	case "main":
		v.Highlight = false
		return nil
	case "filter":
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

	newViewStack := gui.State.ViewStack

	if gui.State.Filter.panel != nil && !lo.Contains(newViewStack, gui.State.Filter.panel.GetView().Name()) {
		if err := gui.clearFilter(); err != nil {
			return err
		}
	}

	// TODO: add 'onFocusLost' hook
	if !lo.Contains(newViewStack, "menu") {
		gui.Views.Menu.Visible = false
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

func (gui *Gui) removeViewFromStack(view *gocui.View) {
	gui.Mutexes.ViewStackMutex.Lock()
	defer gui.Mutexes.ViewStackMutex.Unlock()

	gui.State.ViewStack = lo.Filter(gui.State.ViewStack, func(viewName string, _ int) bool {
		return viewName != view.Name()
	})
}

// Not to be called directly. Use `switchFocus` instead
func (gui *Gui) pushView(name string) {
	// No matter what view we're pushing, we first remove all popup panels from the stack
	// (unless it's the search view because we may be searching the menu panel)
	if name != "filter" {
		gui.State.ViewStack = lo.Filter(gui.State.ViewStack, func(viewName string, _ int) bool {
			return !gui.isPopupPanel(viewName)
		})
	}

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
