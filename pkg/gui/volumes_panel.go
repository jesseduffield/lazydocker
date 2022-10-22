package gui

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func (gui *Gui) getVolumesPanel() *SideListPanel[*commands.Volume] {
	return &SideListPanel[*commands.Volume]{
		contextKeyPrefix: "volumes",
		ListPanel: ListPanel[*commands.Volume]{
			list: NewFilteredList[*commands.Volume](),
			view: gui.Views.Volumes,
		},
		contextIdx:    0,
		noItemsMessge: gui.Tr.NoVolumes,
		gui:           gui.intoInterface(),
		getContexts: func() []ContextConfig[*commands.Volume] {
			return []ContextConfig[*commands.Volume]{
				{
					key:    "config",
					title:  gui.Tr.ConfigTitle,
					render: gui.renderVolumeConfig,
				},
			}
		},
		getSearchStrings: func(volume *commands.Volume) []string {
			// TODO: think about more things to search on
			return []string{volume.Name}
		},
		getContextCacheKey: func(volume *commands.Volume) string {
			return volume.Name
		},
		// we're sorting these volumes based on whether they have labels defined,
		// because those are the ones you typically care about.
		// Within that, we also sort them alphabetically
		sort: func(a *commands.Volume, b *commands.Volume) bool {
			if len(a.Volume.Labels) == 0 && len(b.Volume.Labels) > 0 {
				return false
			}
			if len(a.Volume.Labels) > 0 && len(b.Volume.Labels) == 0 {
				return true
			}
			return a.Name < b.Name
		},
	}
}

func (gui *Gui) renderVolumeConfig(volume *commands.Volume) error {
	return gui.T.NewTask(func(stop chan struct{}) {
		mainView := gui.Views.Main
		mainView.Autoscroll = false
		mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

		padding := 15
		output := ""
		output += utils.WithPadding("Name: ", padding) + volume.Name + "\n"
		output += utils.WithPadding("Driver: ", padding) + volume.Volume.Driver + "\n"
		output += utils.WithPadding("Scope: ", padding) + volume.Volume.Scope + "\n"
		output += utils.WithPadding("Mountpoint: ", padding) + volume.Volume.Mountpoint + "\n"
		output += utils.WithPadding("Labels: ", padding) + utils.FormatMap(padding, volume.Volume.Labels) + "\n"
		output += utils.WithPadding("Options: ", padding) + utils.FormatMap(padding, volume.Volume.Options) + "\n"

		output += utils.WithPadding("Status: ", padding)
		if volume.Volume.Status != nil {
			output += "\n"
			for k, v := range volume.Volume.Status {
				output += utils.FormatMapItem(padding, k, v)
			}
		} else {
			output += "n/a"
		}

		if volume.Volume.UsageData != nil {
			output += utils.WithPadding("RefCount: ", padding) + fmt.Sprintf("%d", volume.Volume.UsageData.RefCount) + "\n"
			output += utils.WithPadding("Size: ", padding) + utils.FormatBinaryBytes(int(volume.Volume.UsageData.Size)) + "\n"
		}

		_ = gui.renderStringMain(output)
	})
}

func (gui *Gui) reloadVolumes() error {
	if err := gui.refreshStateVolumes(); err != nil {
		return err
	}

	return gui.Panels.Volumes.RerenderList()
}

func (gui *Gui) refreshStateVolumes() error {
	volumes, err := gui.DockerCommand.RefreshVolumes()
	if err != nil {
		return err
	}

	gui.Panels.Volumes.SetItems(volumes)

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
	volume, err := gui.Panels.Volumes.GetSelectedItem()
	if err != nil {
		return nil
	}

	options := []*removeVolumeOption{
		{
			description: gui.Tr.Remove,
			command:     utils.WithShortSha("docker volume rm " + volume.Name),
			force:       false,
			runCommand:  true,
		},
		{
			description: gui.Tr.ForceRemove,
			command:     utils.WithShortSha("docker volume rm --force " + volume.Name),
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
				return gui.createErrorPanel(cerr.Error())
			}
			return nil
		})
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handlePruneVolumes() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmPruneVolumes, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.DockerCommand.PruneVolumes()
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleVolumesCustomCommand(g *gocui.Gui, v *gocui.View) error {
	volume, err := gui.Panels.Volumes.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{
		Volume: volume,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Volumes

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleVolumesBulkCommand(g *gocui.Gui, v *gocui.View) error {
	baseBulkCommands := []config.CustomCommand{
		{
			Name:             gui.Tr.PruneVolumes,
			InternalFunction: gui.handlePruneVolumes,
		},
	}

	bulkCommands := append(baseBulkCommands, gui.Config.UserConfig.BulkCommands.Volumes...)
	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}
