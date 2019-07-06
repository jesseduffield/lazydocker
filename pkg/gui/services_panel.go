package gui

import (
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// list panel functions

func (gui *Gui) getServiceContexts() []string {
	return []string{"logs", "stats", "container-config", "top"}
}

func (gui *Gui) getServiceContextTitles() []string {
	return []string{gui.Tr.LogsTitle, gui.Tr.StatsTitle, gui.Tr.ContainerConfigTitle, gui.Tr.TopTitle}
}

func (gui *Gui) getSelectedService() (*commands.Service, error) {
	selectedLine := gui.State.Panels.Services.SelectedLine
	if selectedLine == -1 {
		return &commands.Service{}, errors.New("no service selected")
	}

	return gui.DockerCommand.Services[selectedLine], nil
}

func (gui *Gui) handleServicesClick(g *gocui.Gui, v *gocui.View) error {
	itemCount := len(gui.DockerCommand.Services)
	handleSelect := gui.handleServiceSelect
	selectedLine := &gui.State.Panels.Services.SelectedLine

	return gui.handleClick(v, itemCount, selectedLine, handleSelect)
}

func (gui *Gui) handleServiceSelect(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	containerID := ""
	if service.Container != nil {
		containerID = service.Container.ID
	}

	if err := gui.focusPoint(0, gui.State.Panels.Services.SelectedLine, len(gui.DockerCommand.Services), v); err != nil {
		return err
	}

	key := "services-" + service.ID + "-" + containerID + "-" + gui.getServiceContexts()[gui.State.Panels.Services.ContextIndex]
	if !gui.shouldRefresh(key) {
		return nil
	}

	mainView := gui.getMainView()

	mainView.Tabs = gui.getServiceContextTitles()
	mainView.TabIndex = gui.State.Panels.Services.ContextIndex

	switch gui.getServiceContexts()[gui.State.Panels.Services.ContextIndex] {
	case "logs":
		if err := gui.renderServiceLogs(service); err != nil {
			return err
		}
	case "stats":
		if err := gui.renderServiceStats(service); err != nil {
			return err
		}
	case "container-config":
		if service.Container == nil {
			return gui.renderString(gui.g, "main", gui.Tr.NoContainer)
		}
		if err := gui.renderContainerConfig(service.Container); err != nil {
			return err
		}
	case "top":
		if err := gui.renderServiceTop(service); err != nil {
			return err
		}
	default:
		return errors.New("Unknown context for services panel")
	}

	return nil
}

func (gui *Gui) renderServiceStats(service *commands.Service) error {
	if service.Container == nil {
		return nil
	}

	return gui.renderContainerStats(service.Container)
}

func (gui *Gui) renderServiceTop(service *commands.Service) error {
	mainView := gui.getMainView()
	mainView.Autoscroll = false
	mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

	return gui.T.NewTickerTask(time.Second, func(stop chan struct{}) { gui.clearMainView() }, func(stop, notifyStopped chan struct{}) {
		contents, err := service.RenderTop()
		if err != nil {
			gui.reRenderString(gui.g, "main", err.Error())
		}

		gui.reRenderString(gui.g, "main", contents)
	})
}

func (gui *Gui) renderServiceLogs(service *commands.Service) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	if service.Container == nil {
		return gui.T.NewTask(func(stop chan struct{}) {
			gui.clearMainView()
		})
	}

	return gui.renderContainerLogs(service.Container)
}

func (gui *Gui) handleServicesNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() || gui.g.CurrentView() != v {
		return nil
	}

	panelState := gui.State.Panels.Services
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.Services), false)

	return gui.handleServiceSelect(gui.g, v)
}

func (gui *Gui) handleServicesPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() || gui.g.CurrentView() != v {
		return nil
	}

	panelState := gui.State.Panels.Services
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.Services), true)

	return gui.handleServiceSelect(gui.g, v)
}

func (gui *Gui) handleServicesNextContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getServiceContexts()
	if gui.State.Panels.Services.ContextIndex >= len(contexts)-1 {
		gui.State.Panels.Services.ContextIndex = 0
	} else {
		gui.State.Panels.Services.ContextIndex++
	}

	gui.handleServiceSelect(gui.g, v)

	return nil
}

func (gui *Gui) handleServicesPrevContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getServiceContexts()
	if gui.State.Panels.Services.ContextIndex <= 0 {
		gui.State.Panels.Services.ContextIndex = len(contexts) - 1
	} else {
		gui.State.Panels.Services.ContextIndex--
	}

	gui.handleServiceSelect(gui.g, v)

	return nil
}

type commandOption struct {
	description string
	command     string
	f           func() error
}

// GetDisplayStrings is a function.
func (r *commandOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description, color.New(color.FgCyan).Sprint(r.command)}
}

func (gui *Gui) handleServiceRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	composeCommand := gui.Config.UserConfig.CommandTemplates.DockerCompose

	options := []*commandOption{
		{
			description: gui.Tr.Remove,
			command:     fmt.Sprintf("%s rm --stop --force %s", composeCommand, service.Name),
		},
		{
			description: gui.Tr.RemoveWithVolumes,
			command:     fmt.Sprintf("%s rm --stop --force -v %s", composeCommand, service.Name),
		},
		{
			description: gui.Tr.Cancel,
		},
	}

	return gui.createServiceCommandMenu(options, gui.Tr.RemovingStatus)
}

func (gui *Gui) handleServiceStop(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.g, v, gui.Tr.Confirm, gui.Tr.StopService, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			if err := service.Stop(); err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}

			return gui.refreshContainersAndServices()
		})

	}, nil)
}

func (gui *Gui) handleServiceRestart(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
		if err := service.Restart(); err != nil {
			return gui.createErrorPanel(gui.g, err.Error())
		}

		return gui.refreshContainersAndServices()
	})
}

func (gui *Gui) handleServiceAttach(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	if service.Container == nil {
		return gui.createErrorPanel(gui.g, gui.Tr.NoContainers)
	}

	c, err := service.Attach()

	if err != nil {
		return gui.createErrorPanel(gui.g, err.Error())
	}

	gui.SubProcess = c
	return gui.Errors.ErrSubProcess
}

func (gui *Gui) handleServiceViewLogs(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	c, err := service.ViewLogs()
	if err != nil {
		return gui.createErrorPanel(gui.g, err.Error())
	}

	gui.SubProcess = c
	return gui.Errors.ErrSubProcess
}

func (gui *Gui) handleServiceRestartMenu(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	rebuildCommand := utils.ApplyTemplate(
		gui.Config.UserConfig.CommandTemplates.RebuildService,
		gui.DockerCommand.NewCommandObject(commands.CommandObject{Service: service}),
	)

	recreateCommand := utils.ApplyTemplate(
		gui.Config.UserConfig.CommandTemplates.RecreateService,
		gui.DockerCommand.NewCommandObject(commands.CommandObject{Service: service}),
	)

	options := []*commandOption{
		{
			description: gui.Tr.Restart,
			command: utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.RestartService,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Service: service}),
			),
			f: func() error {
				return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
					if err := service.Restart(); err != nil {
						return gui.createErrorPanel(gui.g, err.Error())
					}
					return nil
				})
			},
		}, {
			description: gui.Tr.Recreate,
			command: utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.RecreateService,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Service: service}),
			),
			f: func() error {
				return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
					if err := gui.OSCommand.RunCommand(recreateCommand); err != nil {
						return gui.createErrorPanel(gui.g, err.Error())
					}
					return nil
				})
			},
		},
		{
			description: gui.Tr.Rebuild,
			command: utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.RebuildService,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Service: service}),
			),
			f: func() error {
				gui.SubProcess = gui.OSCommand.RunCustomCommand(rebuildCommand)
				return gui.Errors.ErrSubProcess
			},
		},
		{
			description: gui.Tr.Cancel,
			f:           func() error { return nil },
		},
	}

	handleMenuPress := func(index int) error { return options[index].f() }

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) createServiceCommandMenu(options []*commandOption, status string) error {
	handleMenuPress := func(index int) error {
		if options[index].command == "" {
			return nil
		}
		return gui.WithWaitingStatus(status, func() error {
			if err := gui.OSCommand.RunCommand(options[index].command); err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}

			return nil
		})
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handleServicesCustomCommand(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{
		Service:   service,
		Container: service.Container,
	})

	var customCommands []config.CustomCommand

	customServiceCommands := gui.Config.UserConfig.CustomCommands.Services
	// we only include service commands if they have no serviceNames defined or if our service happens to be one of the serviceNames defined
L:
	for _, cmd := range customServiceCommands {
		if len(cmd.ServiceNames) == 0 {
			customCommands = append(customCommands, cmd)
			continue L
		}
		for _, serviceName := range cmd.ServiceNames {
			if serviceName == service.Name {
				// appending these to the top given they're more likely to be selected
				customCommands = append([]config.CustomCommand{cmd}, customCommands...)
				continue L
			}
		}
	}

	if service.Container != nil {
		customCommands = append(customCommands, gui.Config.UserConfig.CustomCommands.Containers...)
	}

	return gui.createCustomCommandMenu(customCommands, commandObject)
}

func (gui *Gui) handleServicesBulkCommand(g *gocui.Gui, v *gocui.View) error {
	bulkCommands := gui.Config.UserConfig.BulkCommands.Services
	commandObject := gui.DockerCommand.NewCommandObject(commands.CommandObject{})

	return gui.createBulkCommandMenu(bulkCommands, commandObject)
}
