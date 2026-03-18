package presentation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/jesseduffield/lazycontainer/pkg/config"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
	"github.com/samber/lo"
)

func GetContainerDisplayStrings(guiConfig *config.GuiConfig, container *commands.Container) []string {
	return []string{
		getContainerDisplayStatus(guiConfig, container),
		getContainerDisplaySubstatus(guiConfig, container),
		container.Name,
		getDisplayCPUPerc(container),
		utils.ColoredString(displayPorts(container), color.FgYellow),
		utils.ColoredString(displayContainerImage(container), color.FgMagenta),
	}
}

func displayContainerImage(container *commands.Container) string {
	return strings.TrimPrefix(container.GetImage(), "sha256:")
}

func displayPorts(c *commands.Container) string {
	ports := c.AppleContainer.Configuration.PublishedPorts
	portStrings := lo.Map(ports, func(port struct {
		HostIP        string `json:"hostIP"`
		HostPort      int    `json:"hostPort"`
		ContainerPort int    `json:"containerPort"`
		Protocol      string `json:"protocol"`
	}, _ int) string {
		if port.HostPort == 0 {
			return fmt.Sprintf("%d/%s", port.ContainerPort, port.Protocol)
		}

		ipString := ""
		if port.HostIP != "0.0.0.0" && port.HostIP != "" {
			ipString = port.HostIP + ":"
		}
		return fmt.Sprintf("%s%d->%d/%s", ipString, port.HostPort, port.ContainerPort, port.Protocol)
	})

	sort.Strings(portStrings)

	return strings.Join(portStrings, ", ")
}

func getContainerDisplayStatus(guiConfig *config.GuiConfig, c *commands.Container) string {
	status := c.GetStatus()

	shortStatusMap := map[string]string{
		"paused":     "P",
		"exited":     "X",
		"created":    "C",
		"removing":   "RM",
		"restarting": "RS",
		"running":    "R",
		"dead":       "D",
		"stopped":    "S",
	}

	iconStatusMap := map[string]rune{
		"paused":     '◫',
		"exited":     '⨯',
		"created":    '+',
		"removing":   '−',
		"restarting": '⟳',
		"running":    '▶',
		"dead":       '!',
		"stopped":    '■',
	}

	var containerState string
	switch guiConfig.ContainerStatusHealthStyle {
	case "short":
		containerState = shortStatusMap[status]
	case "icon":
		containerState = string(iconStatusMap[status])
	case "long":
		fallthrough
	default:
		containerState = status
	}

	return utils.ColoredString(containerState, getContainerColor(c))
}

func getContainerDisplaySubstatus(guiConfig *config.GuiConfig, c *commands.Container) string {
	if !c.DetailsLoaded() {
		return ""
	}

	status := c.GetStatus()
	switch status {
	case "exited", "stopped":
		return utils.ColoredString(
			fmt.Sprintf("(%d)", 0), getContainerColor(c),
		)
	case "running":
		return getHealthStatus(guiConfig, c)
	default:
		return ""
	}
}

func getHealthStatus(guiConfig *config.GuiConfig, c *commands.Container) string {
	return ""
}

func getDisplayCPUPerc(c *commands.Container) string {
	stats, ok := c.GetLastStats()
	if !ok {
		return ""
	}

	percentage := stats.DerivedStats.CPUPercentage
	formattedPercentage := fmt.Sprintf("%.2f%%", stats.DerivedStats.CPUPercentage)

	var clr color.Attribute
	if percentage > 90 {
		clr = color.FgRed
	} else if percentage > 50 {
		clr = color.FgYellow
	} else {
		clr = color.FgWhite
	}

	return utils.ColoredString(formattedPercentage, clr)
}

func getContainerColor(c *commands.Container) color.Attribute {
	status := c.GetStatus()
	switch status {
	case "exited", "stopped":
		return color.FgYellow
	case "created":
		return color.FgCyan
	case "running":
		return color.FgGreen
	case "paused":
		return color.FgYellow
	case "dead":
		return color.FgRed
	case "restarting":
		return color.FgBlue
	case "removing":
		return color.FgMagenta
	default:
		return color.FgWhite
	}
}
