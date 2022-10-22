package gui

import (
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func (gui *Gui) getServicesPanel() *SideListPanel[*commands.Service] {
	return &SideListPanel[*commands.Service]{
		contextKeyPrefix: "services",
		ListPanel: ListPanel[*commands.Service]{
			list: NewFilteredList[*commands.Service](),
			view: gui.Views.Services,
		},
		contextIdx: 0,
		// TODO: i18n
		noItemsMessge: "no service selected",
		gui:           gui.intoInterface(),
		getContexts: func() []ContextConfig[*commands.Service] {
			return []ContextConfig[*commands.Service]{
				{
					key:    "logs",
					title:  gui.Tr.LogsTitle,
					render: gui.renderServiceLogs,
				},
				{
					key:    "stats",
					title:  gui.Tr.StatsTitle,
					render: gui.renderServiceStats,
				},
				{
					key:    "container-env",
					title:  gui.Tr.ContainerEnvTitle,
					render: gui.renderServiceContainerEnv,
				},
				{
					key:    "container-config",
					title:  gui.Tr.ContainerConfigTitle,
					render: gui.renderServiceContainerConfig,
				},
				{
					key:    "top",
					title:  gui.Tr.TopTitle,
					render: gui.renderServiceTop,
				},
			}
		},
		getSearchStrings: func(service *commands.Service) []string {
			// TODO: think about more things to search on
			return []string{service.Name}
		},
		getContextCacheKey: func(service *commands.Service) string {
			if service.Container == nil {
				return service.ID
			}
			return service.ID + "-" + service.Container.ID + "-" + service.Container.Container.State
		},
		// sort services first by whether they have a linked container, and second by alphabetical order
		sort: func(a *commands.Service, b *commands.Service) bool {
			if a.Container != nil && b.Container == nil {
				return true
			}

			if a.Container == nil && b.Container != nil {
				return false
			}

			return a.Name < b.Name
		},
	}
}

func (gui *Gui) renderServiceContainerConfig(service *commands.Service) error {
	if service.Container == nil {
		return gui.renderStringMain(gui.Tr.NoContainer)
	}

	return gui.renderContainerConfig(service.Container)
}

func (gui *Gui) renderServiceContainerEnv(service *commands.Service) error {
	if service.Container == nil {
		return gui.renderStringMain(gui.Tr.NoContainer)
	}

	return gui.renderContainerEnv(service.Container)
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
			gui.reRenderStringMain(err.Error())
		}

		gui.reRenderStringMain(contents)
	})
}

func (gui *Gui) renderServiceLogs(service *commands.Service) error {
	if service.Container == nil {
		return gui.T.NewTask(func(stop chan struct{}) {
			gui.clearMainView()
		})
	}

	return gui.renderContainerLogsToMain(service.Container)
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
	service, err := gui.Panels.Services.GetSelectedItem()
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

func (gui *Gui) handleServicePause(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}
	if service.Container == nil {
		return nil
	}

	return gui.PauseContainer(service.Container)
}

func (gui *Gui) handleServiceStop(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.StopService, func(g *gocui.Gui, v *gocui.View) error {
		return gui.WithWaitingStatus(gui.Tr.StoppingStatus, func() error {
			if err := service.Stop(); err != nil {
				return gui.createErrorPanel(err.Error())
			}

			return nil
		})
	}, nil)
}

func (gui *Gui) handleServiceUp(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.UppingServiceStatus, func() error {
		if err := service.Up(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}

func (gui *Gui) handleServiceRestart(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
		if err := service.Restart(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}

func (gui *Gui) handleServiceStart(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.StartingStatus, func() error {
		if err := service.Start(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}

func (gui *Gui) handleServiceAttach(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	if service.Container == nil {
		return gui.createErrorPanel(gui.Tr.NoContainers)
	}

	c, err := service.Attach()
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocess(c)
}

func (gui *Gui) handleServiceRenderLogsToMain(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	c, err := service.ViewLogs()
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocess(c)
}

func (gui *Gui) handleProjectUp(g *gocui.Gui, v *gocui.View) error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, gui.Tr.ConfirmUpProject, func(g *gocui.Gui, v *gocui.View) error {
		cmdStr := utils.ApplyTemplate(
			gui.Config.UserConfig.CommandTemplates.Up,
			gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
		)

		return gui.WithWaitingStatus(gui.Tr.UppingProjectStatus, func() error {
			if err := gui.OSCommand.RunCommand(cmdStr); err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return nil
		})
	}, nil)
}

func (gui *Gui) handleProjectDown(g *gocui.Gui, v *gocui.View) error {
	downCommand := utils.ApplyTemplate(
		gui.Config.UserConfig.CommandTemplates.Down,
		gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
	)

	downWithVolumesCommand := utils.ApplyTemplate(
		gui.Config.UserConfig.CommandTemplates.DownWithVolumes,
		gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
	)

	options := []*commandOption{
		{
			description: gui.Tr.Down,
			command:     downCommand,
			f: func() error {
				return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
					if err := gui.OSCommand.RunCommand(downCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		},
		{
			description: gui.Tr.DownWithVolumes,
			command:     downWithVolumesCommand,
			f: func() error {
				return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
					if err := gui.OSCommand.RunCommand(downWithVolumesCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
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

func (gui *Gui) handleServiceRestartMenu(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
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
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		},
		{
			description: gui.Tr.Recreate,
			command: utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.RecreateService,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Service: service}),
			),
			f: func() error {
				return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
					if err := gui.OSCommand.RunCommand(recreateCommand); err != nil {
						return gui.createErrorPanel(err.Error())
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
				return gui.runSubprocess(gui.OSCommand.RunCustomCommand(rebuildCommand))
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
				return gui.createErrorPanel(err.Error())
			}

			return nil
		})
	}

	return gui.createMenu("", options, len(options), handleMenuPress)
}

func (gui *Gui) handleServicesCustomCommand(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
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

func (gui *Gui) handleServicesExecShell(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	container := service.Container
	if container == nil {
		return gui.createErrorPanel(gui.Tr.NoContainers)
	}

	return gui.containerExecShell(container)
}

func (gui *Gui) handleServicesOpenInBrowserCommand(g *gocui.Gui, v *gocui.View) error {
	service, err := gui.Panels.Services.GetSelectedItem()
	if err != nil {
		return nil
	}

	container := service.Container
	if container == nil {
		return gui.createErrorPanel(gui.Tr.NoContainers)
	}

	return gui.openContainerInBrowser(container)
}
