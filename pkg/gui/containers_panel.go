package gui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

func (gui *Gui) getContainersPanel() *SideListPanel[*commands.Container] {
	states := map[string]int{
		"running": 1,
		"exited":  2,
		"created": 3,
	}

	// Standalone containers are containers which are either one-off containers, or whose service is not part of this docker-compose context.
	isStandaloneContainer := func(container *commands.Container) bool {
		if container.OneOff || container.ServiceName == "" {
			return true
		}

		return !lo.SomeBy(gui.Panels.Services.list.GetAllItems(), func(service *commands.Service) bool {
			return service.Name == container.ServiceName
		})
	}

	return &SideListPanel[*commands.Container]{
		contextKeyPrefix: "containers",
		ListPanel: ListPanel[*commands.Container]{
			list: NewFilteredList[*commands.Container](),
			view: gui.Views.Containers,
		},
		contextIdx:     0,
		noItemsMessage: gui.Tr.NoContainers,
		gui:            gui.intoInterface(),
		getContexts: func() []ContextConfig[*commands.Container] {
			return []ContextConfig[*commands.Container]{
				{
					key:    "logs",
					title:  gui.Tr.LogsTitle,
					render: gui.renderContainerLogsToMain,
				},
				{
					key:    "stats",
					title:  gui.Tr.StatsTitle,
					render: gui.renderContainerStats,
				},
				{
					key:    "env",
					title:  gui.Tr.EnvTitle,
					render: gui.renderContainerEnv,
				},
				{
					key:    "config",
					title:  gui.Tr.ConfigTitle,
					render: gui.renderContainerConfig,
				},
				{
					key:    "top",
					title:  gui.Tr.TopTitle,
					render: gui.renderContainerTop,
				},
			}
		},
		getContextCacheKey: func(container *commands.Container) string {
			return container.ID + "-" + container.Container.State
		},
		// sortedContainers returns containers sorted by state if c.SortContainersByState is true (follows 1- running, 2- exited, 3- created)
		// and sorted by name if c.SortContainersByState is false
		sort: func(a *commands.Container, b *commands.Container) bool {
			if gui.Config.UserConfig.Gui.LegacySortContainers {
				return a.Name < b.Name
			}

			stateLeft := states[a.Container.State]
			stateRight := states[b.Container.State]
			if stateLeft == stateRight {
				return a.Name < b.Name
			}

			return states[a.Container.State] < states[b.Container.State]
		},
		filter: func(container *commands.Container) bool {
			// Note that this is O(N*M) time complexity where N is the number of services
			// and M is the number of containers. We expect N to be small but M may be large,
			// so we will need to keep an eye on this.
			if !gui.Config.UserConfig.Gui.ShowAllContainers && !isStandaloneContainer(container) {
				return false
			}

			if !gui.State.ShowExitedContainers && container.Container.State == "exited" {
				return false
			}

			return true
		},
		getDisplayStrings: func(container *commands.Container) []string {
			image := strings.TrimPrefix(container.Container.Image, "sha256:")

			return []string{
				container.GetDisplayStatus(),
				container.GetDisplaySubstatus(),
				container.Name,
				container.GetDisplayCPUPerc(),
				utils.ColoredString(image, color.FgMagenta),
				utils.ColoredString(container.DisplayPorts(), color.FgYellow),
			}
		},
	}
}

func (gui *Gui) renderContainerEnv(container *commands.Container) error {
	if !container.DetailsLoaded() {
		return gui.T.NewTask(func(stop chan struct{}) {
			_ = gui.RenderStringMain(gui.Tr.WaitingForContainerInfo)
		})
	}

	mainView := gui.Views.Main
	mainView.Autoscroll = false
	mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

	return gui.T.NewTask(func(stop chan struct{}) {
		_ = gui.RenderStringMain(gui.containerEnv(container))
	})
}

func (gui *Gui) containerEnv(container *commands.Container) string {
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

func (gui *Gui) renderContainerConfig(container *commands.Container) error {
	if !container.DetailsLoaded() {
		return gui.T.NewTask(func(stop chan struct{}) {
			_ = gui.RenderStringMain(gui.Tr.WaitingForContainerInfo)
		})
	}

	mainView := gui.Views.Main
	mainView.Autoscroll = false
	mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

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
				output += fmt.Sprintf("%s%s %s\n", strings.Repeat(" ", padding), utils.ColoredString(string(mount.Type)+":", color.FgYellow), mount.Name)
			} else {
				output += fmt.Sprintf("%s%s %s:%s\n", strings.Repeat(" ", padding), utils.ColoredString(string(mount.Type)+":", color.FgYellow), mount.Source, mount.Destination)
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

	data, err := json.MarshalIndent(&container.Details, "", "  ")
	if err != nil {
		return err
	}
	output += fmt.Sprintf("\nFull details:\n\n%s", string(data))

	return gui.T.NewTask(func(stop chan struct{}) {
		_ = gui.RenderStringMain(output)
	})
}

func (gui *Gui) renderContainerStats(container *commands.Container) error {
	mainView := gui.Views.Main
	mainView.Autoscroll = false
	mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

	return gui.T.NewTickerTask(time.Second, func(stop chan struct{}) { gui.clearMainView() }, func(stop, notifyStopped chan struct{}) {
		width, _ := mainView.Size()

		contents, err := container.RenderStats(width)
		if err != nil {
			_ = gui.createErrorPanel(err.Error())
		}

		gui.reRenderStringMain(contents)
	})
}

func (gui *Gui) renderContainerTop(container *commands.Container) error {
	mainView := gui.Views.Main
	mainView.Autoscroll = false
	mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

	return gui.T.NewTickerTask(time.Second, func(stop chan struct{}) { gui.clearMainView() }, func(stop, notifyStopped chan struct{}) {
		contents, err := container.RenderTop()
		if err != nil {
			gui.reRenderStringMain(err.Error())
		}

		gui.reRenderStringMain(contents)
	})
}

func (gui *Gui) refreshContainersAndServices() error {
	if gui.Views.Containers == nil {
		// if the containersView hasn't been instantiated yet we just return
		return nil
	}

	// keep track of current service selected so that we can reposition our cursor if it moves position in the list
	originalSelectedLineIdx := gui.Panels.Services.selectedIdx
	selectedService, isServiceSelected := gui.Panels.Services.list.TryGet(originalSelectedLineIdx)

	containers, services, err := gui.DockerCommand.RefreshContainersAndServices(
		gui.Panels.Services.list.GetAllItems(),
		gui.Panels.Containers.list.GetAllItems(),
	)
	if err != nil {
		return err
	}

	gui.Panels.Services.SetItems(services)
	gui.Panels.Containers.SetItems(containers)

	// see if our selected service has moved
	if isServiceSelected {
		for i, service := range gui.Panels.Services.list.GetItems() {
			if service.ID == selectedService.ID {
				if i == originalSelectedLineIdx {
					break
				}
				gui.Panels.Services.setSelectedLineIdx(i)
				gui.Panels.Services.Refocus()
			}
		}
	}

	return gui.renderContainersAndServices()
}

func (gui *Gui) renderContainersAndServices() error {
	if gui.DockerCommand.InDockerComposeProject {
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
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	handleMenuPress := func(configOptions types.ContainerRemoveOptions) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			if err := container.Remove(configOptions); err != nil {
				if commands.HasErrorCode(err, commands.MustStopContainer) {
					return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.MustForceToRemoveContainer, func(g *gocui.Gui, v *gocui.View) error {
						return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
							configOptions.Force = true
							return container.Remove(configOptions)
						})
					}, nil)
				}
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}

	menuItems := []*MenuItem{
		{
			LabelColumns: []string{gui.Tr.Remove, "docker rm " + container.ID[1:10]},
			OnPress:      func() error { return handleMenuPress(types.ContainerRemoveOptions{}) },
		},
		{
			LabelColumns: []string{gui.Tr.RemoveWithVolumes, "docker rm --volumes " + container.ID[1:10]},
			OnPress:      func() error { return handleMenuPress(types.ContainerRemoveOptions{RemoveVolumes: true}) },
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
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.PauseContainer(container)
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

func (gui *Gui) handlePruneContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmPruneContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.DockerCommand.PruneContainers()
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleContainerViewLogs(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	gui.renderLogsToStdout(container)

	return nil
}

func (gui *Gui) handleContainersExecShell(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.containerExecShell(container)
}

func (gui *Gui) containerExecShell(container *commands.Container) error {
	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{
		Container: container,
	})

	// TODO: use SDK
	resolvedCommand := utils.ApplyTemplate("docker exec -it {{ .Container.ID }} /bin/sh -c 'eval $(grep ^$(id -un): /etc/passwd | cut -d : -f 7-)'", commandObject)
	// attach and return the subprocess error
	cmd := gui.OSCommand.ExecutableFromString(resolvedCommand)
	return gui.runSubprocess(cmd)
}

func (gui *Gui) handleContainersCustomCommand(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{
		Container: container,
	})

	customCommands := gui.Config.UserConfig.CustomCommands.Containers

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleStopContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmStopContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			for _, container := range gui.Panels.Containers.list.GetAllItems() {
				if err := container.Stop(); err != nil {
					gui.Log.Error(err)
				}
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleRemoveContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmRemoveContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			for _, container := range gui.Panels.Containers.list.GetAllItems() {
				if err := container.Remove(types.ContainerRemoveOptions{Force: true}); err != nil {
					gui.Log.Error(err)
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
	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}

// Open first port in browser
func (gui *Gui) handleContainersOpenInBrowserCommand(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.openContainerInBrowser(container)
}

func (gui *Gui) openContainerInBrowser(container *commands.Container) error {
	// skip if no any ports
	if len(container.Container.Ports) == 0 {
		return nil
	}
	// skip if the first port is not published
	port := container.Container.Ports[0]
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
