package gui

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
)

type Views struct {
	Project    *gocui.View
	Services   *gocui.View
	Containers *gocui.View
	Images     *gocui.View
	Volumes    *gocui.View

	Main *gocui.View

	Options      *gocui.View
	Confirmation *gocui.View
	Menu         *gocui.View
	Information  *gocui.View
	AppStatus    *gocui.View
	Limit        *gocui.View
}

type viewNameMapping struct {
	viewPtr **gocui.View
	name    string
}

func (gui *Gui) orderedViewNameMappings() []viewNameMapping {
	return []viewNameMapping{
		// first layer. Ordering within this layer does not matter because there are
		// no overlapping views
		{viewPtr: &gui.Views.Project, name: "project"},
		{viewPtr: &gui.Views.Services, name: "services"},
		{viewPtr: &gui.Views.Containers, name: "containers"},
		{viewPtr: &gui.Views.Images, name: "images"},
		{viewPtr: &gui.Views.Volumes, name: "volumes"},

		{viewPtr: &gui.Views.Main, name: "main"},

		// bottom line
		{viewPtr: &gui.Views.Options, name: "options"},
		{viewPtr: &gui.Views.AppStatus, name: "appStatus"},
		{viewPtr: &gui.Views.Information, name: "information"},

		// popups.
		{viewPtr: &gui.Views.Menu, name: "menu"},
		{viewPtr: &gui.Views.Confirmation, name: "confirmation"},

		// this guy will cover everything else when it appears
		{viewPtr: &gui.Views.Limit, name: "limit"},
	}
}

func (gui *Gui) createAllViews() error {
	var err error
	for _, mapping := range gui.orderedViewNameMappings() {
		*mapping.viewPtr, err = gui.prepareView(mapping.name)
		if err != nil && err.Error() != UNKNOWN_VIEW_ERROR_MSG {
			return err
		}
		(*mapping.viewPtr).FgColor = gocui.ColorDefault
	}

	gui.Views.Main.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel
	// when you run a docker container with the -it flags (interactive mode) it adds carriage returns for some reason. This is not docker's fault, it's an os-level default.
	gui.Views.Main.IgnoreCarriageReturns = true

	gui.Views.Project.Title = gui.Tr.ProjectTitle

	gui.Views.Services.Highlight = true
	gui.Views.Services.Title = gui.Tr.ServicesTitle

	gui.Views.Containers.Highlight = true
	if gui.Config.UserConfig.Gui.ShowAllContainers || !gui.DockerCommand.InDockerComposeProject {
		gui.Views.Containers.Title = gui.Tr.ContainersTitle
	} else {
		gui.Views.Containers.Title = gui.Tr.StandaloneContainersTitle
	}

	gui.Views.Images.Highlight = true
	gui.Views.Images.Title = gui.Tr.ImagesTitle

	gui.Views.Volumes.Highlight = true
	gui.Views.Volumes.Title = gui.Tr.VolumesTitle

	gui.Views.Options.Frame = false
	gui.Views.Options.FgColor = gui.GetOptionsPanelTextColor()

	gui.Views.AppStatus.FgColor = gocui.ColorCyan
	gui.Views.AppStatus.Frame = false

	gui.Views.Information.Frame = false
	gui.Views.Information.FgColor = gocui.ColorGreen

	if err := gui.renderString(gui.g, "information", gui.getInformationContent()); err != nil {
		return err
	}

	gui.Views.Confirmation.Visible = false
	gui.Views.Confirmation.Wrap = true
	gui.Views.Menu.Visible = false

	gui.Views.Limit.Visible = false
	gui.Views.Limit.Title = gui.Tr.NotEnoughSpace
	gui.Views.Limit.Wrap = true

	gui.waitForIntro.Done()

	return nil
}

func (gui *Gui) getInformationContent() string {
	informationStr := gui.Config.Version
	if !gui.g.Mouse {
		return informationStr
	}

	donate := color.New(color.FgMagenta, color.Underline).Sprint(gui.Tr.Donate)
	return donate + " " + informationStr
}

func (gui *Gui) popupViewNames() []string {
	return []string{"confirmation", "menu"}
}

// these views have their position and size determined by arrangement.go
func (gui *Gui) controlledBoundsViewNames() []string {
	return []string{"project", "services", "containers", "images", "volumes", "options", "information", "appStatus", "main", "limit"}
}
