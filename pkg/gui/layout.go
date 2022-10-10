package gui

import (
	"github.com/jesseduffield/gocui"
)

const UNKNOWN_VIEW_ERROR_MSG = "unknown view"

// getFocusLayout returns a manager function for when view gain and lose focus
func (gui *Gui) getFocusLayout() func(g *gocui.Gui) error {
	var previousView *gocui.View
	return func(g *gocui.Gui) error {
		newView := gui.g.CurrentView()
		if err := gui.onFocusChange(); err != nil {
			return err
		}
		// for now we don't consider losing focus to a popup panel as actually losing focus
		if newView != previousView && !gui.isPopupPanel(newView.Name()) {
			gui.onFocusLost(previousView, newView)
			gui.onFocus(newView)
			previousView = newView
		}
		return nil
	}
}

func (gui *Gui) onFocusChange() error {
	currentView := gui.g.CurrentView()
	for _, view := range gui.g.Views() {
		view.Highlight = view == currentView && view.Name() != "main"
	}
	return nil
}

func (gui *Gui) onFocusLost(v *gocui.View, newView *gocui.View) {
	if v == nil {
		return
	}

	if !gui.isPopupPanel(newView.Name()) {
		v.ParentView = nil
	}

	// refocusing because in responsive mode (when the window is very short) we want to ensure that after the view size changes we can still see the last selected item
	gui.focusPointInView(v)

	gui.Log.Info(v.Name() + " focus lost")
}

func (gui *Gui) onFocus(v *gocui.View) {
	if v == nil {
		return
	}

	gui.focusPointInView(v)

	gui.Log.Info(v.Name() + " focus gained")
}

// layout is called for every screen re-render e.g. when the screen is resized
func (gui *Gui) layout(g *gocui.Gui) error {
	if !gui.ViewsSetup {
		if err := gui.createAllViews(); err != nil {
			return err
		}

		gui.ViewsSetup = true
	}

	g.Highlight = true
	width, height := g.Size()

	appStatus := gui.statusManager.getStatusString()

	viewDimensions := gui.getWindowDimensions(gui.getInformationContent(), appStatus)
	// we assume that the view has already been created.
	setViewFromDimensions := func(viewName string, windowName string) (*gocui.View, error) {
		dimensionsObj, ok := viewDimensions[windowName]

		view, err := g.View(viewName)
		if err != nil {
			return nil, err
		}

		if !ok {
			// view not specified in dimensions object: so create the view and hide it
			// making the view take up the whole space in the background in case it needs
			// to render content as soon as it appears, because lazyloaded content (via a pty task)
			// cares about the size of the view.
			_, err := g.SetView(viewName, 0, 0, width, height, 0)
			view.Visible = false
			return view, err
		}

		frameOffset := 1
		if view.Frame {
			frameOffset = 0
		}
		_, err = g.SetView(
			viewName,
			dimensionsObj.X0-frameOffset,
			dimensionsObj.Y0-frameOffset,
			dimensionsObj.X1+frameOffset,
			dimensionsObj.Y1+frameOffset,
			0,
		)
		view.Visible = true

		return view, err
	}

	for _, viewName := range []string{"project", "services", "containers", "images", "volumes", "options", "information", "appStatus", "main", "limit"} {
		_, err := setViewFromDimensions(viewName, viewName)
		if err != nil && err.Error() != UNKNOWN_VIEW_ERROR_MSG {
			return err
		}
	}

	if gui.g.CurrentView() == nil {
		viewName := gui.initiallyFocusedViewName()
		view, err := gui.g.View(viewName)
		if err != nil {
			return err
		}

		if err := gui.switchFocus(view); err != nil {
			return err
		}
	}

	// here is a good place log some stuff
	// if you download humanlog and do tail -f development.log | humanlog
	// this will let you see these branches as prettified json
	// gui.Log.Info(utils.AsJson(gui.State.Branches[0:4]))
	return gui.resizeCurrentPopupPanel(g)
}

type listViewState struct {
	selectedLine int
	lineCount    int
}

func (gui *Gui) focusPointInView(view *gocui.View) {
	if view == nil {
		return
	}

	listViews := map[string]listViewState{
		"containers": {selectedLine: gui.State.Panels.Containers.SelectedLine, lineCount: len(gui.DockerCommand.DisplayContainers)},
		"images":     {selectedLine: gui.State.Panels.Images.SelectedLine, lineCount: len(gui.DockerCommand.Images)},
		"volumes":    {selectedLine: gui.State.Panels.Volumes.SelectedLine, lineCount: len(gui.DockerCommand.Volumes)},
		"services":   {selectedLine: gui.State.Panels.Services.SelectedLine, lineCount: len(gui.DockerCommand.Services)},
		"menu":       {selectedLine: gui.State.Panels.Menu.SelectedLine, lineCount: gui.State.MenuItemCount},
	}

	if state, ok := listViews[view.Name()]; ok {
		gui.focusY(state.selectedLine, state.lineCount, view)
	}
}

func (gui *Gui) prepareView(viewName string) (*gocui.View, error) {
	// arbitrarily giving the view enough size so that we don't get an error, but
	// it's expected that the view will be given the correct size before being shown
	return gui.g.SetView(viewName, 0, 0, 10, 10, 0)
}
