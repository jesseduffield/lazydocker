package presentation

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
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
	return strings.TrimPrefix(container.Container.Image, "sha256:")
}

func displayPorts(c *commands.Container) string {
	portStrings := lo.Map(c.Container.Ports, func(port dockerTypes.Port, _ int) string {
		if port.PublicPort == 0 {
			return fmt.Sprintf("%d/%s", port.PrivatePort, port.Type)
		}

		// docker ps will show '0.0.0.0:80->80/tcp' but we'll show
		// '80->80/tcp' instead to save space (unless the IP is something other than
		// 0.0.0.0)
		ipString := ""
		if port.IP != "0.0.0.0" {
			ipString = port.IP + ":"
		}
		return fmt.Sprintf("%s%d->%d/%s", ipString, port.PublicPort, port.PrivatePort, port.Type)
	})

	// sorting because the order of the ports is not deterministic
	// and we don't want to have them constantly swapping
	sort.Strings(portStrings)

	return strings.Join(portStrings, ", ")
}

// getContainerDisplayStatus returns the colored status of the container
func getContainerDisplayStatus(guiConfig *config.GuiConfig, c *commands.Container) string {
	shortStatusMap := map[string]string{
		"paused":     "P",
		"exited":     "X",
		"created":    "C",
		"removing":   "RM",
		"restarting": "RS",
		"running":    "R",
		"dead":       "D",
	}

	iconStatusMap := map[string]rune{
		"paused":     '◫',
		"exited":     '⨯',
		"created":    '+',
		"removing":   '−',
		"restarting": '⟳',
		"running":    '▶',
		"dead":       '!',
	}

	var containerState string
	switch guiConfig.ContainerStatusHealthStyle {
	case "short":
		containerState = shortStatusMap[c.Container.State]
	case "icon":
		containerState = string(iconStatusMap[c.Container.State])
	case "long":
		fallthrough
	default:
		containerState = c.Container.State
	}

	return utils.ColoredString(containerState, getContainerColor(c))
}

// GetDisplayStatus returns the exit code if the container has exited, and the health status if the container is running (and has a health check)
func getContainerDisplaySubstatus(guiConfig *config.GuiConfig, c *commands.Container) string {
	if !c.DetailsLoaded() {
		return ""
	}

	switch c.Container.State {
	case "exited":
		return utils.ColoredString(
			fmt.Sprintf("(%s)", strconv.Itoa(c.Details.State.ExitCode)), getContainerColor(c),
		)
	case "running":
		return getHealthStatus(guiConfig, c)
	default:
		return ""
	}
}

func getHealthStatus(guiConfig *config.GuiConfig, c *commands.Container) string {
	if !c.DetailsLoaded() {
		return ""
	}

	healthStatusColorMap := map[string]color.Attribute{
		"healthy":   color.FgGreen,
		"unhealthy": color.FgRed,
		"starting":  color.FgYellow,
	}

	if c.Details.State.Health == nil {
		return ""
	}

	shortHealthStatusMap := map[string]string{
		"healthy":   "H",
		"unhealthy": "U",
		"starting":  "S",
	}

	iconHealthStatusMap := map[string]rune{
		"healthy":   '✔',
		"unhealthy": '?',
		"starting":  '…',
	}

	var healthStatus string
	switch guiConfig.ContainerStatusHealthStyle {
	case "short":
		healthStatus = shortHealthStatusMap[c.Details.State.Health.Status]
	case "icon":
		healthStatus = string(iconHealthStatusMap[c.Details.State.Health.Status])
	case "long":
		fallthrough
	default:
		healthStatus = c.Details.State.Health.Status
	}

	if healthStatusColor, ok := healthStatusColorMap[c.Details.State.Health.Status]; ok {
		return utils.ColoredString(fmt.Sprintf("(%s)", healthStatus), healthStatusColor)
	}
	return ""
}

// getDisplayCPUPerc colors the cpu percentage based on how extreme it is
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

// getContainerColor Container color
func getContainerColor(c *commands.Container) color.Attribute {
	switch c.Container.State {
	case "exited":
		// This means the colour may be briefly yellow and then switch to red upon starting
		// Not sure what a better alternative is.
		if !c.DetailsLoaded() || c.Details.State.ExitCode == 0 {
			return color.FgYellow
		}
		return color.FgRed
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
