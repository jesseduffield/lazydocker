package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/jesseduffield/lazycontainer/pkg/gui/panels"
	"github.com/jesseduffield/lazycontainer/pkg/gui/presentation"
	"github.com/jesseduffield/lazycontainer/pkg/tasks"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
)

func (gui *Gui) getContainersPanel() *panels.SideListPanel[*commands.Container] {
	return &panels.SideListPanel[*commands.Container]{
		ContextState: &panels.ContextState[*commands.Container]{
			GetMainTabs: func() []panels.MainTab[*commands.Container] {
				return []panels.MainTab[*commands.Container]{
					{
						Key:    "logs",
						Title:  gui.Tr.LogsTitle,
						Render: gui.renderContainerLogsToMain,
					},
					{
						Key:    "stats",
						Title:  gui.Tr.StatsTitle,
						Render: gui.renderContainerStats,
					},
					{
						Key:    "env",
						Title:  gui.Tr.EnvTitle,
						Render: gui.renderContainerEnv,
					},
					{
						Key:    "config",
						Title:  gui.Tr.ConfigTitle,
						Render: gui.renderContainerConfig,
					},
					{
						Key:    "top",
						Title:  gui.Tr.TopTitle,
						Render: gui.renderContainerTop,
					},
				}
			},
			GetItemContextCacheKey: func(container *commands.Container) string {
				return "containers-" + container.ID + "-" + container.GetStatus()
			},
		},
		ListPanel: panels.ListPanel[*commands.Container]{
			List: panels.NewFilteredList[*commands.Container](),
			View: gui.Views.Containers,
		},
		NoItemsMessage: gui.Tr.NoContainers,
		Gui:            gui.intoInterface(),
		Sort: func(a *commands.Container, b *commands.Container) bool {
			return sortContainers(a, b, gui.Config.UserConfig.Gui.LegacySortContainers)
		},
		Filter: func(container *commands.Container) bool {
			if !gui.State.ShowExitedContainers && container.GetStatus() != "running" {
				return false
			}
			return true
		},
		GetTableCells: func(container *commands.Container) []string {
			return presentation.GetContainerDisplayStrings(&gui.Config.UserConfig.Gui, container)
		},
	}
}

var containerStates = map[string]int{
	"running": 1,
	"stopped": 2,
	"exited":  2,
	"created": 3,
}

func sortContainers(a *commands.Container, b *commands.Container, legacySort bool) bool {
	if legacySort {
		return a.Name < b.Name
	}

	stateLeft := containerStates[a.GetStatus()]
	stateRight := containerStates[b.GetStatus()]
	if stateLeft == stateRight {
		return a.Name < b.Name
	}

	return stateLeft < stateRight
}

func (gui *Gui) renderContainerEnv(container *commands.Container) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.containerEnv(container) })
}

func (gui *Gui) containerEnv(container *commands.Container) string {
	env := container.GetEnv()
	if len(env) == 0 {
		return gui.Tr.NothingToDisplay
	}

	var lines []string
	for _, envVar := range env {
		splitEnv := strings.SplitN(envVar, "=", 2)
		key := splitEnv[0]
		value := ""
		if len(splitEnv) > 1 {
			value = splitEnv[1]
		}
		lines = append(lines, fmt.Sprintf("%s=%s",
			utils.ColoredString(key, color.FgGreen),
			utils.ColoredString(value, color.FgYellow),
		))
	}

	return strings.Join(lines, "\n")
}

func (gui *Gui) renderContainerConfig(container *commands.Container) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.containerConfig(container) })
}

func (gui *Gui) containerConfig(container *commands.Container) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("%s: %s",
		utils.ColoredString("ID", color.FgCyan),
		container.ID,
	))
	lines = append(lines, fmt.Sprintf("%s: %s",
		utils.ColoredString("Name", color.FgCyan),
		container.Name,
	))
	lines = append(lines, fmt.Sprintf("%s: %s",
		utils.ColoredString("Image", color.FgCyan),
		container.GetImage(),
	))
	lines = append(lines, fmt.Sprintf("%s: %s",
		utils.ColoredString("Status", color.FgCyan),
		container.GetStatus(),
	))
	lines = append(lines, fmt.Sprintf("%s: %s",
		utils.ColoredString("IP", color.FgCyan),
		container.GetIP(),
	))
	lines = append(lines, fmt.Sprintf("%s: %d",
		utils.ColoredString("CPUs", color.FgCyan),
		container.GetCPUs(),
	))
	lines = append(lines, fmt.Sprintf("%s: %s",
		utils.ColoredString("Memory", color.FgCyan),
		utils.FormatBinaryBytes(int(container.GetMemory())),
	))

	ports := container.GetPorts()
	if len(ports) > 0 {
		lines = append(lines, fmt.Sprintf("%s: %s",
			utils.ColoredString("Ports", color.FgCyan),
			strings.Join(ports, ", "),
		))
	}

	return strings.Join(lines, "\n")
}

func (gui *Gui) renderContainerStats(container *commands.Container) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.containerStats(container) })
}

func (gui *Gui) containerStats(container *commands.Container) string {
	stats, ok := container.GetLastStats()
	if !ok {
		return gui.Tr.NoStats
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("%s: %.1f%%",
		utils.ColoredString("CPU", color.FgCyan),
		stats.DerivedStats.CPUPercentage,
	))
	lines = append(lines, fmt.Sprintf("%s: %.1f%% (%s / %s)",
		utils.ColoredString("Memory",
			color.FgCyan),
		stats.DerivedStats.MemoryPercentage,
		utils.FormatBinaryBytes(int(stats.ClientStats.MemoryUsageBytes)),
		utils.FormatBinaryBytes(int(stats.ClientStats.MemoryLimitBytes)),
	))
	lines = append(lines, fmt.Sprintf("%s: %s / %s",
		utils.ColoredString("Network I/O", color.FgCyan),
		utils.FormatBinaryBytes(int(stats.ClientStats.NetworkRxBytes)),
		utils.FormatBinaryBytes(int(stats.ClientStats.NetworkTxBytes)),
	))
	lines = append(lines, fmt.Sprintf("%s: %s / %s",
		utils.ColoredString("Block I/O", color.FgCyan),
		utils.FormatBinaryBytes(int(stats.ClientStats.BlockReadBytes)),
		utils.FormatBinaryBytes(int(stats.ClientStats.BlockWriteBytes)),
	))
	lines = append(lines, fmt.Sprintf("%s: %d",
		utils.ColoredString("Processes", color.FgCyan),
		stats.ClientStats.NumProcesses,
	))

	return strings.Join(lines, "\n")
}

func (gui *Gui) renderContainerTop(container *commands.Container) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			contents, err := container.RenderTop(ctx)
			if err != nil {
				gui.RenderStringMain(err.Error())
			}

			gui.reRenderStringMain(contents)
		},
		Duration:   time.Second,
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Autoscroll: false,
	})
}

func (gui *Gui) refreshContainers() error {
	containers := gui.ContainerCmd.RefreshContainers(gui.Panels.Containers.List.GetAllItems())
	gui.Panels.Containers.SetItems(containers)
	return gui.Panels.Containers.RerenderList()
}

func (gui *Gui) handleHideStoppedContainers(g *gocui.Gui, v *gocui.View) error {
	gui.State.ShowExitedContainers = !gui.State.ShowExitedContainers
	return gui.refreshContainers()
}

func (gui *Gui) handleContainerStop(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.StopContainer, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			if err := container.Stop(); err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainerRestart(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
		if err := container.Restart(); err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return nil
	})
}

func (gui *Gui) handleContainerAttach(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	c, err := container.Attach()
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocess(c)
}

func (gui *Gui) handleContainerViewLogs(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	cmd := gui.ContainerCmd.Client.LogsContainer(container.ID, true, 0)
	return gui.runSubprocess(cmd)
}

func (gui *Gui) handleContainerKill(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.KillingStatus, func() error {
		if err := container.Kill(""); err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return nil
	})
}

func (gui *Gui) handleContainersRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.RemoveContainer, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			if err := container.Remove(true); err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainersExecShell(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if container.GetStatus() != "running" {
		return gui.createErrorPanel(gui.Tr.ContainerNotRunning)
	}

	cmd := gui.ContainerCmd.Client.ExecContainer(container.ID, commands.ExecOptions{
		Command:     []string{"/bin/sh"},
		Interactive: true,
		TTY:         true,
	})

	return gui.runSubprocess(cmd)
}

func (gui *Gui) handleContainersCustomCommand(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.ContainerCmd.NewCommandObject(commands.CommandObject{
		Container: container,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Containers

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleContainersBulkCommand(g *gocui.Gui, v *gocui.View) error {
	bulkCommands := gui.Config.UserConfig.BulkCommands.Containers
	commandObject := gui.ContainerCmd.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}

func (gui *Gui) handleContainersOpenInBrowserCommand(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	ip := container.GetIP()
	if ip == "" {
		return gui.createErrorPanel(gui.Tr.NoIp)
	}

	ports := container.GetPorts()
	if len(ports) == 0 {
		return gui.createErrorPanel(gui.Tr.NoPorts)
	}

	for _, port := range ports {
		parts := strings.Split(port, ":")
		if len(parts) >= 2 {
			hostPort := strings.Split(parts[0], "->")[0]
			url := fmt.Sprintf("http://%s:%s", ip, hostPort)
			return gui.OSCommand.OpenLink(url)
		}
	}

	return nil
}

func (gui *Gui) PauseContainer(container *commands.Container) error {
	return gui.createErrorPanel("Pause is not supported by Apple Container")
}
