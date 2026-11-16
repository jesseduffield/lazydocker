package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
)

func (gui *Gui) getSwarmNetworksPanel() *panels.SideListPanel[*commands.SwarmNetwork] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmNetwork]{
		view:    gui.Views.SwarmNetworks,
		noItems: gui.Tr.NoNetworks,
		getID:   func(n *commands.SwarmNetwork) string { return n.ID },
		cells:   presentation.GetSwarmNetworkDisplayStrings,
		less:    func(a, b *commands.SwarmNetwork) bool { return a.Name < b.Name },
		filter:  func(n *commands.SwarmNetwork) bool { return gui.isInSelectedSwarmStack(n.Stack) },
		tabs: []swarmTab[*commands.SwarmNetwork]{
			{key: "services", title: gui.Tr.ServicesTitle, live: func(n *commands.SwarmNetwork) string { return swarmServicesTable(n.UsedBy) }},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(n *commands.SwarmNetwork) string { return swarmYaml(n.Network) }},
		},
	})
}

func (gui *Gui) handleSwarmNetworkCreate(g *gocui.Gui, v *gocui.View) error {
	swarm := gui.State.Swarm
	if swarm == nil {
		return nil
	}

	return gui.createSwarmPrompt(gui.Tr.SwarmNamePrompt, "", func(name string) error {
		return gui.runSwarmAction(gui.Tr.SwarmCreatingStatus, func() error { return swarm.CreateNetwork(name) })
	})
}

func (gui *Gui) handleSwarmNetworkAttachMenu(g *gocui.Gui, v *gocui.View) error {
	network, err := gui.Panels.SwarmNetworks.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.swarmAttachMenu(network.Name, network.UsedBy, func(service *commands.SwarmService) error {
		return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return service.AttachNetwork(network) })
	})
}

func (gui *Gui) handleSwarmNetworkDetachMenu(g *gocui.Gui, v *gocui.View) error {
	network, err := gui.Panels.SwarmNetworks.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.swarmDetachMenu(network.Name, network.UsedBy, func(service *commands.SwarmService) error {
		return service.DetachNetwork(network)
	})
}

func (gui *Gui) handleSwarmNetworkRemove(g *gocui.Gui, v *gocui.View) error {
	network, err := gui.Panels.SwarmNetworks.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRemove, network.Name), gui.Tr.RemovingStatus, network.Remove)
}
