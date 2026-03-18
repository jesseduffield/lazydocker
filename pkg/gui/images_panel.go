package gui

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/jesseduffield/lazycontainer/pkg/gui/panels"
	"github.com/jesseduffield/lazycontainer/pkg/gui/presentation"
	"github.com/jesseduffield/lazycontainer/pkg/gui/types"
	"github.com/jesseduffield/lazycontainer/pkg/tasks"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
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
		Sort: func(a, b *commands.Image) bool {
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
		Wrap:          false,
	})
}

func (gui *Gui) imageConfigStr(image *commands.Image) string {
	padding := 10
	output := ""
	output += utils.WithPadding("Name: ", padding) + image.Name + "\n"
	output += utils.WithPadding("Tag: ", padding) + image.Tag + "\n"
	output += utils.WithPadding("ID: ", padding) + image.ID + "\n"
	output += utils.WithPadding("Reference: ", padding) + image.AppleImage.Reference + "\n"
	output += utils.WithPadding("Digest: ", padding) + image.AppleImage.Descriptor.Digest + "\n"
	output += utils.WithPadding("Size: ", padding) + image.AppleImage.FullSize + "\n"
	return output
}

func (gui *Gui) reloadImages() error {
	if err := gui.refreshStateImages(); err != nil {
		return err
	}
	return gui.Panels.Images.RerenderList()
}

func (gui *Gui) refreshStateImages() error {
	images, err := gui.ContainerCmd.RefreshImages()
	if err != nil {
		return err
	}
	gui.Panels.Images.SetItems(images)
	return nil
}

func (gui *Gui) handleImagesRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	img, err := gui.Panels.Images.GetSelectedItem()
	if err != nil {
		return nil
	}

	shortSha := img.ID
	if len(img.ID) > 12 {
		shortSha = img.ID[:12]
	}

	options := []struct {
		description string
		command     string
		force       bool
	}{
		{description: gui.Tr.Remove, command: "container image rm " + shortSha, force: false},
		{description: gui.Tr.RemoveWithForce, command: "container image rm --force " + shortSha, force: true},
	}

	menuItems := make([]*types.MenuItem, len(options))
	for i, opt := range options {
		opt := opt
		menuItems[i] = &types.MenuItem{
			LabelColumns: []string{opt.description, color.New(color.FgRed).Sprint(opt.command)},
			OnPress: func() error {
				if err := img.Remove(opt.force); err != nil {
					return gui.createErrorPanel(err.Error())
				}
				return nil
			},
		}
	}

	return gui.Menu(CreateMenuOptions{
		Title: "",
		Items: menuItems,
	})
}

func (gui *Gui) handlePruneImages(all bool) error {
	title := gui.Tr.ConfirmPruneImages
	return gui.createConfirmationPanel(gui.Tr.Confirm, title, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.ContainerCmd.Client.PruneImages(all)
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return gui.reloadImages()
		})
	}, nil)
}

func (gui *Gui) handleImagesCustomCommand(g *gocui.Gui, v *gocui.View) error {
	img, err := gui.Panels.Images.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.ContainerCmd.NewCommandObject(commands.CommandObject{Image: img})
	customCommands := gui.Config.UserConfig.CustomCommands.Images
	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleImagesBulkCommand(g *gocui.Gui, v *gocui.View) error {
	bulkCommands := gui.Config.UserConfig.BulkCommands.Images
	commandObject := gui.ContainerCmd.NewCommandObject(commands.CommandObject{})
	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}
