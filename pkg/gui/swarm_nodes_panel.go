package gui

import (
	"strconv"

	"github.com/docker/docker/api/types/swarm"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func (gui *Gui) getSwarmNodesPanel() *panels.SideListPanel[*commands.SwarmNode] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmNode]{
		view:    gui.Views.SwarmNodes,
		noItems: gui.Tr.SwarmNoNodes,
		getID:   func(n *commands.SwarmNode) string { return n.ID },
		cells:   presentation.GetSwarmNodeDisplayStrings,
		less:    func(a, b *commands.SwarmNode) bool { return a.Hostname() < b.Hostname() },
		tabs: []swarmTab[*commands.SwarmNode]{
			{key: "tasks", title: gui.Tr.SwarmTasksTitle, live: func(n *commands.SwarmNode) string { return swarmTasksTable(n.Tasks, false) }},
			{key: "info", title: gui.Tr.SwarmNodeInfoTitle, live: swarmNodeInfo},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(n *commands.SwarmNode) string { return swarmYaml(n.Node) }},
		},
	})
}

// swarmNodeInfo summarises the node's platform, resources and labels
func swarmNodeInfo(node *commands.SwarmNode) string {
	desc := node.Node.Description
	padding := 16
	output := ""
	output += utils.WithPadding("Address: ", padding) + node.Node.Status.Addr + "\n"
	output += utils.WithPadding("Platform: ", padding) + desc.Platform.OS + "/" + desc.Platform.Architecture + "\n"
	output += utils.WithPadding("CPUs: ", padding) + strconv.FormatFloat(float64(desc.Resources.NanoCPUs)/1e9, 'f', -1, 64) + "\n"
	output += utils.WithPadding("Memory: ", padding) + utils.FormatBinaryBytes(int(desc.Resources.MemoryBytes)) + "\n"
	if status := node.Node.ManagerStatus; status != nil {
		manager := string(status.Reachability) + ", " + status.Addr
		if status.Reachability != swarm.ReachabilityReachable {
			manager = utils.ColoredString(manager, color.FgRed)
		}
		output += utils.WithPadding("Manager: ", padding) + manager + "\n"
	}
	output += utils.WithPadding("Labels: ", padding) + utils.FormatMap(padding, node.Node.Spec.Labels)
	output += utils.WithPadding("Engine Labels: ", padding) + utils.FormatMap(padding, desc.Engine.Labels)
	return output
}

func (gui *Gui) handleSwarmNodeAvailabilityMenu(g *gocui.Gui, v *gocui.View) error {
	node, err := gui.Panels.SwarmNodes.GetSelectedItem()
	if err != nil {
		return nil
	}

	item := func(availability swarm.NodeAvailability, label string) *types.MenuItem {
		return &types.MenuItem{Label: label, OnPress: func() error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return node.SetAvailability(availability) })
		}}
	}

	return gui.Menu(CreateMenuOptions{
		Title: gui.Tr.SwarmSetAvailability,
		Items: []*types.MenuItem{
			item(swarm.NodeAvailabilityActive, gui.Tr.SwarmAvailabilityActive),
			item(swarm.NodeAvailabilityPause, gui.Tr.SwarmAvailabilityPause),
			item(swarm.NodeAvailabilityDrain, gui.Tr.SwarmAvailabilityDrain),
		},
	})
}

func (gui *Gui) handleSwarmNodeRoleMenu(g *gocui.Gui, v *gocui.View) error {
	node, err := gui.Panels.SwarmNodes.GetSelectedItem()
	if err != nil {
		return nil
	}

	item := func(role swarm.NodeRole, label string) *types.MenuItem {
		return &types.MenuItem{Label: label, OnPress: func() error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return node.SetRole(role) })
		}}
	}

	return gui.Menu(CreateMenuOptions{
		Title: gui.Tr.SwarmChangeRole,
		Items: []*types.MenuItem{
			item(swarm.NodeRoleManager, gui.Tr.SwarmPromote),
			item(swarm.NodeRoleWorker, gui.Tr.SwarmDemote),
		},
	})
}

func (gui *Gui) handleSwarmNodeRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	node, err := gui.Panels.SwarmNodes.GetSelectedItem()
	if err != nil {
		return nil
	}

	item := func(force bool, label string) *types.MenuItem {
		return &types.MenuItem{Label: label, OnPress: func() error {
			return gui.runSwarmAction(gui.Tr.RemovingStatus, func() error { return node.Remove(force) })
		}}
	}

	return gui.Menu(CreateMenuOptions{
		Title: gui.Tr.SwarmRemoveNode,
		Items: []*types.MenuItem{
			item(false, gui.Tr.Remove),
			item(true, gui.Tr.ForceRemove),
		},
	})
}
