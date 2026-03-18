package gui

import (
	"os"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/samber/lo"
)

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
	Containers *gocui.View
	Images     *gocui.View
	Volumes    *gocui.View
	Networks   *gocui.View

	Main *gocui.View

	Options     *gocui.View
	Information *gocui.View
	AppStatus   *gocui.View
	FilterPrefix *gocui.View
	Filter *gocui.View

	Confirmation *gocui.View
	Menu         *gocui.View

	Limit *gocui.View
}

type viewNameMapping struct {
	viewPtr **gocui.View
	name    string
	autoPosition bool
}

func (gui *Gui) orderedViewNameMappings() []viewNameMapping {
	return []viewNameMapping{
		{viewPtr: &gui.Views.Containers, name: "containers", autoPosition: true},
		{viewPtr: &gui.Views.Images, name: "images", autoPosition: true},
		{viewPtr: &gui.Views.Volumes, name: "volumes", autoPosition: true},
		{viewPtr: &gui.Views.Networks, name: "networks", autoPosition: true},

		{viewPtr: &gui.Views.Main, name: "main", autoPosition: true},

		{viewPtr: &gui.Views.Options, name: "options", autoPosition: true},
		{viewPtr: &gui.Views.AppStatus, name: "appStatus", autoPosition: true},
		{viewPtr: &gui.Views.Information, name: "information", autoPosition: true},
		{viewPtr: &gui.Views.Filter, name: "filter", autoPosition: true},
		{viewPtr: &gui.Views.FilterPrefix, name: "filterPrefix", autoPosition: true},

		{viewPtr: &gui.Views.Menu, name: "menu", autoPosition: false},
		{viewPtr: &gui.Views.Confirmation, name: "confirmation", autoPosition: false},

		{viewPtr: &gui.Views.Limit, name: "limit", autoPosition: true},
	}
}

func (gui *Gui) createAllViews() error {
	frameRunes := []rune{'─', '│', '╭', '╮', '╰', '╯'}
	switch gui.Config.UserConfig.Gui.Border {
	case "single":
		frameRunes = []rune{'─', '│', '┌', '┐', '└', '┘'}
	case "double":
		frameRunes = []rune{'═', '║', '╔', '╗', '╚', '╝'}
	case "hidden":
		frameRunes = []rune{' ', ' ', ' ', ' ', ' ', ' '}
	}

	var err error
	for _, mapping := range gui.orderedViewNameMappings() {
		*mapping.viewPtr, err = gui.prepareView(mapping.name)
		if err != nil && err.Error() != UNKNOWN_VIEW_ERROR_MSG {
			return err
		}
		(*mapping.viewPtr).FrameRunes = frameRunes
		(*mapping.viewPtr).FgColor = gocui.ColorDefault
	}

	selectedLineBgColor := GetGocuiStyle(gui.Config.UserConfig.Gui.Theme.SelectedLineBgColor)

	gui.Views.Main.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel
	gui.Views.Main.IgnoreCarriageReturns = true

	gui.Views.Containers.Highlight = true
	gui.Views.Containers.SelBgColor = selectedLineBgColor
	gui.Views.Containers.Title = gui.Tr.ContainersTitle
	gui.Views.Containers.TitlePrefix = "[1]"

	gui.Views.Images.Highlight = true
	gui.Views.Images.Title = gui.Tr.ImagesTitle
	gui.Views.Images.SelBgColor = selectedLineBgColor
	gui.Views.Images.TitlePrefix = "[2]"

	gui.Views.Volumes.Highlight = true
	gui.Views.Volumes.Title = gui.Tr.VolumesTitle
	gui.Views.Volumes.TitlePrefix = "[3]"
	gui.Views.Volumes.SelBgColor = selectedLineBgColor

	gui.Views.Networks.Highlight = true
	gui.Views.Networks.Title = gui.Tr.NetworksTitle
	gui.Views.Networks.TitlePrefix = "[4]"
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

func (gui *Gui) autoPositionedViewNames() []string {
	views := lo.Filter(gui.orderedViewNameMappings(), func(viewNameMapping viewNameMapping, _ int) bool {
		return viewNameMapping.autoPosition
	})

	return lo.Map(views, func(viewNameMapping viewNameMapping, _ int) string {
		return viewNameMapping.name
	})
}
