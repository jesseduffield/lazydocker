package gui

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func (gui *Gui) handleDockerHubSearch() error {
	return gui.createPromptPanel(gui.Tr.DockerHubSearchTitle, func(g *gocui.Gui, v *gocui.View) error {
		query := gui.trimmedContent(v)
		if query == "" {
			return nil
		}
		return gui.searchAndShowDockerHubResults(query)
	})
}

func (gui *Gui) searchAndShowDockerHubResults(query string) error {
	return gui.WithWaitingStatus(gui.Tr.PullingImageStatus, func() error {
		results, err := commands.SearchDockerHub(query, 25)
		if err != nil {
			return gui.createErrorPanel(fmt.Sprintf("Docker Hub search failed: %s", err.Error()))
		}

		gui.g.Update(func(g *gocui.Gui) error {
			return gui.showDockerHubResults(results)
		})
		return nil
	})
}

func (gui *Gui) showDockerHubResults(results []commands.DockerHubResult) error {
	if len(results) == 0 {
		return gui.createErrorPanel(gui.Tr.DockerHubNoResults)
	}

	menuItems := make([]*types.MenuItem, 0, len(results))
	for _, r := range results {
		r := r

		officialBadge := ""
		if r.IsOfficial {
			officialBadge = utils.ColoredString("[Official] ", color.FgGreen)
		}

		name := officialBadge + utils.ColoredString(r.RepoName, color.FgCyan)

		description := r.ShortDescription
		if len(description) > 60 {
			description = description[:57] + "..."
		}

		stats := fmt.Sprintf(
			"⭐ %s  ⬇ %s",
			formatCount(r.StarCount),
			formatCount(r.PullCount),
		)

		menuItems = append(menuItems, &types.MenuItem{
			LabelColumns: []string{name, description, stats},
			OnPress: func() error {
				return gui.handlePullDockerHubImage(r)
			},
		})
	}

	return gui.Menu(CreateMenuOptions{
		Title: "Docker Hub Results",
		Items: menuItems,
	})
}

func (gui *Gui) handlePullDockerHubImage(result commands.DockerHubResult) error {
	cmd := gui.OSCommand.ExecutableFromString(fmt.Sprintf("docker pull %s", result.RepoName))
	return gui.runSubprocessWithMessage(cmd, fmt.Sprintf("Pulling %s from Docker Hub…", result.RepoName))
}

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
