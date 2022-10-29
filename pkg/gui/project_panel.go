package gui

import (
	"bytes"
	"path"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/jesseduffield/yaml"
)

// Although at the moment we'll only have one project, in future we could have
// a list of projects in the project panel.

func (gui *Gui) getProjectPanel() *panels.SideListPanel[*commands.Project] {
	return &panels.SideListPanel[*commands.Project]{
		ContextState: &panels.ContextState[*commands.Project]{
			GetContexts: func() []panels.ContextConfig[*commands.Project] {
				if gui.DockerCommand.InDockerComposeProject {
					return []panels.ContextConfig[*commands.Project]{
						{
							Key:    "logs",
							Title:  gui.Tr.LogsTitle,
							Render: gui.renderAllLogs,
						},
						{
							Key:    "config",
							Title:  gui.Tr.DockerComposeConfigTitle,
							Render: gui.renderDockerComposeConfig,
						},
						{
							Key:    "credits",
							Title:  gui.Tr.CreditsTitle,
							Render: gui.renderCredits,
						},
					}
				}

				return []panels.ContextConfig[*commands.Project]{
					{
						Key:    "credits",
						Title:  gui.Tr.CreditsTitle,
						Render: gui.renderCredits,
					},
				}
			},
			GetContextCacheKey: func(project *commands.Project) string {
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
		GetDisplayStrings: presentation.GetProjectDisplayStrings,
		// It doesn't make sense to filter a list of only one item.
		DisableFilter: true,
	}
}

func (gui *Gui) refreshProject() error {
	gui.Panels.Projects.SetItems([]*commands.Project{{Name: gui.getProjectName()}})
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

func (gui *Gui) renderCredits(_project *commands.Project) error {
	return gui.T.NewTask(func(stop chan struct{}) {
		mainView := gui.Views.Main
		mainView.Autoscroll = false
		mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

		var configBuf bytes.Buffer
		_ = yaml.NewEncoder(&configBuf, yaml.IncludeOmitted).Encode(gui.Config.UserConfig)

		dashboardString := strings.Join(
			[]string{
				lazydockerTitle(),
				"Copyright (c) 2019 Jesse Duffield",
				"Keybindings: https://github.com/jesseduffield/lazydocker/blob/master/docs/keybindings",
				"Config Options: https://github.com/jesseduffield/lazydocker/blob/master/docs/Config.md",
				"Raise an Issue: https://github.com/jesseduffield/lazydocker/issues",
				utils.ColoredString("Buy Jesse a coffee: https://github.com/sponsors/jesseduffield", color.FgMagenta), // caffeine ain't free
				"Here's your lazydocker config when merged in with the defaults (you can open your config by pressing 'o'):",
				configBuf.String(),
			}, "\n\n")

		_ = gui.RenderStringMain(dashboardString)
	})
}

func (gui *Gui) renderAllLogs(_project *commands.Project) error {
	return gui.T.NewTask(func(stop chan struct{}) {
		mainView := gui.Views.Main
		mainView.Autoscroll = true
		mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

		gui.clearMainView()

		cmd := gui.OSCommand.RunCustomCommand(
			utils.ApplyTemplate(
				gui.Config.UserConfig.CommandTemplates.AllLogs,
				gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
			),
		)

		cmd.Stdout = mainView
		cmd.Stderr = mainView

		gui.OSCommand.PrepareForChildren(cmd)
		_ = cmd.Start()

		go func() {
			<-stop
			if err := gui.OSCommand.Kill(cmd); err != nil {
				gui.Log.Error(err)
			}
		}()

		_ = cmd.Wait()
	})
}

func (gui *Gui) renderDockerComposeConfig(_project *commands.Project) error {
	return gui.T.NewTask(func(stop chan struct{}) {
		mainView := gui.Views.Main
		mainView.Autoscroll = false
		mainView.Wrap = gui.Config.UserConfig.Gui.WrapMainPanel

		config := gui.DockerCommand.DockerComposeConfig()
		_ = gui.RenderStringMain(config)
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
