package gui

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

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
			if err := gui.onFocusLost(previousView, newView); err != nil {
				return err
			}
			if err := gui.onFocus(newView); err != nil {
				return err
			}
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

func (gui *Gui) onFocusLost(v *gocui.View, newView *gocui.View) error {
	if v == nil {
		return nil
	}

	if !gui.isPopupPanel(newView.Name()) {
		v.ParentView = nil
	}

	// refocusing because in responsive mode (when the window is very short) we want to ensure that after the view size changes we can still see the last selected item
	if err := gui.focusPointInView(v); err != nil {
		return err
	}

	gui.Log.Info(v.Name() + " focus lost")
	return nil
}

func (gui *Gui) onFocus(v *gocui.View) error {
	if v == nil {
		return nil
	}

	if err := gui.focusPointInView(v); err != nil {
		return err
	}

	gui.Log.Info(v.Name() + " focus gained")
	return nil
}

// layout is called for every screen re-render e.g. when the screen is resized
func (gui *Gui) layout(g *gocui.Gui) error {
	g.Highlight = true
	width, height := g.Size()

	information := "lazydocker " + gui.Config.Version
	if gui.g.Mouse {
		donate := color.New(color.FgMagenta, color.Underline).Sprint(gui.Tr.Donate)
		information = donate + " " + information
	}

	minimumHeight := 9
	minimumWidth := 10
	if height < minimumHeight || width < minimumWidth {
		v, err := g.SetView("limit", 0, 0, width-1, height-1, 0)
		if err != nil {
			if err.Error() != "unknown view" {
				return err
			}
			v.Title = gui.Tr.NotEnoughSpace
			v.Wrap = true
			_, _ = g.SetViewOnTop("limit")
		}
		return nil
	}

	currView := gui.g.CurrentView()
	currentCyclebleView := gui.peekPreviousView()
	if currView != nil {
		viewName := currView.Name()
		usePreviouseView := true
		for _, view := range gui.CyclableViews {
			if view == viewName {
				currentCyclebleView = viewName
				usePreviouseView = false
				break
			}
		}
		if usePreviouseView {
			currentCyclebleView = gui.peekPreviousView()
		}
	}

	usableSpace := height - 4

	tallPanels := 3
	var vHeights map[string]int
	if gui.DockerCommand.InDockerComposeProject {
		tallPanels++
		vHeights = map[string]int{
			"project":    3,
			"services":   usableSpace/tallPanels + usableSpace%tallPanels,
			"containers": usableSpace / tallPanels,
			"images":     usableSpace / tallPanels,
			"volumes":    usableSpace / tallPanels,
			"options":    1,
		}
	} else {
		vHeights = map[string]int{
			"project":    3,
			"containers": usableSpace/tallPanels + usableSpace%tallPanels,
			"images":     usableSpace / tallPanels,
			"volumes":    usableSpace / tallPanels,
			"options":    1,
		}
	}

	if height < 28 {
		defaultHeight := 3
		if height < 21 {
			defaultHeight = 1
		}
		vHeights = map[string]int{
			"project":    defaultHeight,
			"containers": defaultHeight,
			"images":     defaultHeight,
			"volumes":    defaultHeight,
			"options":    defaultHeight,
		}
		if gui.DockerCommand.InDockerComposeProject {
			vHeights["services"] = defaultHeight
		}
		vHeights[currentCyclebleView] = height - defaultHeight*tallPanels - 1
	}

	optionsVersionBoundary := width - max(len(utils.Decolorise(information)), 1)
	leftSideWidth := width / 3

	appStatus := gui.statusManager.getStatusString()
	appStatusOptionsBoundary := 0
	if appStatus != "" {
		appStatusOptionsBoundary = len(appStatus) + 2
	}

	_, _ = g.SetViewOnBottom("limit")
	g.DeleteView("limit")

	v, err := g.SetView("main", leftSideWidth+1, 0, width-1, height-2, gocui.LEFT)
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel
		v.FgColor = gocui.ColorDefault

		// when you run a docker container with the -it flags (interactive mode) it adds carriage returns for some reason. This is not docker's fault, it's an os-level default.
		v.IgnoreCarriageReturns = true
	}

	if v, err := g.SetView("project", 0, 0, leftSideWidth, vHeights["project"]-1, gocui.BOTTOM|gocui.RIGHT); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.Title = gui.Tr.ProjectTitle
		v.FgColor = gocui.ColorDefault
	}

	var servicesView *gocui.View
	aboveContainersView := "project"
	if gui.DockerCommand.InDockerComposeProject {
		aboveContainersView = "services"
		servicesView, err = g.SetViewBeneath("services", "project", vHeights["services"])
		if err != nil {
			if err.Error() != "unknown view" {
				return err
			}
			servicesView.Highlight = true
			servicesView.Title = gui.Tr.ServicesTitle
			servicesView.FgColor = gocui.ColorDefault
		}
	}

	containersView, err := g.SetViewBeneath("containers", aboveContainersView, vHeights["containers"])
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		containersView.Highlight = true
		if gui.Config.UserConfig.Gui.ShowAllContainers || !gui.DockerCommand.InDockerComposeProject {
			containersView.Title = gui.Tr.ContainersTitle
		} else {
			containersView.Title = gui.Tr.StandaloneContainersTitle
		}
		containersView.FgColor = gocui.ColorDefault
	}

	imagesView, err := g.SetViewBeneath("images", "containers", vHeights["images"])
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		imagesView.Highlight = true
		imagesView.Title = gui.Tr.ImagesTitle
		imagesView.FgColor = gocui.ColorDefault
	}

	volumesView, err := g.SetViewBeneath("volumes", "images", vHeights["volumes"])
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		volumesView.Highlight = true
		volumesView.Title = gui.Tr.VolumesTitle
		volumesView.FgColor = gocui.ColorDefault
	}

	if v, err := g.SetView("options", appStatusOptionsBoundary-1, height-2, optionsVersionBoundary-1, height, 0); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.Frame = false
		if v.FgColor, err = gui.GetOptionsPanelTextColor(); err != nil {
			return err
		}
	}

	if appStatusView, err := g.SetView("appStatus", -1, height-2, width, height, 0); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		appStatusView.BgColor = gocui.ColorDefault
		appStatusView.FgColor = gocui.ColorCyan
		appStatusView.Frame = false
		if _, err := g.SetViewOnBottom("appStatus"); err != nil {
			return err
		}
	}

	if v, err := g.SetView("information", optionsVersionBoundary-1, height-2, width, height, 0); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.BgColor = gocui.ColorDefault
		v.FgColor = gocui.ColorGreen
		v.Frame = false
		if err := gui.renderString(g, "information", information); err != nil {
			return err
		}

		// doing this here because it'll only happen once
		if err := gui.loadNewDirectory(); err != nil {
			return err
		}
	}

	if gui.g.CurrentView() == nil {
		v, err := gui.g.View(gui.peekPreviousView())
		if err != nil {
			viewName := gui.initiallyFocusedViewName()
			v, err = gui.g.View(viewName)
			if err != nil {
				return err
			}
		}

		if err := gui.switchFocus(gui.g, nil, v, false); err != nil {
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

func (gui *Gui) focusPointInView(view *gocui.View) error {
	if view == nil {
		return nil
	}

	listViews := map[string]listViewState{
		"containers": {selectedLine: gui.State.Panels.Containers.SelectedLine, lineCount: len(gui.DockerCommand.DisplayContainers)},
		"images":     {selectedLine: gui.State.Panels.Images.SelectedLine, lineCount: len(gui.DockerCommand.Images)},
		"volumes":    {selectedLine: gui.State.Panels.Volumes.SelectedLine, lineCount: len(gui.DockerCommand.Volumes)},
		"services":   {selectedLine: gui.State.Panels.Services.SelectedLine, lineCount: len(gui.DockerCommand.Services)},
		"menu":       {selectedLine: gui.State.Panels.Menu.SelectedLine, lineCount: gui.State.MenuItemCount},
	}

	if state, ok := listViews[view.Name()]; ok {
		if err := gui.focusPoint(0, state.selectedLine, state.lineCount, view); err != nil {
			return err
		}
	}

	return nil
}
