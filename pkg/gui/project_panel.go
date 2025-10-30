package gui

import (
	"bytes"
	"context"
	"path"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/jesseduffield/yaml"
)

// Although at the moment we'll only have one project, in future we could have
// a list of projects in the project panel.

func (gui *Gui) getProjectPanel() *panels.SideListPanel[*commands.Project] {
	return &panels.SideListPanel[*commands.Project]{
		ContextState: &panels.ContextState[*commands.Project]{
			GetMainTabs: func() []panels.MainTab[*commands.Project] {
				if gui.ContainerCommand.InDockerComposeProject() {
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
							Key:    "runtime",
							Title:  "Runtime Info",
							Render: gui.renderRuntimeInfo,
						},
						{
							Key:    "system",
							Title:  "System Status",
							Render: gui.renderSystemStatus,
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
					{
						Key:    "runtime",
						Title:  "Runtime Info",
						Render: gui.renderRuntimeInfo,
					},
					{
						Key:    "system",
						Title:  "System Status",
						Render: gui.renderSystemStatus,
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
			return false
		},
		GetTableCells: func(project *commands.Project) []string {
			runtimeIndicator := ""
			if gui.Config.Runtime == "apple" {
				runtimeIndicator = " 🍎"
			} else if gui.Config.Runtime == "docker" {
				runtimeIndicator = " 🐳"
			}
			return []string{project.Name + runtimeIndicator}
		},
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
	if gui.ContainerCommand.InDockerComposeProject() {
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

func (gui *Gui) renderAllLogs(_project *commands.Project) tasks.TaskFunc {
	return gui.NewTask(TaskOpts{
		Autoscroll: true,
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Func: func(ctx context.Context) {
			gui.clearMainView()

			cmd := gui.OSCommand.RunCustomCommand(
				utils.ApplyTemplate(
					gui.Config.UserConfig.CommandTemplates.AllLogs,
					gui.ContainerCommand.NewCommandObject(commands.CommandObject{}),
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
		return utils.ColoredYamlString(gui.ContainerCommand.DockerComposeConfig())
	})
}

// renderRuntimeInfo shows the current runtime and detected capabilities
func (gui *Gui) renderRuntimeInfo(_project *commands.Project) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string { return gui.runtimeInfoStr() })
}

// renderSystemStatus shows runtime-specific system status (Apple runtime)
func (gui *Gui) renderSystemStatus(_project *commands.Project) tasks.TaskFunc {
	return gui.NewSimpleRenderStringTask(func() string {
		status, err := gui.ContainerCommand.SystemStatus()
		if err != nil {
			return utils.ColoredString("System status unavailable: "+err.Error(), color.FgRed)
		}
		if len(status) == 0 {
			return "No system status available for this runtime"
		}
		var buf bytes.Buffer
		_ = yaml.NewEncoder(&buf, yaml.IncludeOmitted).Encode(status)
		return utils.ColoredYamlString(buf.String())
	})
}

func (gui *Gui) runtimeInfoStr() string {
	var b strings.Builder
	runtimeName := gui.ContainerCommand.GetRuntimeName()
	runtimeVersion := gui.ContainerCommand.GetRuntimeVersion()

	b.WriteString("Runtime\n=======\n\n")
	b.WriteString("Name:  " + runtimeName + "\n")
	b.WriteString("Info:  " + runtimeVersion + "\n\n")

	// Capabilities table
	b.WriteString("Capabilities\n============\n\n")
	rows := [][]string{{"Feature", "Supported"}}
	feature := func(label string, f commands.Feature) {
		ok := gui.ContainerCommand.Supports(f)
		status := color.New(color.FgGreen).Sprint("yes")
		if !ok {
			status = color.New(color.FgRed).Sprint("no")
		}
		rows = append(rows, []string{label, status})
	}

	feature("Image History", commands.FeatureImageHistory)
	feature("Image Remove", commands.FeatureImageRemove)
	feature("Image Prune", commands.FeatureImagePrune)
	feature("Container Attach", commands.FeatureContainerAttach)
	feature("Container Exec", commands.FeatureContainerExec)
	feature("Container Top", commands.FeatureContainerTop)
	feature("Container Prune", commands.FeatureContainerPrune)
	feature("Volume Prune", commands.FeatureVolumePrune)
	feature("Network Prune", commands.FeatureNetworkPrune)
	feature("Services/Compose", commands.FeatureServices)
	feature("Events Stream", commands.FeatureEventsStream)
	feature("Live Stats", commands.FeatureStats)

	table, _ := utils.RenderTable(rows)
	b.WriteString(table)
	b.WriteString("\n")

	b.WriteString("Notes\n=====\n")
	b.WriteString("- Capabilities are detected from your local CLI (help output).\n")
	b.WriteString("- If a feature appears unsupported but your CLI gained it, restart LazyDocker.\n")

	return b.String()
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
	c, err := gui.ContainerCommand.ViewAllLogs()
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocess(c)
}
