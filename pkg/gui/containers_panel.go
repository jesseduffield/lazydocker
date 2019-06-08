package gui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// list panel functions

func (gui *Gui) getContainerContexts() []string {
	return []string{"logs", "config", "stats"}
}

func (gui *Gui) getSelectedContainer(g *gocui.Gui) (*commands.Container, error) {
	selectedLine := gui.State.Panels.Containers.SelectedLine
	if selectedLine == -1 {
		return &commands.Container{}, gui.Errors.ErrNoContainers
	}

	return gui.DockerCommand.DisplayContainers[selectedLine], nil
}

func (gui *Gui) handleContainersFocus(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	cx, cy := v.Cursor()
	_, oy := v.Origin()

	prevSelectedLine := gui.State.Panels.Containers.SelectedLine
	newSelectedLine := cy - oy

	if newSelectedLine > len(gui.DockerCommand.DisplayContainers)-1 || len(utils.Decolorise(gui.DockerCommand.DisplayContainers[newSelectedLine].Name)) < cx {
		return gui.handleContainerSelect(gui.g, v)
	}

	gui.State.Panels.Containers.SelectedLine = newSelectedLine

	if prevSelectedLine == newSelectedLine && gui.currentViewName() == v.Name() {
		return nil
	}

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainerSelect(g *gocui.Gui, v *gocui.View) error {
	if _, err := gui.g.SetCurrentView(v.Name()); err != nil {
		return err
	}

	container, err := gui.getSelectedContainer(g)
	if err != nil {
		if err != gui.Errors.ErrNoContainers {
			return err
		}
		return nil
	}

	key := container.ID + "-" + gui.getContainerContexts()[gui.State.Panels.Containers.ContextIndex]
	if gui.State.Panels.Main.ObjectKey == key {
		return nil
	} else {
		gui.State.Panels.Main.ObjectKey = key
	}

	if err := gui.focusPoint(0, gui.State.Panels.Containers.SelectedLine, len(gui.DockerCommand.DisplayContainers), v); err != nil {
		return err
	}

	mainView := gui.getMainView()

	gui.clearMainView()

	switch gui.getContainerContexts()[gui.State.Panels.Containers.ContextIndex] {
	case "logs":
		if err := gui.renderContainerLogs(mainView, container); err != nil {
			return err
		}
	case "config":
		if err := gui.renderContainerConfig(mainView, container); err != nil {
			return err
		}
	case "stats":
		if err := gui.renderContainerStats(mainView, container); err != nil {
			return err
		}
	default:
		return errors.New("Unknown context for containers panel")
	}

	return nil
}

func (gui *Gui) renderContainerConfig(mainView *gocui.View, container *commands.Container) error {
	mainView.Autoscroll = false
	mainView.Title = "Config"

	data, err := json.MarshalIndent(&container.Details, "", "  ")
	if err != nil {
		return err
	}

	go gui.T.NewTask(func(stop chan struct{}) {
		gui.renderString(gui.g, "main", string(data))
	})

	return nil
}

func (gui *Gui) renderContainerStats(mainView *gocui.View, container *commands.Container) error {
	mainView.Autoscroll = false
	mainView.Title = "Stats"
	mainView.Wrap = false

	return gui.T.NewTickerTask(time.Second, func(stop chan struct{}) { gui.clearMainView() }, func(stop, notifyStopped chan struct{}) {
		width, _ := mainView.Size()

		contents, err := container.RenderStats(width)
		if err != nil {
			gui.createErrorPanel(gui.g, err.Error())
		}

		gui.reRenderString(gui.g, "main", contents)
	})
}

func (gui *Gui) renderContainerLogs(mainView *gocui.View, container *commands.Container) error {
	mainView.Autoscroll = true
	mainView.Title = "Logs"

	if container.Details.Config.OpenStdin {
		return gui.renderLogsForTTYContainer(container)
	}
	return gui.renderLogsForRegularContainer(container)
}

func (gui *Gui) renderLogsForRegularContainer(container *commands.Container) error {
	gui.renderLogs(container, gui.Config.UserConfig.CommandTemplates.ContainerLogs, func(cmd *exec.Cmd) {
		mainView := gui.getMainView()
		cmd.Stdout = mainView
		cmd.Stderr = mainView
	})

	return nil
}

func (gui *Gui) renderLogsForTTYContainer(container *commands.Container) error {
	gui.renderLogs(container, gui.Config.UserConfig.CommandTemplates.ContainerTTYLogs, func(cmd *exec.Cmd) {
		// for some reason just saying cmd.Stdout = mainView does not work here as it does for non-tty containers, so we feed it through line by line
		r, err := cmd.StdoutPipe()
		if err != nil {
			gui.ErrorChan <- err
		}

		go func() {
			mainView := gui.getMainView()
			s := bufio.NewScanner(r)
			s.Split(bufio.ScanLines)
			for s.Scan() {
				// I might put a check on the stopped channel here. Would mean more code duplication though
				mainView.Write(append(s.Bytes(), '\n'))
			}
		}()
	})

	return nil
}

func (gui *Gui) renderLogs(container *commands.Container, template string, setup func(*exec.Cmd)) {
	gui.T.NewTickerTask(time.Millisecond*200, nil, func(stop, notifyStopped chan struct{}) {
		gui.clearMainView()

		command := utils.ApplyTemplate(gui.Config.UserConfig.CommandTemplates.ContainerTTYLogs, container)
		cmd := gui.OSCommand.RunCustomCommand(command)

		setup(cmd)

		cmd.Start()

		go func() {
			<-stop
			cmd.Process.Kill()
			return
		}()

		cmd.Wait()

		// if we are here because the task has been stopped, we should return
		// if we are here then the container must have exited, meaning we should wait until it's back again before
	L:
		for {
			select {
			case <-stop:
				return
			default:
				result, err := gui.DockerCommand.Client.ContainerInspect(context.Background(), container.ID)
				if err != nil {
					// if we get an error, then the container has probably been removed so we'll get out of here
					gui.Log.Error(err)
					notifyStopped <- struct{}{}
					return
				}
				if result.State.Running {
					break L
				}
				time.Sleep(time.Millisecond * 100)
			}
		}
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

	if err := gui.refreshStateContainersAndServices(); err != nil {
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
				if err := gui.focusPoint(0, i, len(gui.DockerCommand.Services), gui.getServicesView()); err != nil {
					return err
				}
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

	gui.g.Update(func(g *gocui.Gui) error {
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

	return nil
}

func (gui *Gui) refreshStateContainersAndServices() error {
	return gui.DockerCommand.GetContainersAndServices()
}

func (gui *Gui) handleContainersNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.DisplayContainers), false)

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainersPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Containers
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.DisplayContainers), true)

	return gui.handleContainerSelect(gui.g, v)
}

func (gui *Gui) handleContainersPrevContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getContainerContexts()
	if gui.State.Panels.Containers.ContextIndex >= len(contexts)-1 {
		gui.State.Panels.Containers.ContextIndex = 0
	} else {
		gui.State.Panels.Containers.ContextIndex++
	}

	gui.handleContainerSelect(gui.g, v)

	return nil
}

func (gui *Gui) handleContainersNextContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getContainerContexts()
	if gui.State.Panels.Containers.ContextIndex <= 0 {
		gui.State.Panels.Containers.ContextIndex = len(contexts) - 1
	} else {
		gui.State.Panels.Containers.ContextIndex--
	}

	gui.handleContainerSelect(gui.g, v)

	return nil
}

type removeContainerOption struct {
	description   string
	command       string
	configOptions types.ContainerRemoveOptions
	runCommand    bool
}

// GetDisplayStrings is a function.
func (r *removeContainerOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description, color.New(color.FgRed).Sprint(r.command)}
}

func (gui *Gui) handleContainersRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
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
					return gui.createConfirmationPanel(gui.g, v, gui.Tr.Confirm, gui.Tr.MustForceToRemoveContainer, func(g *gocui.Gui, v *gocui.View) error {
						return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
							configOptions.Force = true
							if err := container.Remove(configOptions); err != nil {
								return err
							}
							return gui.refreshContainersAndServices()
						})
					}, nil)
				}
				return gui.createErrorPanel(gui.g, err.Error())
			}
			return gui.refreshContainersAndServices()
		})

	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handleContainerStop(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.g, v, gui.Tr.Confirm, gui.Tr.StopContainer, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			if err := container.Stop(); err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}

			return gui.refreshContainersAndServices()
		})

	}, nil)
}

func (gui *Gui) handleContainerRestart(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
		if err := container.Restart(); err != nil {
			return gui.createErrorPanel(gui.g, err.Error())
		}

		return gui.refreshContainersAndServices()
	})
}

func (gui *Gui) handleContainerAttach(g *gocui.Gui, v *gocui.View) error {
	container, err := gui.getSelectedContainer(g)
	if err != nil {
		return nil
	}

	c, err := container.Attach()
	if err != nil {
		return gui.createErrorPanel(gui.g, err.Error())
	}

	gui.SubProcess = c
	return gui.Errors.ErrSubProcess
}

func (gui *Gui) handlePruneContainers(g *gocui.Gui, v *gocui.View) error {
	return gui.createConfirmationPanel(gui.g, v, gui.Tr.Confirm, gui.Tr.ConfirmPruneContainers, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.PruningStatus, func() error {
			err := gui.DockerCommand.PruneContainers()
			if err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}
			return nil
		})
	}, nil)
}
