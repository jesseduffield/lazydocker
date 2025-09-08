package gui

import (
	"bytes"
	"context"
	"os/exec"
	"path"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/jesseduffield/yaml"
	"github.com/samber/lo"
)

// Although at the moment we'll only have one project, in future we could have
// a list of projects in the project panel.

func (gui *Gui) getProjectPanel() *panels.SideListPanel[*commands.Project] {
	return &panels.SideListPanel[*commands.Project]{
		ContextState: &panels.ContextState[*commands.Project]{
			GetMainTabs: func() []panels.MainTab[*commands.Project] {
				tabsList := []panels.MainTab[*commands.Project]{
					{
						Key:    "credits",
						Title:  gui.Tr.CreditsTitle,
						Render: gui.renderCredits,
					},
				}

				if gui.DockerCommand.InDockerComposeProject {
					tabsListCompose := []panels.MainTab[*commands.Project]{
						{
							Key:    "logs",
							Title:  gui.Tr.LogsTitle,
							Render: gui.renderLogs,
						},
						{
							Key:    "config",
							Title:  gui.Tr.DockerComposeConfigTitle,
							Render: gui.renderDockerComposeConfig,
						},
					}
					tabsList = append(tabsListCompose, tabsList...)
				}

				return tabsList
			},
			GetItemContextCacheKey: func(project *commands.Project) string {
				return "projects-" + project.Name
			},
		},

		ListPanel: panels.ListPanel[*commands.Project]{
			List: panels.NewFilteredList[*commands.Project](),
			View: gui.Views.Project,
		},
		NoItemsMessage: "",
		Gui:            gui.intoInterface(),

		Sort: func(a *commands.Project, b *commands.Project) bool {
			return false
		},
		GetTableCells: presentation.GetProjectDisplayStrings,
		DisableFilter: true,
	}
}

func (gui *Gui) refreshProject() error {
	projectName := gui.getProjectName()
	profiles, err := gui.DockerCommand.GetProfiles()
	if err != nil {
		return err
	}

	gui.DockerCommand.HasProfiles = len(profiles) > 0
	items := []*commands.Project{{
		Name:          projectName,
		IsProfile:     false,
		Config:        gui.Config,
		OSCommand:     gui.OSCommand,
		DockerCommand: gui.DockerCommand,
	}}
	for _, profile := range profiles {
		items = append(items, &commands.Project{
			Name:          profile,
			IsProfile:     true,
			Config:        gui.Config,
			OSCommand:     gui.OSCommand,
			DockerCommand: gui.DockerCommand,
		})
	}

	gui.Panels.Projects.SetItems(items)
	return gui.Panels.Projects.RerenderList()
}

func (gui *Gui) getProjectName() string {
	projectName := path.Base(gui.Config.ProjectDir)
	if gui.DockerCommand.InDockerComposeProject {
		for _, service := range gui.Panels.Services.List.GetAllItems() {
			container := service.Container
			if container != nil && container.DetailsLoaded() {
				return container.Details.Config.Labels["com.docker.compose.project"]
			}
		}
	}

	return projectName
}

func (gui *Gui) renderCredits(_project *commands.Project) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.creditsStr() })
}

func (gui *Gui) creditsStr() string {
	var configBuf bytes.Buffer
	_ = yaml.NewEncoder(&configBuf, yaml.IncludeOmitted).Encode(gui.Config.UserConfig)

	return strings.Join(
		[]string{
			lazydockerTitle(),
			"Copyright (c) 2019 Jesse Duffield",
			"Keybindings: https://github.com/jesseduffield/lazydocker/blob/master/docs/keybindings",
			"Config Options: https://github.com/jesseduffield/lazydocker/blob/master/docs/Config.md",
			"Raise an Issue: https://github.com/jesseduffield/lazydocker/issues",
			utils.ColoredString("Buy Jesse a coffee: https://github.com/sponsors/jesseduffield", color.FgMagenta), // caffeine ain't free
			"Here's your lazydocker config when merged in with the defaults (you can open your config by pressing 'o'):",
			utils.ColoredYamlString(configBuf.String()),
		}, "\n\n")
}

func (gui *Gui) renderLogs(_project *commands.Project) tasks.TaskFunc {
	return gui.NewTask(TaskOpts{
		Autoscroll: true,
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Func: func(ctx context.Context) {
			gui.clearMainView()

			var cmd *exec.Cmd
			if _project.IsProfile {
				cmd = gui.OSCommand.RunCustomCommand(
					utils.ApplyTemplate(
						gui.Config.UserConfig.CommandTemplates.AllLogsProfile,
						gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: _project.Name}),
					),
				)
			} else {
				cmd = gui.OSCommand.RunCustomCommand(
					utils.ApplyTemplate(
						gui.Config.UserConfig.CommandTemplates.AllLogs,
						gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
					),
				)
			}

			cmd.Stdout = gui.Views.Main
			cmd.Stderr = gui.Views.Main

			gui.OSCommand.PrepareForChildren(cmd)
			_ = cmd.Start()

			go func() {
				<-ctx.Done()
				if err := gui.OSCommand.Kill(cmd); err != nil {
					gui.Log.Error(err)
				}
			}()

			_ = cmd.Wait()
		},
	})
}

func (gui *Gui) renderDockerComposeConfig(_project *commands.Project) tasks.TaskFunc {
	if _project.IsProfile {
		return gui.NewSimpleRenderStringTask(func() string {
			return utils.ColoredYamlString(gui.DockerCommand.DockerComposeProfileConfig(_project.Name))
		})
	}
	return gui.NewSimpleRenderStringTask(func() string {
		return utils.ColoredYamlString(gui.DockerCommand.DockerComposeConfig())
	})
}

func (gui *Gui) handleOpenConfig(g *gocui.Gui, v *gocui.View) error {
	return gui.openFile(gui.Config.ConfigFilename())
}

func (gui *Gui) handleEditConfig(g *gocui.Gui, v *gocui.View) error {
	return gui.editFile(gui.Config.ConfigFilename())
}

func lazydockerTitle() string {
	return `
   _                     _            _
  | |                   | |          | |
  | | __ _ _____   _  __| | ___   ___| | _____ _ __
  | |/ _` + "`" + ` |_  / | | |/ _` + "`" + ` |/ _ \ / __| |/ / _ \ '__|
  | | (_| |/ /| |_| | (_| | (_) | (__|   <  __/ |
  |_|\__,_/___|\__, |\__,_|\___/ \___|_|\_\___|_|
                __/ |
               |___/
`
}

// handleViewAllLogs switches to a subprocess viewing all the logs from docker-compose
func (gui *Gui) handleViewAllLogs(g *gocui.Gui, v *gocui.View) error {
	c, err := gui.DockerCommand.ViewAllLogs()
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocess(c)
}

func (gui *Gui) handleProjectUpMenu(g *gocui.Gui, v *gocui.View) error {
	upCommand := utils.ApplyTemplate(
		gui.Config.UserConfig.CommandTemplates.Up,
		gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
	)
	options := []*commandOption{
		{
			description: gui.Tr.UpProject,
			command:     upCommand,
			onPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.UppingProjectStatus, func() error {
					if err := gui.OSCommand.RunCommand(upCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		},
	}

	if gui.DockerCommand.HasProfiles {
		upAllProfilesCommand := utils.ApplyTemplate(
			gui.Config.UserConfig.CommandTemplates.UpProfile,
			gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: "*"}),
		)
		options = append(options, &commandOption{
			description: gui.Tr.UpAllProfiles,
			command:     upAllProfilesCommand,
			onPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.UppingProjectStatus, func() error {
					if err := gui.OSCommand.RunCommand(upAllProfilesCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		})

		profile, err := gui.Panels.Projects.GetSelectedItem()
		if err == nil && profile.IsProfile {
			upProfileCommand := utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.UpProfile,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: profile.Name}),
			)
			options = append(options, &commandOption{
				description: gui.Tr.UpProfile,
				command:     upProfileCommand,
				onPress: func() error {
					return gui.WithWaitingStatus(gui.Tr.UppingProjectStatus, func() error {
						if err := gui.OSCommand.RunCommand(upProfileCommand); err != nil {
							return gui.createErrorPanel(err.Error())
						}
						return nil
					})
				},
			})
		}
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

func (gui *Gui) handleProjectDownMenu(g *gocui.Gui, v *gocui.View) error {
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

	if gui.DockerCommand.HasProfiles {
		downProfileCommand := utils.ApplyTemplate(
			gui.Config.UserConfig.CommandTemplates.DownProfile,
			gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: "*"}),
		)
		downProfileWithVolumesCommand := utils.ApplyTemplate(
			gui.Config.UserConfig.CommandTemplates.DownProfileWithVolumes,
			gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: "*"}),
		)
		options = append(options, &commandOption{
			description: gui.Tr.DownAllProfiles,
			command:     downProfileCommand,
			onPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
					if err := gui.OSCommand.RunCommand(downProfileCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		}, &commandOption{
			description: gui.Tr.DownAllProfilesWithVolumes,
			command:     downProfileWithVolumesCommand,
			onPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
					if err := gui.OSCommand.RunCommand(downProfileWithVolumesCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		})

		profile, err := gui.Panels.Projects.GetSelectedItem()
		if err == nil && profile.IsProfile {
			downProfileCommand := utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.DownProfile,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: profile.Name}),
			)
			downProfileWithVolumesCommand := utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.DownProfileWithVolumes,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: profile.Name}),
			)
			options = append(options, &commandOption{
				description: gui.Tr.DownProfile,
				command:     downProfileCommand,
				onPress: func() error {
					return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
						if err := gui.OSCommand.RunCommand(downProfileCommand); err != nil {
							return gui.createErrorPanel(err.Error())
						}
						return nil
					})
				},
			}, &commandOption{
				description: gui.Tr.DownProfileWithVolumes,
				command:     downProfileWithVolumesCommand,
				onPress: func() error {
					return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
						if err := gui.OSCommand.RunCommand(downProfileWithVolumesCommand); err != nil {
							return gui.createErrorPanel(err.Error())
						}
						return nil
					})
				},
			})
		}
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

func (gui *Gui) handleProjectRestartMenu(g *gocui.Gui, v *gocui.View) error {
	restartCommand := utils.ApplyTemplate(
		gui.Config.UserConfig.CommandTemplates.Restart,
		gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
	)
	options := []*commandOption{
		{
			description: gui.Tr.Restart,
			command:     restartCommand,
			onPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
					if err := gui.OSCommand.RunCommand(restartCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		},
	}

	if gui.DockerCommand.HasProfiles {
		restartProfileCommand := utils.ApplyTemplate(
			gui.Config.UserConfig.CommandTemplates.RestartProfile,
			gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: "*"}),
		)
		options = append(options, &commandOption{
			description: gui.Tr.Restart,
			command:     restartProfileCommand,
			onPress: func() error {
				return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
					if err := gui.OSCommand.RunCommand(restartProfileCommand); err != nil {
						return gui.createErrorPanel(err.Error())
					}
					return nil
				})
			},
		})

		profile, err := gui.Panels.Projects.GetSelectedItem()
		if err == nil && profile.IsProfile {
			restartProfileCommand := utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.RestartProfile,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{Profile: profile.Name}),
			)
			options = append(options, &commandOption{
				description: gui.Tr.Restart,
				command:     restartProfileCommand,
				onPress: func() error {
					return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
						if err := gui.OSCommand.RunCommand(restartProfileCommand); err != nil {
							return gui.createErrorPanel(err.Error())
						}
						return nil
					})
				},
			})
		}
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

func (gui *Gui) handleProjectUp(g *gocui.Gui, v *gocui.View) error {
	project, err := gui.Panels.Projects.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.UppingProjectStatus, func() error {
		if err := project.Up(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}

func (gui *Gui) handleProjectDown(g *gocui.Gui, v *gocui.View) error {
	project, err := gui.Panels.Projects.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.DowningStatus, func() error {
		if err := project.Down(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}

func (gui *Gui) handleProjectRestart(g *gocui.Gui, v *gocui.View) error {
	project, err := gui.Panels.Projects.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.WithWaitingStatus(gui.Tr.RestartingStatus, func() error {
		if err := project.Restart(); err != nil {
			return gui.createErrorPanel(err.Error())
		}

		return nil
	})
}
