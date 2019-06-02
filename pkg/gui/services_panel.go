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
	return []string{"logs", "config", "stats"}
}

func (gui *Gui) getSelectedService(g *gocui.Gui) (*commands.Service, error) {
	selectedLine := gui.State.Panels.Services.SelectedLine
	if selectedLine == -1 {
		return &commands.Service{}, errors.New("no service selected")
	}

	return gui.State.Services[selectedLine], nil
}

func (gui *Gui) handleServicesFocus(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	cx, cy := v.Cursor()
	_, oy := v.Origin()

	prevSelectedLine := gui.State.Panels.Services.SelectedLine
	newSelectedLine := cy - oy

	if newSelectedLine > len(gui.State.Services)-1 || len(utils.Decolorise(gui.State.Services[newSelectedLine].Name)) < cx {
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

	service, err := gui.getSelectedService(g)
	if err != nil {
		return err
	}

	key := service.ID + "-" + gui.getServiceContexts()[gui.State.Panels.Services.ContextIndex]
	if gui.State.Panels.Main.ObjectKey == key {
		return nil
	} else {
		gui.State.Panels.Main.ObjectKey = key
	}

	if err := gui.focusPoint(0, gui.State.Panels.Services.SelectedLine, len(gui.State.Services), v); err != nil {
		return err
	}

	mainView := gui.getMainView()

	mainView.Clear()
	mainView.SetOrigin(0, 0)
	mainView.SetCursor(0, 0)

	switch gui.getServiceContexts()[gui.State.Panels.Services.ContextIndex] {
	case "logs":
		if err := gui.renderServiceLogs(mainView, service); err != nil {
			return err
		}
	case "config":
		if err := gui.renderServiceConfig(mainView, service); err != nil {
			return err
		}
	case "stats":
		if err := gui.renderServiceStats(mainView, service); err != nil {
			return err
		}
	default:
		return errors.New("Unknown context for services panel")
	}

	return nil
}

func (gui *Gui) renderServiceConfig(mainView *gocui.View, service *commands.Service) error {
	mainView.Autoscroll = false
	mainView.Title = "Config"

	gui.T.NewTask(func(stop chan struct{}) {
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

func (gui *Gui) renderServiceStats(mainView *gocui.View, service *commands.Service) error {
	return nil
}

func (gui *Gui) renderServiceLogs(mainView *gocui.View, service *commands.Service) error {
	service, err := gui.getSelectedService(gui.g)
	if err != nil {
		return nil
	}

	if service.Container == nil {
		return nil
	}

	return gui.renderContainerLogs(gui.getMainView(), service.Container)
}

func (gui *Gui) handleServicesNextLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Services
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.State.Services), false)

	return gui.handleServiceSelect(gui.g, v)
}

func (gui *Gui) handleServicesPrevLine(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	panelState := gui.State.Panels.Services
	gui.changeSelectedLine(&panelState.SelectedLine, len(gui.State.Services), true)

	return gui.handleServiceSelect(gui.g, v)
}

func (gui *Gui) handleServicesPrevContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getServiceContexts()
	if gui.State.Panels.Services.ContextIndex >= len(contexts)-1 {
		gui.State.Panels.Services.ContextIndex = 0
	} else {
		gui.State.Panels.Services.ContextIndex++
	}

	gui.handleServiceSelect(gui.g, v)

	return nil
}

func (gui *Gui) handleServicesNextContext(g *gocui.Gui, v *gocui.View) error {
	contexts := gui.getServiceContexts()
	if gui.State.Panels.Services.ContextIndex <= 0 {
		gui.State.Panels.Services.ContextIndex = len(contexts) - 1
	} else {
		gui.State.Panels.Services.ContextIndex--
	}

	gui.handleServiceSelect(gui.g, v)

	return nil
}

type removeServiceOption struct {
	description string
	command     string
}

// GetDisplayStrings is a function.
func (r *removeServiceOption) GetDisplayStrings(isFocused bool) []string {
	return []string{r.description, color.New(color.FgRed).Sprint(r.command)}
}

func (gui *Gui) handleServiceRemoveMenu(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService(g)
	if err != nil {
		return nil
	}

	composeCommand := gui.Config.UserConfig.CommandTemplates.DockerCompose

	options := []*removeServiceOption{
		{
			description: gui.Tr.SLocalize("remove"),
			command:     fmt.Sprintf("%s rm --stop --force %s", composeCommand, service.Name),
		},
		{
			description: gui.Tr.SLocalize("removeWithVolumes"),
			command:     fmt.Sprintf("%s rm --stop --force -v %s", composeCommand, service.Name),
		},
		{
			description: gui.Tr.SLocalize("cancel"),
		},
	}

	handleMenuPress := func(index int) error {
		if options[index].command == "" {
			return nil
		}
		if err := gui.OSCommand.RunCommand(options[index].command); err != nil {
			return gui.createErrorPanel(gui.g, err.Error())
		}

		return gui.refreshContainersAndServices()
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handleServiceStop(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService(g)
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.g, v, gui.Tr.SLocalize("Confirm"), gui.Tr.SLocalize("StopService"), func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.SLocalize("StoppingStatus"), func() error {
			if err := service.Stop(); err != nil {
				return gui.createErrorPanel(gui.g, err.Error())
			}

			return gui.refreshContainersAndServices()
		})

	}, nil)
}

func (gui *Gui) handleServiceRestart(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService(g)
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.SLocalize("RestartingStatus"), func() error {
		if err := service.Restart(); err != nil {
			return gui.createErrorPanel(gui.g, err.Error())
		}

		return gui.refreshContainersAndServices()
	})
}

func (gui *Gui) handleServiceAttach(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.getSelectedService(g)
	if err != nil {
		return nil
	}

	c, err := service.Attach()
	if err != nil {
		return gui.createErrorPanel(gui.g, err.Error())
	}

	gui.SubProcess = c
	return gui.Errors.ErrSubProcess
}
