package gui

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

func (gui *Gui) getServicesPanel() *panels.SideListPanel[*commands.Service] {
	return &panels.SideListPanel[*commands.Service]{
		ContextState: &panels.ContextState[*commands.Service]{
			GetMainTabs: func() []panels.MainTab[*commands.Service] {
				return []panels.MainTab[*commands.Service]{
					{
						Key:    "logs",
						Title:  gui.Tr.LogsTitle,
						Render: gui.renderServiceLogs,
					},
					{
						Key:    "stats",
						Title:  gui.Tr.StatsTitle,
						Render: gui.renderServiceStats,
					},
					{
						Key:    "container-env",
						Title:  gui.Tr.ContainerEnvTitle,
						Render: gui.renderServiceContainerEnv,
					},
					{
						Key:    "container-config",
						Title:  gui.Tr.ContainerConfigTitle,
						Render: gui.renderServiceContainerConfig,
					},
					{
						Key:    "top",
						Title:  gui.Tr.TopTitle,
						Render: gui.renderServiceTop,
					},
				}
			},
			GetItemContextCacheKey: func(service *commands.Service) string {
				if service.Container == nil {
					return "services-" + service.ID
				}
				return "services-" + service.ID + "-" + service.Container.ID + "-" + service.Container.Container.State
			},
		},
		ListPanel: panels.ListPanel[*commands.Service]{
			List: panels.NewFilteredList[*commands.Service](),
			View: gui.Views.Services,
		},
		NoItemsMessage: gui.Tr.NoServices,
		Gui:            gui.intoInterface(),
		// sort services first by whether they have a linked container, and second by alphabetical order
		Sort: func(a *commands.Service, b *commands.Service) bool {
			if a.Container != nil && b.Container == nil {
				return true
			}

			if a.Container == nil && b.Container != nil {
				return false
			}

			return a.Name < b.Name
		},
		GetTableCells: func(service *commands.Service) []string {
			return presentation.GetServiceDisplayStrings(&gui.Config.UserConfig.Gui, service)
		},
		Hide: func() bool {
			return !gui.DockerCommand.InDockerComposeProject
		},
	}
}

func (gui *Gui) renderServiceContainerConfig(service *commands.Service) tasks.TaskFunc {
	if service.Container == nil {
		return gui.NewSimpleRenderStringTask(func() string { return gui.Tr.NoContainer })
	}

	return gui.renderContainerConfig(service.Container)
}

func (gui *Gui) renderServiceContainerEnv(service *commands.Service) tasks.TaskFunc {
	if service.Container == nil {
		return gui.NewSimpleRenderStringTask(func() string { return gui.Tr.NoContainer })
	}

	return gui.renderContainerEnv(service.Container)
}

func (gui *Gui) renderServiceStats(service *commands.Service) tasks.TaskFunc {
	if service.Container == nil {
		return gui.NewSimpleRenderStringTask(func() string { return gui.Tr.NoContainer })
	}

	return gui.renderContainerStats(service.Container)
}

func (gui *Gui) renderServiceTop(service *commands.Service) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			contents, err := service.RenderTop(ctx)
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

func (gui *Gui) renderServiceLogs(service *commands.Service) tasks.TaskFunc {
	if service.Container == nil {
		return gui.NewSimpleRenderStringTask(func() string { return gui.Tr.NoContainerForService })
	}

	return gui.renderContainerLogsToMain(service.Container)
}

type commandOption struct {
	description string
	command     string
	onPress     func() error
}

func (r *commandOption) getDisplayStrings() []string {
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
	}

	menuItems := lo.Map(options, func(option *commandOption, _ int) *types.MenuItem {
		return &types.MenuItem{
			LabelColumns: option.getDisplayStrings(),
			OnPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.RemovingStatus, func() error {
					if err := gui.OSCommand.RunCommand(option.command); err != nil {
						return gui.createErrorPanel(err.Error())
					}

					return nil
				})
			},
		}
	})

	return gui.Menu(CreateMenuOptions{
		Title: "",
		Items: menuItems,
	})
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
			onPress: func() error {
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
			onPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
					if err := gui.OSCommand.RunCommand(downWithVolumesCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		},
	}

	menuItems := lo.Map(options, func(option *commandOption, _ int) *types.MenuItem {
		return &types.MenuItem{
			LabelColumns: option.getDisplayStrings(),
			OnPress:      option.onPress,
		}
	})

	return gui.Menu(CreateMenuOptions{
		Title: "",
		Items: menuItems,
	})
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
			onPress: func() error {
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
			onPress: func() error {
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
			onPress: func() error {
				return gui.runSubprocess(gui.OSCommand.RunCustomCommand(rebuildCommand))
			},
		},
	}

	menuItems := lo.Map(options, func(option *commandOption, _ int) *types.MenuItem {
		return &types.MenuItem{
			LabelColumns: option.getDisplayStrings(),
			OnPress:      option.onPress,
		}
	})

	return gui.Menu(CreateMenuOptions{
		Title: "",
		Items: menuItems,
	})
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
