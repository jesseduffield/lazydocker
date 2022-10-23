package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

func (gui *Gui) getImagesPanel() *SideListPanel[*commands.Image] {
	noneLabel := "<none>"

	return &SideListPanel[*commands.Image]{
		contextKeyPrefix: "images",
		ListPanel: ListPanel[*commands.Image]{
			list: NewFilteredList[*commands.Image](),
			view: gui.Views.Images,
		},
		contextIdx:     0,
		noItemsMessage: gui.Tr.NoImages,
		gui:            gui.intoInterface(),
		getContexts: func() []ContextConfig[*commands.Image] {
			return []ContextConfig[*commands.Image]{
				{
					key:   "config",
					title: gui.Tr.ConfigTitle,
					render: func(image *commands.Image) error {
						return gui.renderImageConfig(image)
					},
				},
			}
		},
		getContextCacheKey: func(image *commands.Image) string {
			return image.ID
		},
		sort: func(a *commands.Image, b *commands.Image) bool {
			if a.Name == noneLabel && b.Name != noneLabel {
				return false
			}

			if a.Name != noneLabel && b.Name == noneLabel {
				return true
			}

			if a.Name != b.Name {
				return a.Name < b.Name
			}

			if a.Tag != b.Tag {
				return a.Tag < b.Tag
			}

			return a.ID < b.ID
		},
		getDisplayStrings: func(image *commands.Image) []string {
			return []string{image.Name, image.Tag, utils.FormatDecimalBytes(int(image.Image.Size))}
		},
	}
}

func (gui *Gui) renderImageConfig(image *commands.Image) error {
	return gui.T.NewTask(func(stop chan struct{}) {
		padding := 10
		output := ""
		output += utils.WithPadding("Name: ", padding) + image.Name + "\n"
		output += utils.WithPadding("ID: ", padding) + image.Image.ID + "\n"
		output += utils.WithPadding("Tags: ", padding) + utils.ColoredString(strings.Join(image.Image.RepoTags, ", "), color.FgGreen) + "\n"
		output += utils.WithPadding("Size: ", padding) + utils.FormatDecimalBytes(int(image.Image.Size)) + "\n"
		output += utils.WithPadding("Created: ", padding) + fmt.Sprintf("%v", time.Unix(image.Image.Created, 0).Format(time.RFC1123)) + "\n"

		history, err := image.RenderHistory()
		if err != nil {
			gui.Log.Error(err)
		}

		output += "\n\n" + history

		mainView := gui.Views.Main
		mainView.Autoscroll = false
		mainView.Wrap = false // don't care what your config is this page is ugly without wrapping

		_ = gui.renderStringMain(output)
	})
}

func (gui *Gui) reloadImages() error {
	if err := gui.refreshStateImages(); err != nil {
		return err
	}

	return gui.Panels.Images.RerenderList()
}

func (gui *Gui) refreshStateImages() error {
	images, err := gui.DockerCommand.RefreshImages()
	if err != nil {
		return err
	}

	gui.Panels.Images.SetItems(images)

	return nil
}

func (gui *Gui) filterString(view *gocui.View) string {
	if gui.State.Filter.panel != nil && gui.State.Filter.panel.View() != view {
		return ""
	}

	return gui.State.Filter.needle
}

// TODO: merge into the above
func (gui *Gui) FilterString(view *gocui.View) string {
	return gui.filterString(view)
}

func (gui *Gui) handleImagesRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	type removeImageOption struct {
		description   string
		command       string
		configOptions types.ImageRemoveOptions
	}

	image, err := gui.Panels.Images.GetSelectedItem()
	if err != nil {
		return nil
	}

	shortSha := image.ID[7:17]

	// TODO: have a way of toggling in a menu instead of showing each permutation as a separate menu item
	options := []*removeImageOption{
		{
			description:   gui.Tr.Remove,
			command:       "docker image rm " + shortSha,
			configOptions: types.ImageRemoveOptions{PruneChildren: true, Force: false},
		},
		{
			description:   gui.Tr.RemoveWithoutPrune,
			command:       "docker image rm --no-prune " + shortSha,
			configOptions: types.ImageRemoveOptions{PruneChildren: false, Force: false},
		},
		{
			description:   gui.Tr.RemoveWithForce,
			command:       "docker image rm --force " + shortSha,
			configOptions: types.ImageRemoveOptions{PruneChildren: true, Force: true},
		},
		{
			description:   gui.Tr.RemoveWithoutPruneWithForce,
			command:       "docker image rm --no-prune --force " + shortSha,
			configOptions: types.ImageRemoveOptions{PruneChildren: false, Force: true},
		},
	}

	menuItems := lo.Map(options, func(option *removeImageOption, _ int) *MenuItem {
		return &MenuItem{
			LabelColumns: []string{
				option.description,
				color.New(color.FgRed).Sprint(option.command),
			},
			OnPress: func() error {
				if err := image.Remove(option.configOptions); err != nil {
					return gui.createErrorPanel(err.Error())
				}

				return nil
			},
		}
	})

	return gui.Menu(CreateMenuOptions{
		Title: "",
		Items: menuItems,
	})
}

func (gui *Gui) handlePruneImages() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmPruneImages, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.DockerCommand.PruneImages()
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return gui.reloadImages()
		})
	}, nil)
}

func (gui *Gui) handleImagesCustomCommand(g *gocui.Gui, v *gocui.View) error {
	image, err := gui.Panels.Images.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{
		Image: image,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Images

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleImagesBulkCommand(g *gocui.Gui, v *gocui.View) error {
	baseBulkCommands := []config.CustomCommand{
		{
			Name:             gui.Tr.PruneImages,
			InternalFunction: gui.handlePruneImages,
		},
	}

	bulkCommands := append(baseBulkCommands, gui.Config.UserConfig.BulkCommands.Images...)
	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}
