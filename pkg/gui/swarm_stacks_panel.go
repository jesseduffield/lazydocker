package gui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/samber/lo"
)

func (gui *Gui) getSwarmStacksPanel() *panels.SideListPanel[*commands.SwarmStack] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmStack]{
		view:    gui.Views.SwarmStacks,
		noItems: gui.Tr.SwarmNoStacks,
		getID:   func(s *commands.SwarmStack) string { return s.Name },
		cells:   presentation.GetSwarmStackDisplayStrings,
		less:    func(a, b *commands.SwarmStack) bool { return a.Name < b.Name },
		onSelect: func(*commands.SwarmStack) error {
			// like a compose project, the selected stack filters these panels
			for _, panel := range []panels.ISideListPanel{gui.Panels.SwarmServices, gui.Panels.SwarmTasks, gui.Panels.SwarmVolumes, gui.Panels.SwarmNetworks} {
				if err := panel.RerenderList(); err != nil {
					return err
				}
			}
			return nil
		},
		tabs: []swarmTab[*commands.SwarmStack]{
			{key: "services", title: gui.Tr.ServicesTitle, live: func(s *commands.SwarmStack) string { return swarmServicesTable(s.Services) }},
			{key: "tasks", title: gui.Tr.SwarmTasksTitle, live: func(s *commands.SwarmStack) string {
				return swarmTasksTable(lo.FlatMap(s.Services, func(s *commands.SwarmService, _ int) []*commands.SwarmTask { return s.Tasks }), false)
			}},
		},
	})
}

// stacks are deployed with the docker CLI, which parses the compose file.
// --with-registry-auth sends the local registry credentials along, so that
// the nodes can pull private images.
func (gui *Gui) handleSwarmStackDeploy(g *gocui.Gui, v *gocui.View) error {
	swarm := gui.State.Swarm
	if swarm == nil {
		return nil
	}

	defaultName := filepath.Base(gui.Config.ProjectDir)
	if stack := gui.selectedSwarmStack(); stack != nil && !stack.IsAll() {
		defaultName = stack.Name
	}

	return gui.createSwarmPrompt(gui.Tr.SwarmDeployFilePrompt, "docker-compose.yml", func(composeFiles string) error {
		return gui.createSwarmPrompt(gui.Tr.SwarmDeployNamePrompt, defaultName, func(name string) error {
			args := []string{"stack", "deploy", "--with-registry-auth"}
			for _, composeFile := range strings.Split(composeFiles, ",") {
				args = append(args, "--compose-file", expandHomeDir(strings.TrimSpace(composeFile)))
			}
			return gui.runSubprocess(swarm.NewCmd(append(args, name)...))
		})
	})
}

func (gui *Gui) handleSwarmStackStop(g *gocui.Gui, v *gocui.View) error {
	stack, err := gui.Panels.SwarmStacks.GetSelectedItem()
	if err != nil {
		return nil
	}

	if stack.IsAll() {
		return gui.createErrorPanel(gui.Tr.SwarmSelectStack)
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmStopStack, stack.Name), gui.Tr.StoppingStatus, stack.Stop)
}

func (gui *Gui) handleSwarmStackStart(g *gocui.Gui, v *gocui.View) error {
	stack, err := gui.Panels.SwarmStacks.GetSelectedItem()
	if err != nil {
		return nil
	}

	if stack.IsAll() {
		return gui.createErrorPanel(gui.Tr.SwarmSelectStack)
	}

	return gui.runSwarmAction(gui.Tr.StartingStatus, stack.Start)
}

func (gui *Gui) handleSwarmStackRemove(g *gocui.Gui, v *gocui.View) error {
	stack, err := gui.Panels.SwarmStacks.GetSelectedItem()
	if err != nil {
		return nil
	}

	if stack.IsAll() {
		return gui.createErrorPanel(gui.Tr.SwarmSelectStack)
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRemoveStack, stack.Name), gui.Tr.RemovingStatus, stack.Remove)
}
