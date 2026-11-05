package gui

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
)

func (gui *Gui) getSwarmServicesPanel() *panels.SideListPanel[*commands.SwarmService] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmService]{
		view:    gui.Views.SwarmServices,
		noItems: gui.Tr.NoServices,
		getID:   func(s *commands.SwarmService) string { return s.ID },
		cells:   presentation.GetSwarmServiceDisplayStrings,
		less:    func(a, b *commands.SwarmService) bool { return a.Name < b.Name },
		filter:  func(s *commands.SwarmService) bool { return gui.isInSelectedSwarmStack(s.Stack) },
		tabs: []swarmTab[*commands.SwarmService]{
			{key: "logs", title: gui.Tr.LogsTitle, render: func(s *commands.SwarmService) tasks.TaskFunc {
				return gui.renderSwarmLogs(s, gui.swarmLogLabel)
			}},
			{key: "tasks", title: gui.Tr.SwarmTasksTitle, live: func(s *commands.SwarmService) string {
				return swarmTasksTable(s.Tasks, true)
			}},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(s *commands.SwarmService) string { return swarmYaml(s.Service) }},
		},
	})
}

func (gui *Gui) handleSwarmServiceViewLogs(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	gui.renderLogsToStdout(func(ctx context.Context, out io.Writer) error {
		return gui.writeSwarmLogs(ctx, out, service, gui.swarmLogLabel)
	})

	return nil
}

func (gui *Gui) handleSwarmServiceStop(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmStopService, service.Name), gui.Tr.StoppingStatus, service.Stop)
}

func (gui *Gui) handleSwarmServiceStart(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.runSwarmAction(gui.Tr.StartingStatus, service.Start)
}

func (gui *Gui) handleSwarmServiceScale(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	_, desired := service.Replicas()
	return gui.createSwarmPrompt(fmt.Sprintf(gui.Tr.SwarmScalePrompt, service.Name), strconv.FormatUint(desired, 10), func(input string) error {
		replicas, err := strconv.ParseUint(input, 10, 64)
		if err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return service.Scale(replicas) })
	})
}

func (gui *Gui) handleSwarmServiceRestart(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRestartService, service.Name), gui.Tr.RestartingStatus, service.ForceUpdate)
}

func (gui *Gui) handleSwarmServiceUpdateImage(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.createSwarmPrompt(fmt.Sprintf(gui.Tr.SwarmUpdateImagePrompt, service.Name), service.Image(), func(image string) error {
		return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return service.UpdateImage(image) })
	})
}

func (gui *Gui) handleSwarmServiceRollback(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRollbackService, service.Name), gui.Tr.SwarmRollingBackStatus, service.Rollback)
}

func (gui *Gui) handleSwarmServiceRemove(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.SwarmServices.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.confirmSwarmAction(fmt.Sprintf(gui.Tr.SwarmConfirmRemoveService, service.Name), gui.Tr.RemovingStatus, service.Remove)
}

func swarmServicesTable(services []*commands.SwarmService) string {
	return swarmTable(services, presentation.GetSwarmServiceDisplayStrings)
}
