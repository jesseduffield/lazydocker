package gui

import (
	"bytes"
	"context"
	"github.com/docker/docker/api/types/container"
	"github.com/peauc/lazydocker-ng/pkg/gui/types"
	"log"
	"path"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/peauc/lazydocker-ng/pkg/commands"
	"github.com/peauc/lazydocker-ng/pkg/gui/panels"
	"github.com/peauc/lazydocker-ng/pkg/gui/presentation"
	"github.com/peauc/lazydocker-ng/pkg/tasks"
	"github.com/peauc/lazydocker-ng/pkg/utils"
	"github.com/jesseduffield/yaml"
)

// Although at the moment we'll only have one project, in future we could have
// a list of projects in the project panel.

func (gui *Gui) getProjectPanel() *panels.SideListPanel[*commands.Project] {
	return &panels.SideListPanel[*commands.Project]{
		ContextState: &panels.ContextState[*commands.Project]{
			GetMainTabs: func() []panels.MainTab[*commands.Project] {
				if gui.DockerCommand.InDockerComposeProject {
					return []panels.MainTab[*commands.Project]{
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

				return []panels.MainTab[*commands.Project]{
					{
						Key:    "credits",
						Title:  gui.Tr.CreditsTitle,
						Render: gui.renderCredits,
					},
				}
			},
			GetItemContextCacheKey: func(project *commands.Project) string {
				return "projects-" + project.Name
			},
		},

		ListPanel: panels.ListPanel[*commands.Project]{
			List: panels.NewFilteredList[*commands.Project](),
			View: gui.Views.Project,
		},
		NoItemsMessage: "No docker compose projects found.",
		Gui:            gui.intoInterface(),

		Sort: func(a *commands.Project, b *commands.Project) bool {
			return (gui.State.Project != nil && gui.State.Project.Name == a.Name) || a.Name < b.Name
		},
		GetTableCells: presentation.GetProjectDisplayStrings,
	}
}

func (gui *Gui) refreshProjects() error {
	containers, err := gui.DockerCommand.Client.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return err
	}

	projectsMap := make(map[string]*commands.Project)

	for _, container := range containers {
		if projectName, exists := container.Labels["com.docker.compose.project"]; exists && projectName != "" {
			projectsMap[projectName] = &commands.Project{Name: projectName}
		}
	}

	projectsList := make([]*commands.Project, 0, len(projectsMap))
	for _, project := range projectsMap {
		projectsList = append(projectsList, project)
	}

	// Add current's folder project if exists
	if gui.DockerCommand.InDockerComposeProject {
		projectsList = append(projectsList, &commands.Project{Name: gui.GetProjectName()})
	}
	gui.Panels.Projects.SetItems(projectsList)

	return gui.Panels.Projects.RerenderList()
}

func (gui *Gui) GetProjectName() string {
	if gui.State.Project != nil {
		return gui.State.Project.Name
	}

	// Default to the command line argument
	return path.Base(gui.Config.ProjectDir)
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
			"Keybindings: https://github.com/peauc/lazydocker-ng/blob/master/docs/keybindings",
			"Config Options: https://github.com/peauc/lazydocker-ng/blob/master/docs/Config.md",
			"Raise an Issue: https://github.com/peauc/lazydocker-ng/issues",
			utils.ColoredString("Buy Jesse a coffee: https://github.com/sponsors/jesseduffield", color.FgMagenta), // caffeine ain't free
			"Here's your lazydocker config when merged in with the defaults (you can open your config by pressing 'o'):",
			utils.ColoredYamlString(configBuf.String()),
		}, "\n\n")
}

func (gui *Gui) renderAllLogs(_project *commands.Project) tasks.TaskFunc {
	return gui.NewTask(TaskOpts{
		Autoscroll: true,
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Func: func(ctx context.Context) {
			gui.clearMainView()

			cmd := gui.OSCommand.RunCustomCommand(
				utils.ApplyTemplate(
					gui.Config.UserConfig.CommandTemplates.AllLogs,
					gui.DockerCommand.NewCommandObject(commands.CommandObject{}),
				),
			)

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

func (gui *Gui) handleCreateProjectMenu(g *gocui.Gui, v *gocui.View) error {
	if gui.isPopupPanel(v.Name()) {
		return nil
	}

	testMenuItem := &types.MenuItem{
		LabelColumns: []string{"t", "test"},
		OnPress: func() error {
			log.Println("tested")
			return nil
		},
	}

	menuItems := []*types.MenuItem{testMenuItem}

	return gui.Menu(CreateMenuOptions{
		Title:      gui.Tr.MenuTitle,
		Items:      menuItems,
		HideCancel: true,
	})
}
