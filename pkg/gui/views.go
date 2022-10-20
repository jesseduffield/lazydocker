package gui

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
)

const SEARCH_PREFIX = "filter: "

type Views struct {
	// side panels
	Project    *gocui.View
	Services   *gocui.View
	Containers *gocui.View
	Images     *gocui.View
	Volumes    *gocui.View

	// main panel
	Main *gocui.View

	// bottom line
	Options     *gocui.View
	Information *gocui.View
	AppStatus   *gocui.View
	// text that prompts you to enter text in the Search view
	SearchPrefix *gocui.View
	// appears next to the SearchPrefix view, it's where you type in the search string
	Search *gocui.View

	// popups
	Confirmation *gocui.View
	Menu         *gocui.View

	// will cover everything when it appears
	Limit *gocui.View
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
		{viewPtr: &gui.Views.Search, name: "search"},
		{viewPtr: &gui.Views.SearchPrefix, name: "searchPrefix"},

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

	selectedLineBgColor := gocui.ColorBlue

	gui.Views.Main.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel
	// when you run a docker container with the -it flags (interactive mode) it adds carriage returns for some reason. This is not docker's fault, it's an os-level default.
	gui.Views.Main.IgnoreCarriageReturns = true

	gui.Views.Project.Title = gui.Tr.ProjectTitle

	gui.Views.Services.Highlight = true
	gui.Views.Services.Title = gui.Tr.ServicesTitle
	gui.Views.Services.SelBgColor = selectedLineBgColor

	gui.Views.Containers.Highlight = true
	gui.Views.Containers.SelBgColor = selectedLineBgColor
	if gui.Config.UserConfig.Gui.ShowAllContainers || !gui.DockerCommand.InDockerComposeProject {
		gui.Views.Containers.Title = gui.Tr.ContainersTitle
	} else {
		gui.Views.Containers.Title = gui.Tr.StandaloneContainersTitle
	}

	gui.Views.Images.Highlight = true
	gui.Views.Images.Title = gui.Tr.ImagesTitle
	gui.Views.Images.SelBgColor = selectedLineBgColor

	gui.Views.Volumes.Highlight = true
	gui.Views.Volumes.Title = gui.Tr.VolumesTitle
	gui.Views.Volumes.SelBgColor = selectedLineBgColor

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
	gui.Views.Menu.SelBgColor = selectedLineBgColor

	gui.Views.Limit.Visible = false
	gui.Views.Limit.Title = gui.Tr.NotEnoughSpace
	gui.Views.Limit.Wrap = true

	gui.Views.SearchPrefix.BgColor = gocui.ColorDefault
	gui.Views.SearchPrefix.FgColor = gocui.ColorGreen
	gui.Views.SearchPrefix.Frame = false
	_ = gui.setViewContent(gui.Views.SearchPrefix, SEARCH_PREFIX)

	gui.Views.Search.BgColor = gocui.ColorDefault
	gui.Views.Search.FgColor = gocui.ColorGreen
	gui.Views.Search.Editable = true
	gui.Views.Search.Frame = false
	gui.Views.Search.Editor = gocui.EditorFunc(gui.wrapEditor(gocui.SimpleEditor))

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
	return []string{
		"project",
		"services",
		"containers",
		"images",
		"volumes",
		"options",
		"information",
		"appStatus",
		"main",
		"limit",
		"searchPrefix",
		"search",
	}
}
