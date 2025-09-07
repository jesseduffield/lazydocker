package gui

import (
	"fmt"
	"strings"

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

func (gui *Gui) getVolumesPanel() *panels.SideListPanel[*commands.Volume] {
	return &panels.SideListPanel[*commands.Volume]{
		Hide: func() bool {
			// Show volumes panel for both Docker and Apple runtime
			return false
		},
		ContextState: &panels.ContextState[*commands.Volume]{
			GetMainTabs: func() []panels.MainTab[*commands.Volume] {
				return []panels.MainTab[*commands.Volume]{
					{
						Key:    "config",
						Title:  gui.Tr.ConfigTitle,
						Render: gui.renderVolumeConfig,
					},
				}
			},
			GetItemContextCacheKey: func(volume *commands.Volume) string {
				return "volumes-" + volume.Name
			},
		},
		ListPanel: panels.ListPanel[*commands.Volume]{
			List: panels.NewFilteredList[*commands.Volume](),
			View: gui.Views.Volumes,
		},
		NoItemsMessage: gui.Tr.NoVolumes,
		Gui:            gui.intoInterface(),
		// we're sorting these volumes based on whether they have labels defined,
		// because those are the ones you typically care about.
		// Within that, we also sort them alphabetically
		Sort: func(a *commands.Volume, b *commands.Volume) bool {
			if len(a.Volume.Labels) == 0 && len(b.Volume.Labels) > 0 {
				return false
			}
			if len(a.Volume.Labels) > 0 && len(b.Volume.Labels) == 0 {
				return true
			}
			return a.Name < b.Name
		},
		GetTableCells: presentation.GetVolumeDisplayStrings,
	}
}

func (gui *Gui) renderVolumeConfig(volume *commands.Volume) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.volumeConfigStr(volume) })
}

func (gui *Gui) volumeConfigStr(volume *commands.Volume) string {
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

	return output
}

func (gui *Gui) reloadVolumes() error {
	if err := gui.refreshStateVolumes(); err != nil {
		return err
	}

	return gui.Panels.Volumes.RerenderList()
}

func (gui *Gui) refreshStateVolumes() error {
	volumes, err := gui.ContainerCommand.RefreshVolumes()
	if err != nil {
		return err
	}

	gui.Panels.Volumes.SetItems(volumes)

	return nil
}

func (gui *Gui) handleVolumesRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	volume, err := gui.Panels.Volumes.GetSelectedItem()
	if err != nil {
		return nil
	}

	type removeVolumeOption struct {
		description string
		command     string
		force       bool
	}

	runtimeName := gui.ContainerCommand.GetRuntimeName()
	rmCmd := utils.WithShortSha("docker volume rm " + volume.Name)
	rmForceCmd := utils.WithShortSha("docker volume rm --force " + volume.Name)
	if runtimeName == "apple" {
		rmCmd = utils.WithShortSha("container volume rm " + volume.Name)
		rmForceCmd = utils.WithShortSha("container volume rm --force " + volume.Name)
	}

	options := []*removeVolumeOption{
		{
			description: gui.Tr.Remove,
			command:     rmCmd,
			force:       false,
		},
		{
			description: gui.Tr.ForceRemove,
			command:     rmForceCmd,
			force:       true,
		},
	}

	menuItems := lo.Map(options, func(option *removeVolumeOption, _ int) *types.MenuItem {
		return &types.MenuItem{
			LabelColumns: []string{option.description, color.New(color.FgRed).Sprint(option.command)},
			OnPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
					if err := volume.Remove(option.force); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		}
	})

	return gui.Menu(CreateMenuOptions{
		Title: "",
		Items: menuItems,
	})
}

func (gui *Gui) handlePruneVolumes() error {
	if gui.ContainerCommand != nil && !gui.ContainerCommand.Supports(commands.FeatureVolumePrune) {
		return gui.createErrorPanel("Volume pruning is not supported by the current container runtime.")
	}
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmPruneVolumes, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.ContainerCommand.PruneVolumes()
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleCreateVolume(g *gocui.Gui, v *gocui.View) error {
	if gui.ContainerCommand != nil && !gui.ContainerCommand.Supports(commands.FeatureVolumeCreate) {
		return gui.createErrorPanel("Volume create is not supported by the current container runtime.")
	}
	prompt := "Enter: name [opt=value opt2=value2]"
	return gui.createPromptPanel("Create Volume", func(g *gocui.Gui, v *gocui.View) error {
		input := strings.TrimSpace(v.Buffer())
		_ = gui.closeConfirmationPrompt()
		if input == "" {
			return nil
		}
		fields := strings.Fields(input)
		name := fields[0]
		opts := map[string]string{}
		for _, tok := range fields[1:] {
			if kv := strings.SplitN(tok, "=", 2); len(kv) == 2 {
				opts[kv[0]] = kv[1]
			} else if tok != "" {
				opts[tok] = ""
			}
		}
		if err := gui.ContainerCommand.CreateVolume(name, opts); err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return gui.reloadVolumes()
	})
	// write prompt after view appears
	// we can't write synchronously because the view becomes editable after creation
	_ = prompt
	return nil
}

func (gui *Gui) handleVolumesCustomCommand(g *gocui.Gui, v *gocui.View) error {
	volume, err := gui.Panels.Volumes.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.ContainerCommand.NewCommandObject(commands.CommandObject{
		Volume: volume,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Volumes

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleVolumesBulkCommand(g *gocui.Gui, v *gocui.View) error {
	baseBulkCommands := []config.CustomCommand{}
	if gui.ContainerCommand == nil || gui.ContainerCommand.Supports(commands.FeatureVolumePrune) {
		baseBulkCommands = append(baseBulkCommands, config.CustomCommand{
			Name:             gui.Tr.PruneVolumes,
			InternalFunction: gui.handlePruneVolumes,
		})
	}

	bulkCommands := append(baseBulkCommands, gui.Config.UserConfig.BulkCommands.Volumes...)
	commandObject := gui.ContainerCommand.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}
