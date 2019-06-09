package gui

import (
	"encoding/json"
	"fmt"

	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// list panel functions

func (gui *Gui) getServiceContexts() []string {
	return []string{"logs", "stats", "config", "container-config"}
}

func (gui *Gui) getServiceContextTitles() []string {
	return []string{gui.Tr.LogsTitle, gui.Tr.StatsTitle, gui.Tr.ConfigTitle, gui.Tr.ContainerConfigTitle}
}

func (gui *Gui) getSelectedService() (*commands.Service, error) {
	selectedLine := gui.State.Panels.Services.SelectedLine
	if selectedLine == -1 {
		return &commands.Service{}, errors.New("no service selected")
	}

	return gui.DockerCommand.Services[selectedLine], nil
}

func (gui *Gui) handleServicesFocus(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	cx, cy := v.Cursor()
	_, oy := v.Origin()

	prevSelectedLine := gui.State.Panels.Services.SelectedLine
	newSelectedLine := cy - oy

	if newSelectedLine > len(gui.DockerCommand.Services)-1 || len(utils.Decolorise(gui.DockerCommand.Services[newSelectedLine].Name)) < cx {
		return gui.handleServiceSelect(gui.g, v)
	}

	gui.State.Panels.Services.SelectedLine = newSelectedLine

	if prevSelectedLine == newSelectedLine && gui.currentViewName() == v.Name() {
		return nil
	}

	return gui.handleServiceSelect(gui.g, v)
}

func (gui *Gui) handleServiceSelect(g *gocui.Gui, v *gocui.View) error {
	if _, err := gui.g.SetCurrentView(v.Name()); err != nil {
		return err
	}

	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	key := service.ID + "-" + gui.getServiceContexts()[gui.State.Panels.Services.ContextIndex]
	if gui.State.Panels.Main.ObjectKey == key {
		return nil
	} else {
		gui.State.Panels.Main.ObjectKey = key
	}

	if err := gui.focusPoint(0, gui.State.Panels.Services.SelectedLine, len(gui.DockerCommand.Services), v); err != nil {
		return err
	}

	mainView := gui.getMainView()

	mainView.Tabs = gui.getServiceContextTitles()
	mainView.TabIndex = gui.State.Panels.Services.ContextIndex

	switch gui.getServiceContexts()[gui.State.Panels.Services.ContextIndex] {
	case "logs":
		if err := gui.renderServiceLogs(service); err != nil {
			return err
		}
	case "config":
		if err := gui.renderServiceConfig(service); err != nil {
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
	default:
		return errors.New("Unknown context for services panel")
	}

	return nil
}

func (gui *Gui) renderServiceConfig(service *commands.Service) error {
	mainView := gui.getMainView()
	mainView.Autoscroll = false
	mainView.Wrap = true

	go gui.T.NewTask(func(stop chan struct{}) {
		// TODO: actually show service config
		data, err := json.MarshalIndent(&service.Container.Container, "", "  ")
		if err != nil {
			gui.Log.Error(err)
			return
		}
		gui.renderString(gui.g, "main", string(data))
	})

	return nil
}

func (gui *Gui) renderServiceStats(service *commands.Service) error {
	if service.Container == nil {
		return nil
	}

	return gui.renderContainerStats(service.Container)
}

func (gui *Gui) renderServiceLogs(service *commands.Service) error {
	service, err := gui.getSelectedService()
	if err != nil {
		return nil
	}

	if service.Container == nil {
		go gui.T.NewTask(func(stop chan struct{}) {
			gui.clearMainView()
		})
		return nil
	}

	return gui.renderContainerLogs(service.Container)
}

func (gui *Gui) handleServicesNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Services
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.DockerCommand.Services), false)

	return gui.handleServiceSelect(gui.g, v)
}

func (gui *Gui) handleServicesPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
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

	return gui.createCommandMenu(options, gui.Tr.RemovingStatus)
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

	rebuildCommand := utils.ApplyTemplate(gui.Config.UserConfig.CommandTemplates.RebuildService, service)

	options := []*commandOption{
		{
			description: gui.Tr.Restart,
			command:     utils.ApplyTemplate(gui.Config.UserConfig.CommandTemplates.RestartService, service),
			f: func() error {
				return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
					if err := service.Restart(); err != nil {
						return gui.createErrorPanel(gui.g, err.Error())
					}

					return gui.refreshContainersAndServices()
				})
			},
		},
		{
			description: gui.Tr.Rebuild,
			command:     utils.ApplyTemplate(gui.Config.UserConfig.CommandTemplates.RebuildService, service),
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

func (gui *Gui) createCommandMenu(options []*commandOption, status string) error {
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
