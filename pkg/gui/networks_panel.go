package gui

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/jesseduffield/lazycontainer/pkg/config"
	"github.com/jesseduffield/lazycontainer/pkg/gui/panels"
	"github.com/jesseduffield/lazycontainer/pkg/gui/presentation"
	"github.com/jesseduffield/lazycontainer/pkg/gui/types"
	"github.com/jesseduffield/lazycontainer/pkg/tasks"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
	"github.com/samber/lo"
)

func (gui *Gui) getNetworksPanel() *panels.SideListPanel[*commands.Network] {
	return &panels.SideListPanel[*commands.Network]{
		ContextState: &panels.ContextState[*commands.Network]{
			GetMainTabs: func() []panels.MainTab[*commands.Network] {
				return []panels.MainTab[*commands.Network]{
					{
						Key:    "config",
						Title:  gui.Tr.ConfigTitle,
						Render: gui.renderNetworkConfig,
					},
				}
			},
			GetItemContextCacheKey: func(network *commands.Network) string {
				return "networks-" + network.Name
			},
		},
		ListPanel: panels.ListPanel[*commands.Network]{
			List: panels.NewFilteredList[*commands.Network](),
			View: gui.Views.Networks,
		},
		NoItemsMessage: gui.Tr.NoNetworks,
		Gui:            gui.intoInterface(),
		Sort: func(a *commands.Network, b *commands.Network) bool {
			return a.Name < b.Name
		},
		GetTableCells: presentation.GetNetworkDisplayStrings,
	}
}

func (gui *Gui) renderNetworkConfig(network *commands.Network) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.networkConfigStr(network) })
}

func (gui *Gui) networkConfigStr(network *commands.Network) string {
	padding := 15
	output := ""
	output += utils.WithPadding("ID: ", padding) + network.AppleNetwork.ID + "\n"
	output += utils.WithPadding("Name: ", padding) + network.Name + "\n"
	output += utils.WithPadding("Mode: ", padding) + network.AppleNetwork.Config.Mode + "\n"
	output += utils.WithPadding("State: ", padding) + network.AppleNetwork.State + "\n"
	output += utils.WithPadding("IPv4 Subnet: ", padding) + network.AppleNetwork.Status.IPv4Subnet + "\n"
	output += utils.WithPadding("IPv4 Gateway: ", padding) + network.AppleNetwork.Status.IPv4Gateway + "\n"
	output += "\n"
	output += utils.WithPadding("Labels: ", padding) + utils.FormatMap(padding, network.AppleNetwork.Config.Labels)

	return output
}

func (gui *Gui) reloadNetworks() error {
	if err := gui.refreshStateNetworks(); err != nil {
		return err
	}

	return gui.Panels.Networks.RerenderList()
}

func (gui *Gui) refreshStateNetworks() error {
	networks, err := gui.ContainerCmd.RefreshNetworks()
	if err != nil {
		return err
	}

	gui.Panels.Networks.SetItems(networks)

	return nil
}

func (gui *Gui) handleNetworksRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	network, err := gui.Panels.Networks.GetSelectedItem()
	if err != nil {
		return nil
	}

	type removeNetworkOption struct {
		description string
		command     string
	}

	options := []*removeNetworkOption{
		{
			description: gui.Tr.Remove,
			command:     utils.WithShortSha("container network rm " + network.Name),
		},
	}

	menuItems := lo.Map(options, func(option *removeNetworkOption, _ int) *types.MenuItem {
		return &types.MenuItem{
			LabelColumns: []string{option.description, color.New(color.FgRed).Sprint(option.command)},
			OnPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
					if err := network.Remove(); err != nil {
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

func (gui *Gui) handlePruneNetworks() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmPruneNetworks, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.ContainerCmd.PruneNetworks()
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleNetworksCustomCommand(g *gocui.Gui, v *gocui.View) error {
	network, err := gui.Panels.Networks.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.ContainerCmd.Client.NewCommandObject(commands.CommandObject{
		Network: network,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Networks

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleNetworksBulkCommand(g *gocui.Gui, v *gocui.View) error {
	baseBulkCommands := []config.CustomCommand{
		{
			Name:             gui.Tr.PruneNetworks,
			InternalFunction: gui.handlePruneNetworks,
		},
	}

	bulkCommands := append(baseBulkCommands, gui.Config.UserConfig.BulkCommands.Networks...)
	commandObject := gui.ContainerCmd.Client.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}
