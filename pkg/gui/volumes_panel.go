package gui

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// list panel functions

func (gui *Gui) getVolumeContexts() []string {
	return []string{"config"}
}

func (gui *Gui) getVolumeContextTitles() []string {
	return []string{gui.Tr.ConfigTitle}
}

func (gui *Gui) getSelectedVolume() (*commands.Volume, error) {
	selectedLine := gui.State.Panels.Volumes.SelectedLine
	if selectedLine == -1 {
		return nil, gui.Errors.ErrNoVolumes
	}

	return gui.DockerCommand.Volumes[selectedLine], nil
}

func (gui *Gui) handleVolumesFocus(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	cx, cy := v.Cursor()
	_, oy := v.Origin()

	prevSelectedLine := gui.State.Panels.Volumes.SelectedLine
	newSelectedLine := cy - oy

	if newSelectedLine > len(gui.DockerCommand.Volumes)-1 || len(utils.Decolorise(gui.DockerCommand.Volumes[newSelectedLine].Name)) < cx {
		return gui.handleVolumeSelect(gui.g, v)
	}

	gui.State.Panels.Volumes.SelectedLine = newSelectedLine

	if !(prevSelectedLine == newSelectedLine && gui.currentViewName() == v.Name()) {
		return gui.handleVolumeSelect(gui.g, v)
	}

	return nil
}

func (gui *Gui) handleVolumeSelect(g *gocui.Gui, v *gocui.View) error {
	if _, err := gui.g.SetCurrentView(v.Name()); err != nil {
		return err
	}

	volume, err := gui.getSelectedVolume()
	if err != nil {
		if err != gui.Errors.ErrNoVolumes {
			return err
		}
		return gui.renderString(g, "main", gui.Tr.NoVolumes)
	}

	key := "volumes-" + volume.Name + "-" + gui.getVolumeContexts()[gui.State.Panels.Volumes.ContextIndex]
	if gui.State.Panels.Main.ObjectKey == key {
		return nil
	} else {
		gui.State.Panels.Main.ObjectKey = key
	}

	if err := gui.focusPoint(0, gui.State.Panels.Volumes.SelectedLine, len(gui.DockerCommand.Volumes), v); err != nil {
		return err
	}

	mainView := gui.getMainView()
	mainView.Tabs = gui.getVolumeContextTitles()
	mainView.TabIndex = gui.State.Panels.Volumes.ContextIndex

	switch gui.getVolumeContexts()[gui.State.Panels.Volumes.ContextIndex] {
	case "config":
		if err := gui.renderVolumeConfig(mainView, volume); err != nil {
			return err
		}
	default:
		return errors.New("Unknown context for Volumes panel")
	}

	return nil
}

func (gui *Gui) renderVolumeConfig(mainView *gocui.View, volume *commands.Volume) error {
	return gui.T.NewTask(func(stop chan struct{}) {
		mainView.Autoscroll = false
		mainView.Wrap = true

		padding := 15
		output := ""
		output += utils.WithPadding("Name: ", padding) + volume.Name + "\n"
		output += utils.WithPadding("Driver: ", padding) + volume.Volume.Driver + "\n"
		output += utils.WithPadding("Scope: ", padding) + volume.Volume.Scope + "\n"
		output += utils.WithPadding("Mountpoint: ", padding) + volume.Volume.Mountpoint + "\n"
		output += utils.WithPadding("Labels: ", padding)
		if len(volume.Volume.Labels) > 0 {
			output += "\n"
			for k, v := range volume.Volume.Labels {
				output += formatMapItem(padding, k, v)
			}
		} else {
			output += "none\n"
		}

		output += "\n" + utils.WithPadding("Options: ", padding)
		if len(volume.Volume.Options) > 0 {
			output += "\n"
			for k, v := range volume.Volume.Options {
				output += formatMapItem(padding, k, v)
			}
		} else {
			output += "none\n"
		}

		output += "\n" + utils.WithPadding("Status: ", padding)
		if volume.Volume.Status != nil {
			output += "\n"
			for k, v := range volume.Volume.Status {
				output += formatMapItem(padding, k, v)
			}
		} else {
			output += "n/a"
		}

		if volume.Volume.UsageData != nil {
			output += utils.WithPadding("RefCount: ", padding) + string(volume.Volume.UsageData.RefCount) + "\n"
			output += utils.WithPadding("Size: ", padding) + utils.FormatBinaryBytes(int(volume.Volume.UsageData.Size)) + "\n"
		}

		gui.renderString(gui.g, "main", output)
	})
}

func formatMapItem(padding int, k string, v interface{}) string {
	return fmt.Sprintf("%s%s %v\n", strings.Repeat(" ", padding), utils.ColoredString(k+":", color.FgYellow), utils.ColoredString(fmt.Sprintf("%v", v), color.FgMagenta))
}

func (gui *Gui) refreshVolumes() error {
	volumesView := gui.getVolumesView()
	if volumesView == nil {
		// if the volumesView hasn't been instantiated yet we just return
		return nil
	}
	if err := gui.DockerCommand.RefreshVolumes(); err != nil {
		return err
	}

	if len(gui.DockerCommand.Volumes) > 0 && gui.State.Panels.Volumes.SelectedLine == -1 {
		gui.State.Panels.Volumes.SelectedLine = 0
	}
	if len(gui.DockerCommand.Volumes)-1 < gui.State.Panels.Volumes.SelectedLine {
		gui.State.Panels.Volumes.SelectedLine = len(gui.DockerCommand.Volumes) - 1
	}

	gui.g.Update(func(g *gocui.Gui) error {
		volumesView.Clear()
		isFocused := gui.g.CurrentView().Name() == "volumes"
		list, err := utils.RenderList(gui.DockerCommand.Volumes, utils.IsFocused(isFocused))
		if err != nil {
			return err
		}
		fmt.Fprint(volumesView, list)

		if volumesView == g.CurrentView() {
			return gui.handleVolumeSelect(g, volumesView)
		}
		return nil
	})

	return nil
}

func (gui *Gui) handleVolumesNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Volumes
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.Volumes), false)

	return gui.handleVolumeSelect(gui.g, v)
}

func (gui *Gui) handleVolumesPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Volumes
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.Volumes), true)

	return gui.handleVolumeSelect(gui.g, v)
}

func (gui *Gui) handleVolumesNextContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getVolumeContexts()
	if gui.State.Panels.Volumes.ContextIndex >= len(contexts)-1 {
		gui.State.Panels.Volumes.ContextIndex = 0
	} else {
		gui.State.Panels.Volumes.ContextIndex++
	}

	gui.handleVolumeSelect(gui.g, v)

	return nil
}

func (gui *Gui) handleVolumesPrevContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getVolumeContexts()
	if gui.State.Panels.Volumes.ContextIndex <= 0 {
		gui.State.Panels.Volumes.ContextIndex = len(contexts) - 1
	} else {
		gui.State.Panels.Volumes.ContextIndex--
	}

	gui.handleVolumeSelect(gui.g, v)

	return nil
}

type removeVolumeOption struct {
	description string
	command     string
	force       bool
	runCommand  bool
}

// GetDisplayStrings is a function.
func (r *removeVolumeOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description, color.New(color.FgRed).Sprint(r.command)}
}

func (gui *Gui) handleVolumesRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	volume, err := gui.getSelectedVolume()
	if err != nil {
		return nil
	}

	options := []*removeVolumeOption{
		{
			description: gui.Tr.Remove,
			command:     "docker volume rm " + volume.Name,
			force:       false,
			runCommand:  true,
		},
		{
			description: gui.Tr.ForceRemove,
			command:     "docker volume rm --force " + volume.Name,
			force:       true,
			runCommand:  true,
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
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			if cerr := volume.Remove(options[index].force); cerr != nil {
				return gui.createErrorPanel(gui.g, cerr.Error())
			}
			return nil
		})
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handlePruneVolumes(g *gocui.Gui, v *gocui.View) error {
	return gui.createConfirmationPanel(gui.g, v, gui.Tr.Confirm, gui.Tr.ConfirmPruneVolumes, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.DockerCommand.PruneVolumes()
			if err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}
			return nil
		})
	}, nil)
}
