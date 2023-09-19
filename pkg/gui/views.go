package gui

import (
	"os"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/samber/lo"
)

// See https://github.com/xtermjs/xterm.js/issues/4238
// VSCode is soon to fix this in an upcoming update.
// Once that's done, we can scrap the HIDE_UNDERSCORES variable
var (
	underscoreEnvChecked bool
	hideUnderscores      bool
)

func hideUnderScores() bool {
	if !underscoreEnvChecked {
		hideUnderscores = os.Getenv("TERM_PROGRAM") == "vscode"
		underscoreEnvChecked = true
	}

	return hideUnderscores
}

type Views struct {
	// side panels
	Project    *gocui.View
	Services   *gocui.View
	Containers *gocui.View
	Images     *gocui.View
	Volumes    *gocui.View
	Networks   *gocui.View

	// main panel
	Main *gocui.View

	// bottom line
	Options     *gocui.View
	Information *gocui.View
	AppStatus   *gocui.View
	// text that prompts you to enter text in the Filter view
	FilterPrefix *gocui.View
	// appears next to the SearchPrefix view, it's where you type in the search string
	Filter *gocui.View

	// popups
	Confirmation *gocui.View
	Menu         *gocui.View

	// will cover everything when it appears
	Limit *gocui.View
}

type viewNameMapping struct {
	viewPtr **gocui.View
	name    string
	// if true, we handle the position/size of the view in arrangement.go. Otherwise
	// we handle it manually.
	autoPosition bool
}

func (gui *Gui) orderedViewNameMappings() []viewNameMapping {
	return []viewNameMapping{
		// first layer. Ordering within this layer does not matter because there are
		// no overlapping views
		{viewPtr: &gui.Views.Project, name: "project", autoPosition: true},
		{viewPtr: &gui.Views.Services, name: "services", autoPosition: true},
		{viewPtr: &gui.Views.Containers, name: "containers", autoPosition: true},
		{viewPtr: &gui.Views.Images, name: "images", autoPosition: true},
		{viewPtr: &gui.Views.Volumes, name: "volumes", autoPosition: true},
		{viewPtr: &gui.Views.Networks, name: "networks", autoPosition: true},

		{viewPtr: &gui.Views.Main, name: "main", autoPosition: true},

		// bottom line
		{viewPtr: &gui.Views.Options, name: "options", autoPosition: true},
		{viewPtr: &gui.Views.AppStatus, name: "appStatus", autoPosition: true},
		{viewPtr: &gui.Views.Information, name: "information", autoPosition: true},
		{viewPtr: &gui.Views.Filter, name: "filter", autoPosition: true},
		{viewPtr: &gui.Views.FilterPrefix, name: "filterPrefix", autoPosition: true},

		// popups.
		{viewPtr: &gui.Views.Menu, name: "menu", autoPosition: false},
		{viewPtr: &gui.Views.Confirmation, name: "confirmation", autoPosition: false},

		// this guy will cover everything else when it appears
		{viewPtr: &gui.Views.Limit, name: "limit", autoPosition: true},
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

	selectedLineBgColor := GetGocuiStyle(gui.Config.UserConfig.Gui.Theme.SelectedLineBgColor)

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

	gui.Views.Networks.Highlight = true
	gui.Views.Networks.Title = gui.Tr.NetworksTitle
	gui.Views.Networks.SelBgColor = selectedLineBgColor

	gui.Views.Options.Frame = false
	gui.Views.Options.FgColor = gui.GetOptionsPanelTextColor()

	gui.Views.AppStatus.FgColor = gocui.ColorCyan
	gui.Views.AppStatus.Frame = false

	gui.Views.Information.Frame = false
	gui.Views.Information.FgColor = gocui.ColorGreen

	gui.Views.Confirmation.Visible = false
	gui.Views.Confirmation.Wrap = true
	gui.Views.Menu.Visible = false
	gui.Views.Menu.SelBgColor = selectedLineBgColor

	gui.Views.Limit.Visible = false
	gui.Views.Limit.Title = gui.Tr.NotEnoughSpace
	gui.Views.Limit.Wrap = true

	gui.Views.FilterPrefix.BgColor = gocui.ColorDefault
	gui.Views.FilterPrefix.FgColor = gocui.ColorGreen
	gui.Views.FilterPrefix.Frame = false

	gui.Views.Filter.BgColor = gocui.ColorDefault
	gui.Views.Filter.FgColor = gocui.ColorGreen
	gui.Views.Filter.Editable = true
	gui.Views.Filter.Frame = false
	gui.Views.Filter.Editor = gocui.EditorFunc(gui.wrapEditor(gocui.SimpleEditor))

	return nil
}

func (gui *Gui) setInitialViewContent() error {
	if err := gui.renderString(gui.g, "information", gui.getInformationContent()); err != nil {
		return err
	}

	_ = gui.setViewContent(gui.Views.FilterPrefix, gui.filterPrompt())

	return nil
}

func (gui *Gui) getInformationContent() string {
	informationStr := gui.Config.Version
	if !gui.g.Mouse {
		return informationStr
	}

	attrs := []color.Attribute{color.FgMagenta}
	if !hideUnderScores() {
		attrs = append(attrs, color.Underline)
	}

	donate := color.New(attrs...).Sprint(gui.Tr.Donate)
	return donate + " " + informationStr
}

func (gui *Gui) popupViewNames() []string {
	return []string{"confirmation", "menu"}
}

// these views have their position and size determined by arrangement.go
func (gui *Gui) autoPositionedViewNames() []string {
	views := lo.Filter(gui.orderedViewNameMappings(), func(viewNameMapping viewNameMapping, _ int) bool {
		return viewNameMapping.autoPosition
	})

	return lo.Map(views, func(viewNameMapping viewNameMapping, _ int) string {
		return viewNameMapping.name
	})
}
