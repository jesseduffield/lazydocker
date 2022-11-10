package gui

import (
	"fmt"
	"strings"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

func (gui *Gui) getImagesPanel() *panels.SideListPanel[*commands.Image] {
	noneLabel := "<none>"

	return &panels.SideListPanel[*commands.Image]{
		ContextState: &panels.ContextState[*commands.Image]{
			GetMainTabs: func() []panels.MainTab[*commands.Image] {
				return []panels.MainTab[*commands.Image]{
					{
						Key:    "config",
						Title:  gui.Tr.ConfigTitle,
						Render: gui.renderImageConfigTask,
					},
				}
			},
			GetItemContextCacheKey: func(image *commands.Image) string {
				return "images-" + image.ID
			},
		},
		ListPanel: panels.ListPanel[*commands.Image]{
			List: panels.NewFilteredList[*commands.Image](),
			View: gui.Views.Images,
		},
		NoItemsMessage: gui.Tr.NoImages,
		Gui:            gui.intoInterface(),
		Sort: func(a *commands.Image, b *commands.Image) bool {
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
		GetTableCells: presentation.GetImageDisplayStrings,
	}
}

func (gui *Gui) renderImageConfigTask(image *commands.Image) tasks.TaskFunc {
	return gui.NewRenderStringTask(RenderStringTaskOpts{
		GetStrContent: func() string { return gui.imageConfigStr(image) },
		Autoscroll:    false,
		Wrap:          false, // don't care what your config is this page is ugly without wrapping
	})
}

func (gui *Gui) imageConfigStr(image *commands.Image) string {
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

	return output
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

func (gui *Gui) FilterString(view *gocui.View) string {
	if gui.State.Filter.panel != nil && gui.State.Filter.panel.GetView() != view {
		return ""
	}

	return gui.State.Filter.needle
}

func (gui *Gui) handleImagesRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	type removeImageOption struct {
		description   string
		command       string
		configOptions dockerTypes.ImageRemoveOptions
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
			configOptions: dockerTypes.ImageRemoveOptions{PruneChildren: true, Force: false},
		},
		{
			description:   gui.Tr.RemoveWithoutPrune,
			command:       "docker image rm --no-prune " + shortSha,
			configOptions: dockerTypes.ImageRemoveOptions{PruneChildren: false, Force: false},
		},
		{
			description:   gui.Tr.RemoveWithForce,
			command:       "docker image rm --force " + shortSha,
			configOptions: dockerTypes.ImageRemoveOptions{PruneChildren: true, Force: true},
		},
		{
			description:   gui.Tr.RemoveWithoutPruneWithForce,
			command:       "docker image rm --no-prune --force " + shortSha,
			configOptions: dockerTypes.ImageRemoveOptions{PruneChildren: false, Force: true},
		},
	}

	menuItems := lo.Map(options, func(option *removeImageOption, _ int) *types.MenuItem {
		return &types.MenuItem{
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
