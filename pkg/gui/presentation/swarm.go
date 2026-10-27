package presentation

import (
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/go-units"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

func GetSwarmServiceDisplayStrings(service *commands.SwarmService) []string {
	status := ""
	if service.IsUpdating() {
		status = utils.ColoredString(string(service.Service.UpdateStatus.State), color.FgYellow)
	}

	running, desired := service.Replicas()
	ports := []string{}
	for _, port := range service.Service.Endpoint.Ports {
		if port.PublishedPort != 0 {
			ports = append(ports, fmt.Sprintf("*:%d->%d/%s", port.PublishedPort, port.TargetPort, port.Protocol))
		}
	}

	return []string{
		coloredReplicas(running, desired),
		service.Name,
		status,
		utils.ColoredString(service.Mode(), color.FgBlue),
		utils.ColoredString(service.Image(), color.FgMagenta),
		utils.ColoredString(strings.Join(ports, ", "), color.FgYellow),
	}
}

func GetSwarmStackDisplayStrings(stack *commands.SwarmStack) []string {
	running, desired := stack.Replicas()
	name := stack.Name
	if stack.IsAll() {
		name = "all"
	}
	return []string{
		coloredReplicas(running, desired),
		name,
		utils.ColoredString(countServices(len(stack.Services)), color.FgBlue),
	}
}

func GetSwarmTaskDisplayStrings(task *commands.SwarmTask) []string {
	return []string{
		coloredTaskState(task),
		task.Name(),
		utils.ColoredString(task.NodeName, color.FgCyan),
		age(task.Task.Status.Timestamp),
		utils.ColoredString(task.Task.Status.Err, color.FgRed),
	}
}

func GetSwarmNodeDisplayStrings(node *commands.SwarmNode) []string {
	stateColor := color.FgRed
	if node.Node.Status.State == swarm.NodeStateReady {
		stateColor = color.FgGreen
	}

	availabilityColor := map[swarm.NodeAvailability]color.Attribute{
		swarm.NodeAvailabilityActive: color.FgGreen,
		swarm.NodeAvailabilityPause:  color.FgYellow,
		swarm.NodeAvailabilityDrain:  color.FgRed,
	}[node.Node.Spec.Availability]

	role := string(node.Node.Spec.Role)
	if node.Node.ManagerStatus != nil && node.Node.ManagerStatus.Leader {
		role = "leader"
	}

	return []string{
		utils.ColoredString(string(node.Node.Status.State), stateColor),
		utils.ColoredString(string(node.Node.Spec.Availability), availabilityColor),
		utils.ColoredString(role, color.FgBlue),
		node.Hostname(),
		node.Node.Description.Engine.EngineVersion,
	}
}

func GetSwarmVolumeDisplayStrings(vol *commands.SwarmVolume) []string {
	scope := "local"
	if vol.Volume != nil {
		scope = "cluster"
	}
	return []string{vol.Name, utils.ColoredString(vol.Driver, color.FgMagenta), utils.ColoredString(scope, color.FgCyan), usedBy(len(vol.UsedBy))}
}

func GetSwarmNetworkDisplayStrings(n *commands.SwarmNetwork) []string {
	subnets := lo.Map(n.Network.IPAM.Config, func(config network.IPAMConfig, _ int) string { return config.Subnet })
	return []string{
		n.Name,
		utils.ColoredString(n.Network.Driver, color.FgMagenta),
		usedBy(len(n.UsedBy)),
		utils.ColoredString(strings.Join(subnets, ", "), color.FgYellow),
	}
}

func GetSwarmSecretDisplayStrings(secret *commands.SwarmSecret) []string {
	return []string{secret.Name, usedBy(len(secret.UsedBy)), age(secret.Secret.CreatedAt)}
}

func GetSwarmConfigDisplayStrings(cfg *commands.SwarmConfig) []string {
	return []string{cfg.Name, usedBy(len(cfg.UsedBy)), age(cfg.Config.CreatedAt)}
}

// coloredTaskState colours a task's state. Tasks that are history (being shut
// down or already shut down) are dimmed.
func coloredTaskState(task *commands.SwarmTask) string {
	state := task.Task.Status.State
	str := string(state)
	switch {
	case state == swarm.TaskStateFailed || state == swarm.TaskStateRejected:
		return utils.ColoredString(str, color.FgRed)
	case !task.IsCurrent():
		return utils.ColoredString(str, color.FgHiBlack)
	case state == swarm.TaskStateRunning:
		return utils.ColoredString(str, color.FgGreen)
	default:
		return utils.ColoredString(str, color.FgYellow)
	}
}

// age renders a timestamp as e.g. "5 minutes ago"
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return units.HumanDuration(time.Since(t)) + " ago"
}

func coloredReplicas(running, desired uint64) string {
	str := fmt.Sprintf("%d/%d", running, desired)
	switch {
	case desired == 0:
		return utils.ColoredString(str, color.FgBlue)
	case running == 0:
		return utils.ColoredString(str, color.FgRed)
	case running < desired:
		return utils.ColoredString(str, color.FgYellow)
	default:
		return utils.ColoredString(str, color.FgGreen)
	}
}

func usedBy(count int) string {
	if count == 0 {
		return utils.ColoredString("unused", color.FgBlue)
	}
	return utils.ColoredString(countServices(count), color.FgBlue)
}

func countServices(count int) string {
	if count == 1 {
		return "1 service"
	}
	return fmt.Sprintf("%d services", count)
}
