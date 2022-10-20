package gui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

// list panel functions

func (gui *Gui) getContainerContexts() []string {
	return []string{"logs", "stats", "env", "config", "top"}
}

func (gui *Gui) getContainerContextTitles() []string {
	return []string{gui.Tr.LogsTitle, gui.Tr.StatsTitle, gui.Tr.EnvTitle, gui.Tr.ConfigTitle, gui.Tr.TopTitle}
}

func (gui *Gui) getSelectedContainer() (*commands.Container, error) {
	selectedLine := gui.State.Panels.Containers.SelectedLine
	if selectedLine == -1 {
		return &commands.Container{}, gui.Errors.ErrNoContainers
	}

	return gui.DockerCommand.DisplayContainers[selectedLine], nil
}

func (gui *Gui) handleContainersClick(g *gocui.Gui, v *gocui.View) error {
	itemCount := len(gui.DockerCommand.DisplayContainers)
	handleSelect := gui.handleContainerSelect
	selectedLine := &gui.State.Panels.Containers.SelectedLine

	return gui.handleClick(v, itemCount, selectedLine, handleSelect)
}

func (gui *Gui) handleContainerSelect(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer()
	if err != nil {
		if err != gui.Errors.ErrNoContainers {
			return err
		}
		return nil
	}

	gui.focusY(gui.State.Panels.Containers.SelectedLine, len(gui.DockerCommand.DisplayContainers), v)

	key := "containers-" + container.ID + "-" + gui.getContainerContexts()[gui.State.Panels.Containers.ContextIndex]
	if !gui.shouldRefresh(key) {
		return nil
	}

	mainView := gui.getMainView()
	mainView.Tabs = gui.getContainerContextTitles()
	mainView.TabIndex = gui.State.Panels.Containers.ContextIndex

	gui.clearMainView()

	switch gui.getContainerContexts()[gui.State.Panels.Containers.ContextIndex] {
	case "logs":
		if err := gui.renderContainerLogsToMain(container); err != nil {
			return err
		}
	case "config":
		if err := gui.renderContainerConfig(container); err != nil {
			return err
		}
	case "env":
		if err := gui.renderContainerEnv(container); err != nil {
			return err
		}
	case "stats":
		if err := gui.renderContainerStats(container); err != nil {
			return err
		}
	case "top":
		if err := gui.renderContainerTop(container); err != nil {
			return err
		}
	default:
		return errors.New("Unknown context for containers panel")
	}

	return nil
}

func (gui *Gui) renderContainerEnv(container *commands.Container) error {
	if !container.DetailsLoaded() {
		return gui.T.NewTask(func(stop chan struct{}) {
			_ = gui.renderStringMain(gui.Tr.WaitingForContainerInfo)
		})
	}

	mainView := gui.getMainView()
	mainView.Autoscroll = false
	mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

	return gui.T.NewTask(func(stop chan struct{}) {
		_ = gui.renderStringMain(gui.containerEnv(container))
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
			_ = gui.renderStringMain(gui.Tr.WaitingForContainerInfo)
		})
	}

	mainView := gui.getMainView()
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
		_ = gui.renderStringMain(output)
	})
}

func (gui *Gui) renderContainerStats(container *commands.Container) error {
	mainView := gui.getMainView()
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
	mainView := gui.getMainView()
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
	containersView := gui.getContainersView()
	if containersView == nil {
		// if the containersView hasn't been instantiated yet we just return
		return nil
	}

	// keep track of current service selected so that we can reposition our cursor if it moves position in the list
	sl := gui.State.Panels.Services.SelectedLine
	var selectedService *commands.Service
	if len(gui.DockerCommand.Services) > 0 {
		selectedService = gui.DockerCommand.Services[sl]
	}

	if err := gui.DockerCommand.RefreshContainersAndServices(); err != nil {
		return err
	}

	// see if our selected service has moved
	if selectedService != nil {
		for i, service := range gui.DockerCommand.Services {
			if service.ID == selectedService.ID {
				if i == sl {
					break
				}
				gui.State.Panels.Services.SelectedLine = i
				gui.focusY(i, len(gui.DockerCommand.Services), gui.getServicesView())
			}
		}
	}

	if len(gui.DockerCommand.DisplayContainers) > 0 && gui.State.Panels.Containers.SelectedLine == -1 {
		gui.State.Panels.Containers.SelectedLine = 0
	}
	if len(gui.DockerCommand.DisplayContainers)-1 < gui.State.Panels.Containers.SelectedLine {
		gui.State.Panels.Containers.SelectedLine = len(gui.DockerCommand.DisplayContainers) - 1
	}

	// doing the exact same thing for services
	if len(gui.DockerCommand.Services) > 0 && gui.State.Panels.Services.SelectedLine == -1 {
		gui.State.Panels.Services.SelectedLine = 0
	}
	if len(gui.DockerCommand.Services)-1 < gui.State.Panels.Services.SelectedLine {
		gui.State.Panels.Services.SelectedLine = len(gui.DockerCommand.Services) - 1
	}

	gui.renderContainersAndServices()

	return nil
}

func (gui *Gui) renderContainersAndServices() {
	gui.g.Update(func(g *gocui.Gui) error {
		containersView := gui.getContainersView()
		containersView.Clear()
		isFocused := gui.g.CurrentView().Name() == "containers"

		list, err := utils.RenderList(gui.DockerCommand.DisplayContainers, utils.IsFocused(isFocused))
		if err != nil {
			return err
		}
		fmt.Fprint(containersView, list)

		if containersView == g.CurrentView() {
			if err := gui.handleContainerSelect(g, containersView); err != nil {
				return err
			}
		}

		// doing the exact same thing for services
		if !gui.DockerCommand.InDockerComposeProject {
			return nil
		}
		servicesView := gui.getServicesView()
		servicesView.Clear()
		isFocused = gui.g.CurrentView().Name() == "services"
		list, err = utils.RenderList(gui.DockerCommand.Services, utils.IsFocused(isFocused))
		if err != nil {
			return err
		}
		fmt.Fprint(servicesView, list)

		if servicesView == g.CurrentView() {
			return gui.handleServiceSelect(g, servicesView)
		}
		return nil
	})
}

func (gui *Gui) handleContainersNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() || gui.g.CurrentView() != v {
		return nil
	}

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.DisplayContainers), false)

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainersPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() || gui.g.CurrentView() != v {
		return nil
	}

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.DisplayContainers), true)

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainersNextContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getContainerContexts()
	if gui.State.Panels.Containers.ContextIndex >= len(contexts)-1 {
		gui.State.Panels.Containers.ContextIndex = 0
	} else {
		gui.State.Panels.Containers.ContextIndex++
	}

	_ = gui.handleContainerSelect(gui.g, v)

	return nil
}

func (gui *Gui) handleContainersPrevContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getContainerContexts()
	if gui.State.Panels.Containers.ContextIndex <= 0 {
		gui.State.Panels.Containers.ContextIndex = len(contexts) - 1
	} else {
		gui.State.Panels.Containers.ContextIndex--
	}

	_ = gui.handleContainerSelect(gui.g, v)

	return nil
}

type removeContainerOption struct {
	description   string
	command       string
	configOptions types.ContainerRemoveOptions
}

// GetDisplayStrings is a function.
func (r *removeContainerOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description, color.New(color.FgRed).Sprint(r.command)}
}

func (gui *Gui) handleHideStoppedContainers(g *gocui.Gui, v *gocui.View) error {
	gui.DockerCommand.ShowExited = !gui.DockerCommand.ShowExited

	if err := gui.refreshContainersAndServices(); err != nil {
		return err
	}

	return nil
}

func (gui *Gui) handleContainersRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer()
	if err != nil {
		return nil
	}

	options := []*removeContainerOption{
		{
			description:   gui.Tr.Remove,
			command:       "docker rm " + container.ID[1:10],
			configOptions: types.ContainerRemoveOptions{},
		},
		{
			description:   gui.Tr.RemoveWithVolumes,
			command:       "docker rm --volumes " + container.ID[1:10],
			configOptions: types.ContainerRemoveOptions{RemoveVolumes: true},
		},
		{
			description: gui.Tr.Cancel,
		},
	}

	handleMenuPress := func(index int) error {
		if options[index].command == "" {
			return nil
		}
		configOptions := options[index].configOptions

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

	return gui.createMenu("", options, len(options), handleMenuPress)
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
	container, err := gui.getSelectedContainer()
	if err != nil {
		return nil
	}

	return gui.PauseContainer(container)
}

func (gui *Gui) handleContainerStop(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer()
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
	container, err := gui.getSelectedContainer()
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
	container, err := gui.getSelectedContainer()
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
	container, err := gui.getSelectedContainer()
	if err != nil {
		return nil
	}

	gui.renderLogsToStdout(container)

	return nil
}

func (gui *Gui) handleContainersExecShell(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer()
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
	container, err := gui.getSelectedContainer()
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
			for _, container := range gui.DockerCommand.Containers {
				_ = container.Stop()
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleRemoveContainers() error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmRemoveContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
			for _, container := range gui.DockerCommand.Containers {
				_ = container.Remove(types.ContainerRemoveOptions{Force: true})
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
	container, err := gui.getSelectedContainer()
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
