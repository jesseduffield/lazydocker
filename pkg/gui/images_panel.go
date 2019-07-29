package gui

import (
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// list panel functions

func (gui *Gui) getImageContexts() []string {
	return []string{"config"}
}

func (gui *Gui) getImageContextTitles() []string {
	return []string{gui.Tr.ConfigTitle}
}

func (gui *Gui) getSelectedImage() (*commands.Image, error) {
	selectedLine := gui.State.Panels.Images.SelectedLine
	if selectedLine == -1 {
		return &commands.Image{}, gui.Errors.ErrNoImages
	}

	return gui.DockerCommand.Images[selectedLine], nil
}

func (gui *Gui) handleImagesClick(g *gocui.Gui, v *gocui.View) error {
	itemCount := len(gui.DockerCommand.Images)
	handleSelect := gui.handleImageSelect
	selectedLine := &gui.State.Panels.Images.SelectedLine

	return gui.handleClick(v, itemCount, selectedLine, handleSelect)
}

func (gui *Gui) handleImageSelect(g *gocui.Gui, v *gocui.View) error {
	Image, err := gui.getSelectedImage()
	if err != nil {
		if err != gui.Errors.ErrNoImages {
			return err
		}
		return gui.renderString(g, "main", gui.Tr.NoImages)
	}

	if err := gui.focusPoint(0, gui.State.Panels.Images.SelectedLine, len(gui.DockerCommand.Images), v); err != nil {
		return err
	}

	key := "images-" + Image.ID + "-" + gui.getImageContexts()[gui.State.Panels.Images.ContextIndex]
	if !gui.shouldRefresh(key) {
		return nil
	}

	mainView := gui.getMainView()
	mainView.Tabs = gui.getImageContextTitles()
	mainView.TabIndex = gui.State.Panels.Images.ContextIndex

	switch gui.getImageContexts()[gui.State.Panels.Images.ContextIndex] {
	case "config":
		if err := gui.renderImageConfig(mainView, Image); err != nil {
			return err
		}
	default:
		return errors.New("Unknown context for Images panel")
	}

	return nil
}

func (gui *Gui) renderImageConfig(mainView *gocui.View, image *commands.Image) error {
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

		mainView.Autoscroll = false
		mainView.Wrap = false // don't care what your config is this page is ugly without wrapping

		gui.renderString(gui.g, "main", output)
	})
}

func (gui *Gui) refreshImages() error {
	ImagesView := gui.getImagesView()
	if ImagesView == nil {
		// if the ImagesView hasn't been instantiated yet we just return
		return nil
	}
	if err := gui.refreshStateImages(); err != nil {
		return err
	}

	if len(gui.DockerCommand.Images) > 0 && gui.State.Panels.Images.SelectedLine == -1 {
		gui.State.Panels.Images.SelectedLine = 0
	}
	if len(gui.DockerCommand.Images)-1 < gui.State.Panels.Images.SelectedLine {
		gui.State.Panels.Images.SelectedLine = len(gui.DockerCommand.Images) - 1
	}

	gui.g.Update(func(g *gocui.Gui) error {

		ImagesView.Clear()
		isFocused := gui.g.CurrentView().Name() == "Images"
		list, err := utils.RenderList(gui.DockerCommand.Images, utils.IsFocused(isFocused))
		if err != nil {
			return err
		}
		fmt.Fprint(ImagesView, list)

		if ImagesView == g.CurrentView() {
			return gui.handleImageSelect(g, ImagesView)
		}
		return nil
	})

	return nil
}

// TODO: leave this to DockerCommand
func (gui *Gui) refreshStateImages() error {
	Images, err := gui.DockerCommand.RefreshImages()
	if err != nil {
		return err
	}

	gui.DockerCommand.Images = Images

	return nil
}

func (gui *Gui) handleImagesNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() || gui.g.CurrentView() != v {
		return nil
	}

	panelState := gui.State.Panels.Images
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.Images), false)

	return gui.handleImageSelect(gui.g, v)
}

func (gui *Gui) handleImagesPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() || gui.g.CurrentView() != v {
		return nil
	}

	panelState := gui.State.Panels.Images
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.Images), true)

	return gui.handleImageSelect(gui.g, v)
}

func (gui *Gui) handleImagesNextContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getImageContexts()
	if gui.State.Panels.Images.ContextIndex >= len(contexts)-1 {
		gui.State.Panels.Images.ContextIndex = 0
	} else {
		gui.State.Panels.Images.ContextIndex++
	}

	gui.handleImageSelect(gui.g, v)

	return nil
}

func (gui *Gui) handleImagesPrevContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getImageContexts()
	if gui.State.Panels.Images.ContextIndex <= 0 {
		gui.State.Panels.Images.ContextIndex = len(contexts) - 1
	} else {
		gui.State.Panels.Images.ContextIndex--
	}

	gui.handleImageSelect(gui.g, v)

	return nil
}

type removeImageOption struct {
	description   string
	command       string
	configOptions types.ImageRemoveOptions
	runCommand    bool
}

// GetDisplayStrings is a function.
func (r *removeImageOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description, color.New(color.FgRed).Sprint(r.command)}
}

func (gui *Gui) handleImagesRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	Image, err := gui.getSelectedImage()
	if err != nil {
		return nil
	}

	shortSha := Image.ID[7:17]

	options := []*removeImageOption{
		{
			description:   gui.Tr.Remove,
			command:       "docker image rm " + shortSha,
			configOptions: types.ImageRemoveOptions{PruneChildren: true},
			runCommand:    true,
		},
		{
			description:   gui.Tr.RemoveWithoutPrune,
			command:       "docker image rm --no-prune " + shortSha,
			configOptions: types.ImageRemoveOptions{PruneChildren: false},
			runCommand:    true,
		},
		{
			description: gui.Tr.Cancel,
			runCommand:  false,
		},
	}

	handleMenuPress := func(index int) error {
		if !options[index].runCommand {
			return nil
		}
		configOptions := options[index].configOptions
		if cerr := Image.Remove(configOptions); cerr != nil {
			return gui.createErrorPanel(gui.g, cerr.Error())
		}

		return gui.refreshImages()
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handlePruneImages() error {
	return gui.createConfirmationPanel(gui.g, gui.getImagesView(), gui.Tr.Confirm, gui.Tr.ConfirmPruneImages, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.DockerCommand.PruneImages()
			if err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}
			return gui.refreshImages()
		})
	}, nil)
}

func (gui *Gui) handleImagesCustomCommand(g *gocui.Gui, v *gocui.View) error {
	image, err := gui.getSelectedImage()
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
