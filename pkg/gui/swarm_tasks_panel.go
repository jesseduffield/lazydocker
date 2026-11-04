package gui

import (
	"context"
	"io"
	"sort"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/samber/lo"
)

func (gui *Gui) getSwarmTasksPanel() *panels.SideListPanel[*commands.SwarmTask] {
	return newSwarmPanel(gui, swarmPanelOpts[*commands.SwarmTask]{
		view:    gui.Views.SwarmTasks,
		noItems: gui.Tr.SwarmNoTasks,
		getID:   func(t *commands.SwarmTask) string { return t.ID },
		cells:   presentation.GetSwarmTaskDisplayStrings,
		less:    swarmTaskLess,
		filter: func(t *commands.SwarmTask) bool {
			return (gui.State.ShowSwarmTaskHistory || t.IsCurrent()) && gui.isInSelectedSwarmStack(t.Stack)
		},
		tabs: []swarmTab[*commands.SwarmTask]{
			// a single task's lines don't need telling apart, hence no label
			{key: "logs", title: gui.Tr.LogsTitle, render: func(t *commands.SwarmTask) tasks.TaskFunc {
				return gui.renderSwarmLogs(t, nil)
			}},
			{key: "config", title: gui.Tr.ConfigTitle, live: func(t *commands.SwarmTask) string { return swarmYaml(t.Task) }},
		},
	})
}

func (gui *Gui) handleSwarmTaskViewLogs(g *gocui.Gui, v *gocui.View) error {
	task, err := gui.Panels.SwarmTasks.GetSelectedItem()
	if err != nil {
		return nil
	}

	gui.renderLogsToStdout(func(ctx context.Context, out io.Writer) error {
		return gui.writeSwarmLogs(ctx, out, task, nil)
	})

	return nil
}

func (gui *Gui) handleToggleSwarmTaskHistory(g *gocui.Gui, v *gocui.View) error {
	gui.State.ShowSwarmTaskHistory = !gui.State.ShowSwarmTaskHistory
	return gui.Panels.SwarmTasks.RerenderList()
}

// swarmTaskLess orders tasks like `docker service ps`: by service, then by
// slot (or by node, for global services which have one task per node), then
// most recent first, so that a task's history sits right below it
func swarmTaskLess(a, b *commands.SwarmTask) bool {
	if a.ServiceName != b.ServiceName {
		return a.ServiceName < b.ServiceName
	}
	if a.Task.Slot != b.Task.Slot {
		return a.Task.Slot < b.Task.Slot
	}
	if a.Task.Slot == 0 && a.Task.NodeID != b.Task.NodeID {
		return a.Task.NodeID < b.Task.NodeID
	}
	return a.Task.Status.Timestamp.After(b.Task.Status.Timestamp)
}

func swarmTasksTable(swarmTasks []*commands.SwarmTask, includeHistory bool) string {
	swarmTasks = lo.Filter(swarmTasks, func(t *commands.SwarmTask, _ int) bool { return includeHistory || t.IsCurrent() })
	sort.SliceStable(swarmTasks, func(i, j int) bool { return swarmTaskLess(swarmTasks[i], swarmTasks[j]) })
	return swarmTable(swarmTasks, presentation.GetSwarmTaskDisplayStrings)
}
