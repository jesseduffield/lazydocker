package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/christophe-duc/lazypodman/pkg/commands"
	"github.com/christophe-duc/lazypodman/pkg/config"
	"github.com/christophe-duc/lazypodman/pkg/gui/panels"
	"github.com/christophe-duc/lazypodman/pkg/gui/presentation"
	"github.com/christophe-duc/lazypodman/pkg/gui/types"
	"github.com/christophe-duc/lazypodman/pkg/tasks"
	"github.com/christophe-duc/lazypodman/pkg/utils"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/samber/lo"
)

func (gui *Gui) getContainersPanel() *panels.SideListPanel[*commands.ContainerListItem] {
	// Standalone containers are containers which are either one-off containers, or whose service is not part of this docker-compose context.
	isStandaloneContainer := func(container *commands.Container) bool {
		if container.OneOff || container.ServiceName == "" {
			return true
		}

		return !lo.SomeBy(gui.Panels.Services.List.GetAllItems(), func(service *commands.Service) bool {
			return service.Name == container.ServiceName
		})
	}

	return &panels.SideListPanel[*commands.ContainerListItem]{
		ContextState: &panels.ContextState[*commands.ContainerListItem]{
			GetMainTabs: func() []panels.MainTab[*commands.ContainerListItem] {
				return []panels.MainTab[*commands.ContainerListItem]{
					{
						Key:    "logs",
						Title:  gui.Tr.LogsTitle,
						Render: gui.renderContainerListItemLogs,
					},
					{
						Key:    "stats",
						Title:  gui.Tr.StatsTitle,
						Render: gui.renderContainerListItemStats,
					},
					{
						Key:    "env",
						Title:  gui.Tr.EnvTitle,
						Render: gui.renderContainerListItemEnv,
					},
					{
						Key:    "config",
						Title:  gui.Tr.ConfigTitle,
						Render: gui.renderContainerListItemConfig,
					},
					{
						Key:    "top",
						Title:  gui.Tr.TopTitle,
						Render: gui.renderContainerListItemTop,
					},
				}
			},
			GetItemContextCacheKey: func(item *commands.ContainerListItem) string {
				// Including the state in the cache key so that if the container/pod
				// restarts we re-read the logs.
				return "containers-" + item.ID() + "-" + item.State()
			},
		},
		ListPanel: panels.ListPanel[*commands.ContainerListItem]{
			List: panels.NewFilteredList[*commands.ContainerListItem](),
			View: gui.Views.Containers,
		},
		NoItemsMessage: gui.Tr.NoContainers,
		Gui:            gui.intoInterface(),
		// Sort items: pods first (with their containers grouped), then standalone containers
		Sort: func(a *commands.ContainerListItem, b *commands.ContainerListItem) bool {
			return sortContainerListItems(a, b, gui.Config.UserConfig.Gui.LegacySortContainers)
		},
		Filter: func(item *commands.ContainerListItem) bool {
			// Pods are always shown
			if item.IsPod {
				return true
			}

			container := item.Container

			// Hide containers in collapsed pods
			if container.Summary.Pod != "" {
				if !gui.State.ExpandedPods[container.Summary.Pod] {
					return false // Pod is collapsed, hide this container
				}
			}

			// Note that this is O(N*M) time complexity where N is the number of services
			// and M is the number of containers. We expect N to be small but M may be large,
			// so we will need to keep an eye on this.
			if !gui.Config.UserConfig.Gui.ShowAllContainers && !isStandaloneContainer(container) {
				return false
			}

			if !gui.State.ShowExitedContainers && container.Summary.State == "exited" {
				return false
			}

			return true
		},
		GetTableCells: func(item *commands.ContainerListItem) []string {
			expanded := false
			if item.IsPod && item.Pod != nil {
				expanded = gui.State.ExpandedPods[item.Pod.ID]
			}
			return presentation.GetContainerListItemDisplayStrings(&gui.Config.UserConfig.Gui, item, expanded)
		},
	}
}

var containerStates = map[string]int{
	"running": 1,
	"exited":  2,
	"created": 3,
}

func sortContainers(a *commands.Container, b *commands.Container, legacySort bool) bool {
	if legacySort {
		return a.Name < b.Name
	}

	stateLeft := containerStates[a.Summary.State]
	stateRight := containerStates[b.Summary.State]
	if stateLeft == stateRight {
		return a.Name < b.Name
	}

	return containerStates[a.Summary.State] < containerStates[b.Summary.State]
}

// sortContainerListItems sorts items to group pods with their containers.
// Order: pods first (sorted alphabetically), then their containers indented (sorted alphabetically),
// then standalone containers (sorted alphabetically).
func sortContainerListItems(a *commands.ContainerListItem, b *commands.ContainerListItem, legacySort bool) bool {
	// Pods and their containers sort before standalone containers
	aInPod := a.IsPod || a.PodID() != ""
	bInPod := b.IsPod || b.PodID() != ""

	if aInPod && !bInPod {
		return true // Pods/pod containers before standalone
	}
	if !aInPod && bInPod {
		return false
	}

	// Both are in the same category (pod-related or standalone)

	// For pod-related items, sort by pod name first, then by type (pod before containers), then by container name
	if aInPod && bInPod {
		// Get effective pod name for comparison
		aPodName := a.PodName()
		if a.IsPod {
			aPodName = a.Name()
		}
		bPodName := b.PodName()
		if b.IsPod {
			bPodName = b.Name()
		}

		// Different pods: sort by pod name
		if aPodName != bPodName {
			return aPodName < bPodName
		}

		// Same pod: pod comes first, then containers alphabetically
		if a.IsPod && !b.IsPod {
			return true
		}
		if !a.IsPod && b.IsPod {
			return false
		}

		// Both are containers in the same pod: sort by name
		return a.Name() < b.Name()
	}

	// Both are standalone containers
	if legacySort {
		return a.Name() < b.Name()
	}
	// Sort by state, then by name
	stateA := containerStates[a.State()]
	stateB := containerStates[b.State()]
	if stateA == stateB {
		return a.Name() < b.Name()
	}
	return stateA < stateB
}

// Wrapper functions that delegate to container or pod rendering

func (gui *Gui) renderContainerListItemLogs(item *commands.ContainerListItem) tasks.TaskFunc {
	if item.IsPod {
		return gui.renderPodLogsToMain(item.Pod)
	}
	return gui.renderContainerLogsToMain(item.Container)
}

func (gui *Gui) renderContainerListItemStats(item *commands.ContainerListItem) tasks.TaskFunc {
	if item.IsPod {
		return gui.renderPodStats(item.Pod)
	}
	return gui.renderContainerStats(item.Container)
}

func (gui *Gui) renderContainerListItemEnv(item *commands.ContainerListItem) tasks.TaskFunc {
	if item.IsPod {
		return gui.NewSimpleRenderStringTask(func() string {
			return "Environment variables not available for pods. Select a container to view environment."
		})
	}
	return gui.renderContainerEnv(item.Container)
}

func (gui *Gui) renderContainerListItemConfig(item *commands.ContainerListItem) tasks.TaskFunc {
	if item.IsPod {
		return gui.renderPodConfig(item.Pod)
	}
	return gui.renderContainerConfig(item.Container)
}

func (gui *Gui) renderContainerListItemTop(item *commands.ContainerListItem) tasks.TaskFunc {
	if item.IsPod {
		return gui.NewSimpleRenderStringTask(func() string {
			return "Process list not available for pods. Select a container to view processes."
		})
	}
	return gui.renderContainerTop(item.Container)
}

func (gui *Gui) renderPodConfig(pod *commands.Pod) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string {
		padding := 10
		output := ""
		output += utils.ColoredString("Pod Information\n\n", color.FgCyan)
		output += utils.WithPadding("ID: ", padding) + pod.ID + "\n"
		output += utils.WithPadding("Name: ", padding) + pod.Name + "\n"
		output += utils.WithPadding("Status: ", padding) + pod.State() + "\n"
		output += utils.WithPadding("Created: ", padding) + pod.Summary.Created.Format(time.RFC3339) + "\n"
		output += fmt.Sprintf("%s%d\n", utils.WithPadding("Containers: ", padding), len(pod.Containers))

		if len(pod.Containers) > 0 {
			output += "\n" + utils.ColoredString("Containers in this pod:\n", color.FgYellow)
			for _, c := range pod.Containers {
				stateColor := color.FgWhite
				switch c.Summary.State {
				case "running":
					stateColor = color.FgGreen
				case "exited":
					stateColor = color.FgRed
				}
				output += fmt.Sprintf("  - %s (%s)\n", c.Name, utils.ColoredString(c.Summary.State, stateColor))
			}
		}

		return output
	})
}

func (gui *Gui) renderContainerEnv(container *commands.Container) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.containerEnv(container) })
}

func (gui *Gui) containerEnv(container *commands.Container) string {
	if !container.DetailsLoaded() {
		return gui.Tr.WaitingForContainerInfo
	}

	if len(container.Details.Config.Env) == 0 {
		return gui.Tr.NothingToDisplay
	}

	envVarsList := lo.Map(container.Details.Config.Env, func(envVar string, _ int) []string {
		splitEnv := strings.SplitN(envVar, "=", 2)
		key := splitEnv[0]
		value := ""
		if len(splitEnv) > 1 {
			value = splitEnv[1]
		}
		return []string{
			utils.ColoredString(key+":", color.FgGreen),
			utils.ColoredString(value, color.FgYellow),
		}
	})

	output, err := utils.RenderTable(envVarsList)
	if err != nil {
		gui.Log.Error(err)
		return gui.Tr.CannotDisplayEnvVariables
	}

	return output
}

func (gui *Gui) renderContainerConfig(container *commands.Container) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.containerConfigStr(container) })
}

func (gui *Gui) containerConfigStr(container *commands.Container) string {
	if !container.DetailsLoaded() {
		return gui.Tr.WaitingForContainerInfo
	}

	padding := 10
	output := ""
	output += utils.WithPadding("ID: ", padding) + container.ID + "\n"
	output += utils.WithPadding("Name: ", padding) + container.Name + "\n"
	output += utils.WithPadding("Image: ", padding) + container.Details.Config.Image + "\n"
	output += utils.WithPadding("Command: ", padding) + strings.Join(append([]string{container.Details.Path}, container.Details.Args...), " ") + "\n"
	output += utils.WithPadding("Labels: ", padding) + utils.FormatMap(padding, container.Details.Config.Labels)
	output += "\n"

	output += utils.WithPadding("Mounts: ", padding)
	if len(container.Details.Mounts) > 0 {
		output += "\n"
		for _, mount := range container.Details.Mounts {
			if mount.Type == "volume" {
				output += fmt.Sprintf("%s%s %s\n", strings.Repeat(" ", padding), utils.ColoredString(mount.Type+":", color.FgYellow), mount.Name)
			} else {
				output += fmt.Sprintf("%s%s %s:%s\n", strings.Repeat(" ", padding), utils.ColoredString(mount.Type+":", color.FgYellow), mount.Source, mount.Destination)
			}
		}
	} else {
		output += "none\n"
	}

	output += utils.WithPadding("Ports: ", padding)
	if len(container.Details.NetworkSettings.Ports) > 0 {
		output += "\n"
		for k, v := range container.Details.NetworkSettings.Ports {
			for _, host := range v {
				output += fmt.Sprintf("%s%s %s\n", strings.Repeat(" ", padding), utils.ColoredString(host.HostPort+":", color.FgYellow), k)
			}
		}
	} else {
		output += "none\n"
	}

	data, err := utils.MarshalIntoYaml(&container.Details)
	if err != nil {
		return fmt.Sprintf("Error marshalling container details: %v", err)
	}

	output += fmt.Sprintf("\nFull details:\n\n%s", utils.ColoredYamlString(string(data)))

	return output
}

func (gui *Gui) renderContainerStats(container *commands.Container) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			contents, err := presentation.RenderStats(gui.Config.UserConfig, container, gui.Views.Main.Width())
			if err != nil {
				_ = gui.createErrorPanel(err.Error())
			}

			gui.reRenderStringMain(contents)
		},
		Duration:   time.Second,
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       false, // wrapping looks bad here so we're overriding the config value
		Autoscroll: false,
	})
}

func (gui *Gui) renderPodStats(pod *commands.Pod) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			// Start monitoring if not already
			if !pod.MonitoringStats {
				go gui.PodmanCommand.CreatePodStatMonitor(pod)
			}

			contents, err := presentation.RenderPodStats(pod, gui.Views.Main.Width())
			if err != nil {
				_ = gui.createErrorPanel(err.Error())
			}

			gui.reRenderStringMain(contents)
		},
		Duration:   time.Second,
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       false,
		Autoscroll: false,
	})
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

func (gui *Gui) refreshContainersAndServices() error {
	if gui.Views.Containers == nil {
		// if the containersView hasn't been instantiated yet we just return
		return nil
	}

	// keep track of current service selected so that we can reposition our cursor if it moves position in the list
	originalSelectedLineIdx := gui.Panels.Services.SelectedIdx
	selectedService, isServiceSelected := gui.Panels.Services.List.TryGet(originalSelectedLineIdx)

	containers, services, err := gui.PodmanCommand.RefreshContainersAndServices(
		gui.Panels.Services.List.GetAllItems(),
		gui.Panels.Containers.List.GetAllItems(),
	)
	if err != nil {
		return err
	}

	gui.Panels.Services.SetItems(services)
	gui.Panels.Containers.SetItems(containers)

	// see if our selected service has moved
	if isServiceSelected {
		for i, service := range gui.Panels.Services.List.GetItems() {
			if service.ID == selectedService.ID {
				if i == originalSelectedLineIdx {
					break
				}
				gui.Panels.Services.SetSelectedLineIdx(i)
				gui.Panels.Services.Refocus()
			}
		}
	}

	return gui.renderContainersAndServices()
}

func (gui *Gui) renderContainersAndServices() error {
	if gui.PodmanCommand.InComposeProject {
		if err := gui.Panels.Services.RerenderList(); err != nil {
			return err
		}
	}

	if err := gui.Panels.Containers.RerenderList(); err != nil {
		return err
	}

	return nil
}

func (gui *Gui) handleHideStoppedContainers(g *gocui.Gui, v *gocui.View) error {
	gui.State.ShowExitedContainers = !gui.State.ShowExitedContainers

	return gui.Panels.Containers.RerenderList()
}

func (gui *Gui) handleContainersRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Remove not yet supported for pods")
	}

	ctr := item.Container
	handleMenuPress := func(force bool, removeVolumes bool) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			if err := ctr.Remove(force, removeVolumes); err != nil {
				if commands.HasErrorCode(err, commands.MustStopContainer) {
					return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.MustForceToRemoveContainer, func(g *gocui.Gui, v *gocui.View) error {
						return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
							return ctr.Remove(true, removeVolumes)
						})
					}, nil)
				}
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}

	menuItems := []*types.MenuItem{
		{
			LabelColumns: []string{gui.Tr.Remove, "podman rm " + ctr.ID[1:10]},
			OnPress:      func() error { return handleMenuPress(false, false) },
		},
		{
			LabelColumns: []string{gui.Tr.RemoveWithVolumes, "podman rm --volumes " + ctr.ID[1:10]},
			OnPress:      func() error { return handleMenuPress(false, true) },
		},
	}

	return gui.Menu(CreateMenuOptions{
		Title: "",
		Items: menuItems,
	})
}

func (gui *Gui) PauseContainer(container *commands.Container) error {
	return gui.WithWaitingStatus(gui.Tr.PausingStatus, func() (err error) {
		if container.Details.State.Paused {
			err = container.Unpause()
		} else {
			err = container.Pause()
		}

		if err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return gui.refreshContainersAndServices()
	})
}

func (gui *Gui) handleContainerPause(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Pause not yet supported for pods")
	}

	return gui.PauseContainer(item.Container)
}

func (gui *Gui) handleContainerStop(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Stop not yet supported for pods")
	}

	ctr := item.Container
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.StopContainer, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			if err := ctr.Stop(); err != nil {
				return gui.createErrorPanel(err.Error())
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainerRestart(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Restart not yet supported for pods")
	}

	ctr := item.Container
	return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
		if err := ctr.Restart(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}

func (gui *Gui) handleContainerAttach(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Attach not yet supported for pods")
	}

	ctr := item.Container
	c, err := ctr.Attach()
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocessWithMessage(c, gui.Tr.DetachFromContainerShortCut)
}

func (gui *Gui) handlePruneContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmPruneContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.PodmanCommand.PruneContainers()
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainerViewLogs(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		// TODO: implement pod logs to stdout
		return gui.createErrorPanel("View logs (stdout) not yet supported for pods")
	}

	gui.renderLogsToStdout(item.Container)

	return nil
}

func (gui *Gui) handleContainersExecShell(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Exec shell not yet supported for pods. Select a container instead.")
	}

	return gui.containerExecShell(item.Container)
}

func (gui *Gui) containerExecShell(container *commands.Container) error {
	commandObject := gui.PodmanCommand.NewCommandObject(commands.CommandObject{
		Container: container,
	})

	// TODO: use SDK
	resolvedCommand := utils.ApplyTemplate("podman exec -it {{ .Container.ID }} /bin/sh -c 'eval $(grep ^$(id -un): /etc/passwd | cut -d : -f 7-)'", commandObject)
	// attach and return the subprocess error
	cmd := gui.OSCommand.ExecutableFromString(resolvedCommand)
	return gui.runSubprocess(cmd)
}

func (gui *Gui) handleContainersCustomCommand(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Custom commands not yet supported for pods. Select a container instead.")
	}

	commandObject := gui.PodmanCommand.NewCommandObject(commands.CommandObject{
		Container: item.Container,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Containers

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleStopContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmStopContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			for _, item := range gui.Panels.Containers.List.GetAllItems() {
				if !item.IsPod && item.Container != nil {
					if err := item.Container.Stop(); err != nil {
						gui.Log.Error(err)
					}
				}
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleRemoveContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmRemoveContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			for _, item := range gui.Panels.Containers.List.GetAllItems() {
				if !item.IsPod && item.Container != nil {
					if err := item.Container.Remove(true, false); err != nil {
						gui.Log.Error(err)
					}
				}
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainersBulkCommand(g *gocui.Gui, v *gocui.View) error {
	baseBulkCommands := []config.CustomCommand{
		{
			Name:             gui.Tr.StopAllContainers,
			InternalFunction: gui.handleStopContainers,
		},
		{
			Name:             gui.Tr.RemoveAllContainers,
			InternalFunction: gui.handleRemoveContainers,
		},
		{
			Name:             gui.Tr.PruneContainers,
			InternalFunction: gui.handlePruneContainers,
		},
	}

	bulkCommands := append(baseBulkCommands, gui.Config.UserConfig.BulkCommands.Containers...)
	commandObject := gui.PodmanCommand.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}

// Open first port in browser
func (gui *Gui) handleContainersOpenInBrowserCommand(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if item.IsPod {
		return gui.createErrorPanel("Cannot open pod in browser. Select a container instead.")
	}

	return gui.openContainerInBrowser(item.Container)
}

func (gui *Gui) openContainerInBrowser(ctr *commands.Container) error {
	// skip if no any ports
	if len(ctr.Summary.Ports) == 0 {
		return nil
	}
	// skip if the first port is not published
	port := ctr.Summary.Ports[0]
	if port.IP == "" {
		return nil
	}
	ip := port.IP
	if ip == "0.0.0.0" {
		ip = "localhost"
	}
	link := fmt.Sprintf("http://%s:%d/", ip, port.PublicPort)
	return gui.OSCommand.OpenLink(link)
}

func (gui *Gui) handleTogglePodExpansion(g *gocui.Gui, v *gocui.View) error {
	item, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	if !item.IsPod {
		return nil // Only works on pods
	}

	podID := item.Pod.ID
	gui.State.ExpandedPods[podID] = !gui.State.ExpandedPods[podID]

	return gui.Panels.Containers.RerenderList()
}
