package gui

import (
	"bytes"
	"context"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/presentation"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/jesseduffield/yaml"
)

func (gui *Gui) getProjectPanel() *panels.SideListPanel[*commands.Project] {
	return &panels.SideListPanel[*commands.Project]{
		ContextState: &panels.ContextState[*commands.Project]{
			GetMainTabs: func() []panels.MainTab[*commands.Project] {
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
			// Projects first (alphabetical), then profiles (alphabetical).
			if a.IsProfile != b.IsProfile {
				return !a.IsProfile
			}
			return a.Name < b.Name
		},
		GetTableCells: presentation.GetProjectDisplayStrings,
		OnSelect: func(project *commands.Project) error {
			// When a different project is selected, re-filter services and
			// containers to show only those belonging to the selected project.
			return gui.renderContainersAndServices()
		},
		Hide: func() bool {
			return !gui.DockerCommand.IsProjectScoped()
		},
	}
}

func (gui *Gui) refreshProject() error {
	projects := gui.getDiscoveredProjects()

	// Preserve the current selection across refreshes. On the first refresh,
	// select the project specified via -p flag, or fall back to the local project.
	// We match on (Name, IsProfile) so a profile that shares a project's name
	// doesn't cross-select with the project row.
	selectedName := gui.getSelectedProjectName()
	selectedIsProfile := gui.isSelectedProjectAProfile()
	if selectedName == "" {
		if gui.Config.ProjectName != "" {
			selectedName = gui.Config.ProjectName
		} else {
			selectedName = gui.DockerCommand.LocalProjectName
		}
	}

	gui.Panels.Projects.SetItems(projects)

	if selectedName != "" {
		for i, p := range gui.Panels.Projects.List.GetItems() {
			if p.Name == selectedName && p.IsProfile == selectedIsProfile {
				gui.Panels.Projects.SetSelectedLineIdx(i)
				gui.Panels.Projects.Refocus()
				break
			}
		}
	}

	return gui.Panels.Projects.RerenderList()
}

// getDiscoveredProjects returns all docker compose projects by examining container labels.
// The local project (from docker-compose.yml in the current directory, or from -p) is
// included even when it has no running containers, so the user always sees the project
// they explicitly scoped to.
func (gui *Gui) getDiscoveredProjects() []*commands.Project {
	containers := gui.Panels.Containers.List.GetAllItems()
	projectNames := gui.DockerCommand.GetProjectNames(containers)

	// If we're scoped to a project but it has no running containers, still
	// include it. We don't fall back to the directory name here to avoid
	// briefly flashing the wrong project name on startup.
	localName := gui.DockerCommand.LocalProjectName

	if gui.DockerCommand.IsProjectScoped() && localName != "" {
		found := false
		for _, name := range projectNames {
			if name == localName {
				found = true
				break
			}
		}
		if !found {
			projectNames = append([]string{localName}, projectNames...)
		}
	}

	projects := make([]*commands.Project, 0, len(projectNames))
	for _, name := range projectNames {
		projects = append(projects, &commands.Project{Name: name})
	}

	// Append profile pseudo-projects for the local compose project. The Sort
	// callback above places these after all regular project rows.
	if gui.DockerCommand.InDockerComposeProject {
		if profileNames, _ := gui.DockerCommand.GetProfiles(); len(profileNames) > 0 {
			for _, p := range profileNames {
				projects = append(projects, &commands.Project{Name: p, IsProfile: true})
			}
		}
	}

	return projects
}

// getSelectedProjectName returns the name of the currently selected project,
// or empty string if none is selected.
func (gui *Gui) getSelectedProjectName() string {
	project, err := gui.Panels.Projects.GetSelectedItem()
	if err != nil {
		return ""
	}
	return project.Name
}

// isSelectedProjectAProfile reports whether the currently selected project-panel
// row is a profile pseudo-project. Used to disambiguate selection restore when
// a profile name happens to equal a project name.
func (gui *Gui) isSelectedProjectAProfile() bool {
	project, err := gui.Panels.Projects.GetSelectedItem()
	if err != nil {
		return false
	}
	return project.IsProfile
}

// getSelectedProfile returns the selected profile name, or "" if the current
// project-panel selection isn't a profile. Used by the services and containers
// filters to scope to a profile's service set.
func (gui *Gui) getSelectedProfile() string {
	project, err := gui.Panels.Projects.GetSelectedItem()
	if err != nil || project == nil || !project.IsProfile {
		return ""
	}
	return project.Name
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

func (gui *Gui) renderAllLogs(project *commands.Project) tasks.TaskFunc {
	return gui.NewTask(TaskOpts{
		Autoscroll: true,
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Func: func(ctx context.Context) {
			gui.clearMainView()

			obj := commands.CommandObject{}
			if project != nil && project.IsProfile {
				obj.Profile = project.Name
			} else {
				obj.Project = project
			}
			cmd := gui.OSCommand.RunCustomCommand(
				utils.ApplyTemplate(
					gui.Config.UserConfig.CommandTemplates.AllLogs,
					gui.DockerCommand.NewCommandObject(obj),
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

func (gui *Gui) renderDockerComposeConfig(project *commands.Project) tasks.TaskFunc {
	if !gui.DockerCommand.InDockerComposeProject {
		return gui.NewSimpleRenderStringTask(func() string {
			return "Compose config is only available when launched from a docker-compose project directory"
		})
	}
	if project != nil && project.IsProfile {
		return gui.NewSimpleRenderStringTask(func() string {
			return utils.ColoredYamlString(gui.DockerCommand.DockerComposeConfigForProfile(project.Name))
		})
	}
	if project != nil && project.Name != gui.DockerCommand.LocalProjectName {
		return gui.NewSimpleRenderStringTask(func() string {
			return "Compose config is not available for non-local projects"
		})
	}
	return gui.NewSimpleRenderStringTask(func() string {
		return utils.ColoredYamlString(gui.DockerCommand.DockerComposeConfigForProject(project))
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
	project, _ := gui.Panels.Projects.GetSelectedItem()
	obj := commands.CommandObject{}
	if project != nil && project.IsProfile {
		obj.Profile = project.Name
	} else {
		obj.Project = project
	}
	c, err := gui.DockerCommand.ViewAllLogs(obj)
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocess(c)
}
