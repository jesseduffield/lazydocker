package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	handlertypes "github.com/containers/podman/v5/pkg/api/handlers/types"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/network"
	"github.com/containers/podman/v5/pkg/bindings/pods"
	"github.com/containers/podman/v5/pkg/bindings/system"
	"github.com/containers/podman/v5/pkg/bindings/volumes"
	"github.com/containers/podman/v5/pkg/domain/entities"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	nettypes "go.podman.io/common/libnetwork/types"
)

// SocketRuntime implements ContainerRuntime using Podman's REST API bindings.
// This is the preferred mode when a Podman socket is available.
type SocketRuntime struct {
	conn context.Context
}

// NewSocketRuntime creates a new SocketRuntime connected to the given socket path.
// The socketPath should be in the format "unix:///path/to/podman.sock".
func NewSocketRuntime(socketPath string) (*SocketRuntime, error) {
	conn, err := bindings.NewConnection(context.Background(), socketPath)
	if err != nil {
		return nil, err
	}
	return &SocketRuntime{conn: conn}, nil
}

// Mode returns "socket" to indicate this is the socket-based runtime.
func (r *SocketRuntime) Mode() string {
	return "socket"
}

// Close cleans up any resources. For socket mode, this is a no-op.
func (r *SocketRuntime) Close() error {
	return nil
}

// Container operations

// ListContainers returns all containers.
func (r *SocketRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	all := true
	opts := &containers.ListOptions{
		All: &all,
	}
	podmanContainers, err := containers.List(r.conn, opts)
	if err != nil {
		return nil, err
	}
	return convertPodmanContainerList(podmanContainers), nil
}

// InspectContainer returns detailed information about a container.
func (r *SocketRuntime) InspectContainer(ctx context.Context, id string) (*ContainerDetails, error) {
	data, err := containers.Inspect(r.conn, id, nil)
	if err != nil {
		return nil, err
	}
	return convertPodmanContainerInspect(data), nil
}

// StartContainer starts a stopped container.
func (r *SocketRuntime) StartContainer(ctx context.Context, id string) error {
	return containers.Start(r.conn, id, nil)
}

// StopContainer stops a running container.
func (r *SocketRuntime) StopContainer(ctx context.Context, id string, timeout *int) error {
	opts := &containers.StopOptions{}
	if timeout != nil {
		t := uint(*timeout)
		opts.Timeout = &t
	}
	return containers.Stop(r.conn, id, opts)
}

// PauseContainer pauses a running container.
func (r *SocketRuntime) PauseContainer(ctx context.Context, id string) error {
	return containers.Pause(r.conn, id, nil)
}

// UnpauseContainer unpauses a paused container.
func (r *SocketRuntime) UnpauseContainer(ctx context.Context, id string) error {
	return containers.Unpause(r.conn, id, nil)
}

// RestartContainer restarts a container.
func (r *SocketRuntime) RestartContainer(ctx context.Context, id string, timeout *int) error {
	opts := &containers.RestartOptions{}
	if timeout != nil {
		t := *timeout
		opts.Timeout = &t
	}
	return containers.Restart(r.conn, id, opts)
}

// RemoveContainer removes a container.
func (r *SocketRuntime) RemoveContainer(ctx context.Context, id string, force bool, removeVolumes bool) error {
	opts := &containers.RemoveOptions{
		Force:   &force,
		Volumes: &removeVolumes,
	}
	_, err := containers.Remove(r.conn, id, opts)
	return err
}

// ContainerTop returns process information from a container.
func (r *SocketRuntime) ContainerTop(ctx context.Context, id string) ([]string, [][]string, error) {
	result, err := containers.Top(r.conn, id, nil)
	if err != nil {
		return nil, nil, err
	}
	// Parse the result - first line is headers, rest are processes
	if len(result) == 0 {
		return nil, nil, nil
	}
	headers := strings.Fields(result[0])
	processes := make([][]string, 0, len(result)-1)
	for _, line := range result[1:] {
		processes = append(processes, strings.Fields(line))
	}
	return headers, processes, nil
}

// PruneContainers removes all stopped containers.
func (r *SocketRuntime) PruneContainers(ctx context.Context) error {
	_, err := containers.Prune(r.conn, nil)
	return err
}

// ContainerStats streams container statistics.
func (r *SocketRuntime) ContainerStats(ctx context.Context, id string, stream bool) (<-chan ContainerStatsEntry, <-chan error) {
	statsChan := make(chan ContainerStatsEntry)
	errChan := make(chan error, 1)

	go func() {
		defer close(statsChan)
		defer close(errChan)

		opts := &containers.StatsOptions{
			Stream: &stream,
		}

		reportChan, err := containers.Stats(r.conn, []string{id}, opts)
		if err != nil {
			errChan <- err
			return
		}

		for report := range reportChan {
			if report.Error != nil {
				errChan <- report.Error
				return
			}

			for _, stat := range report.Stats {
				entry := convertPodmanContainerStats(stat)
				select {
				case statsChan <- entry:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return statsChan, errChan
}

// Image operations

// ListImages returns all images.
func (r *SocketRuntime) ListImages(ctx context.Context) ([]ImageSummary, error) {
	opts := &images.ListOptions{}
	podmanImages, err := images.List(r.conn, opts)
	if err != nil {
		return nil, err
	}
	return convertPodmanImageList(podmanImages), nil
}

// InspectImage returns detailed information about an image.
func (r *SocketRuntime) InspectImage(ctx context.Context, id string) (*ImageDetails, error) {
	data, err := images.GetImage(r.conn, id, nil)
	if err != nil {
		return nil, err
	}
	return convertPodmanImageInspect(data), nil
}

// ImageHistory returns the history of an image.
func (r *SocketRuntime) ImageHistory(ctx context.Context, id string) ([]ImageHistoryEntry, error) {
	history, err := images.History(r.conn, id, nil)
	if err != nil {
		return nil, err
	}
	return convertPodmanImageHistory(history), nil
}

// RemoveImage removes an image.
func (r *SocketRuntime) RemoveImage(ctx context.Context, id string, force bool) error {
	opts := &images.RemoveOptions{
		Force: &force,
	}
	_, errs := images.Remove(r.conn, []string{id}, opts)
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// PruneImages removes unused images.
func (r *SocketRuntime) PruneImages(ctx context.Context) error {
	_, err := images.Prune(r.conn, nil)
	return err
}

// Volume operations

// ListVolumes returns all volumes.
func (r *SocketRuntime) ListVolumes(ctx context.Context) ([]VolumeSummary, error) {
	opts := &volumes.ListOptions{}
	podmanVolumes, err := volumes.List(r.conn, opts)
	if err != nil {
		return nil, err
	}
	return convertPodmanVolumeList(podmanVolumes), nil
}

// RemoveVolume removes a volume.
func (r *SocketRuntime) RemoveVolume(ctx context.Context, name string, force bool) error {
	opts := &volumes.RemoveOptions{
		Force: &force,
	}
	return volumes.Remove(r.conn, name, opts)
}

// PruneVolumes removes unused volumes.
func (r *SocketRuntime) PruneVolumes(ctx context.Context) error {
	_, err := volumes.Prune(r.conn, nil)
	return err
}

// Network operations

// ListNetworks returns all network.
func (r *SocketRuntime) ListNetworks(ctx context.Context) ([]NetworkSummary, error) {
	opts := &network.ListOptions{}
	podmanNetworks, err := network.List(r.conn, opts)
	if err != nil {
		return nil, err
	}
	return convertPodmanNetworkList(podmanNetworks), nil
}

// RemoveNetwork removes a network.
func (r *SocketRuntime) RemoveNetwork(ctx context.Context, name string) error {
	_, err := network.Remove(r.conn, name, nil)
	return err
}

// PruneNetworks removes unused network.
func (r *SocketRuntime) PruneNetworks(ctx context.Context) error {
	_, err := network.Prune(r.conn, nil)
	return err
}

// ListPods returns all pods using the pods bindings API.
func (r *SocketRuntime) ListPods(ctx context.Context) ([]PodSummary, error) {
	podList, err := pods.List(r.conn, nil)
	if err != nil {
		return nil, err
	}

	result := make([]PodSummary, len(podList))
	for i, p := range podList {
		result[i] = PodSummary{
			ID:      p.Id,
			Name:    p.Name,
			Status:  p.Status,
			Created: p.Created,
			InfraID: p.InfraId,
			Labels:  p.Labels,
		}
	}
	return result, nil
}

// PodStats streams pod statistics. The Podman API returns stats for each container
// in the pod, so we aggregate them into a single PodStatsEntry.
func (r *SocketRuntime) PodStats(ctx context.Context, id string, stream bool) (<-chan PodStatsEntry, <-chan error) {
	statsChan := make(chan PodStatsEntry)
	errChan := make(chan error, 1)

	go func() {
		defer close(statsChan)
		defer close(errChan)

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			reports, err := pods.Stats(r.conn, []string{id}, nil)
			if err != nil {
				errChan <- err
				return
			}

			entry := aggregatePodStats(reports)
			select {
			case statsChan <- entry:
			case <-ctx.Done():
				return
			}

			if !stream {
				return
			}

			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()

	return statsChan, errChan
}

// aggregatePodStats combines stats from all containers in a pod into a single entry.
func aggregatePodStats(reports []*types.PodStatsReport) PodStatsEntry {
	if len(reports) == 0 {
		return PodStatsEntry{}
	}

	var entry PodStatsEntry
	var totalCPU, totalMem float64
	var totalMemUsage, totalMemLimit uint64
	var totalNetIn, totalNetOut uint64
	var totalBlockIn, totalBlockOut uint64
	var totalPIDs uint64

	for _, r := range reports {
		// Parse CPU percentage (e.g., "75.5%" -> 75.5)
		cpu := parsePercentage(r.CPU)
		totalCPU += cpu

		// Parse memory percentage
		mem := parsePercentage(r.Mem)
		totalMem += mem

		// Parse memory usage bytes (e.g., "1000000 / 4000000")
		memUsage, memLimit := parseMemoryBytes(r.MemUsageBytes)
		totalMemUsage += memUsage
		totalMemLimit += memLimit

		// Parse network I/O (e.g., "1.5kB / 2.3kB")
		netIn, netOut := parseIOBytes(r.NetIO)
		totalNetIn += netIn
		totalNetOut += netOut

		// Parse block I/O
		blockIn, blockOut := parseIOBytes(r.BlockIO)
		totalBlockIn += blockIn
		totalBlockOut += blockOut

		// Parse PIDs
		pids := parseUint(r.PIDS)
		totalPIDs += pids

		// Use the first report's pod info
		if entry.PodID == "" {
			entry.PodID = r.Pod
			entry.PodName = r.Name
		}
	}

	entry.CPU = totalCPU
	entry.Memory = totalMem
	entry.MemUsage = totalMemUsage
	entry.MemLimit = totalMemLimit
	entry.NetInput = totalNetIn
	entry.NetOutput = totalNetOut
	entry.BlockInput = totalBlockIn
	entry.BlockOutput = totalBlockOut
	entry.PIDs = totalPIDs

	return entry
}

// parsePercentage parses a percentage string like "75.5%" into a float64.
func parsePercentage(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	var val float64
	fmt.Sscanf(s, "%f", &val)
	return val
}

// parseMemoryBytes parses memory usage string like "1000000 / 4000000" into usage and limit.
func parseMemoryBytes(s string) (usage, limit uint64) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	usage = parseByteValue(strings.TrimSpace(parts[0]))
	limit = parseByteValue(strings.TrimSpace(parts[1]))
	return
}

// parseIOBytes parses I/O string like "1.5kB / 2.3kB" into input and output bytes.
func parseIOBytes(s string) (input, output uint64) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	input = parseByteValue(strings.TrimSpace(parts[0]))
	output = parseByteValue(strings.TrimSpace(parts[1]))
	return
}

// parseByteValue parses a byte value string with optional unit (e.g., "1.5kB", "10MB", "1000000").
func parseByteValue(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" {
		return 0
	}

	// Remove commas from numbers
	s = strings.ReplaceAll(s, ",", "")

	var val float64
	var unit string
	n, _ := fmt.Sscanf(s, "%f%s", &val, &unit)
	if n == 0 {
		return 0
	}

	unit = strings.ToLower(strings.TrimSpace(unit))
	switch unit {
	case "b", "":
		return uint64(val)
	case "kb", "kib":
		return uint64(val * 1024)
	case "mb", "mib":
		return uint64(val * 1024 * 1024)
	case "gb", "gib":
		return uint64(val * 1024 * 1024 * 1024)
	case "tb", "tib":
		return uint64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return uint64(val)
	}
}

// parseUint parses a string to uint64.
func parseUint(s string) uint64 {
	s = strings.TrimSpace(s)
	var val uint64
	fmt.Sscanf(s, "%d", &val)
	return val
}

// Events streams container runtime events.
func (r *SocketRuntime) Events(ctx context.Context) (<-chan Event, <-chan error) {
	eventChan := make(chan Event)
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errChan)

		opts := &system.EventsOptions{}
		podmanEventChan := make(chan types.Event)
		cancelChan := make(chan bool)

		go func() {
			<-ctx.Done()
			close(cancelChan)
		}()

		go func() {
			err := system.Events(r.conn, podmanEventChan, cancelChan, opts)
			if err != nil {
				errChan <- err
			}
		}()

		for podmanEvent := range podmanEventChan {
			event := Event{
				Type:   string(podmanEvent.Type),
				Action: string(podmanEvent.Action),
				Actor: EventActor{
					ID:         podmanEvent.Actor.ID,
					Attributes: podmanEvent.Actor.Attributes,
				},
				Time: podmanEvent.Time,
			}
			select {
			case eventChan <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return eventChan, errChan
}

// Conversion functions

func convertPodmanContainerList(podmanContainers []entities.ListContainer) []ContainerSummary {
	result := make([]ContainerSummary, len(podmanContainers))
	for i, c := range podmanContainers {
		var sizeRw, sizeRootFs int64
		if c.Size != nil {
			sizeRw = c.Size.RwSize
			sizeRootFs = c.Size.RootFsSize
		}
		result[i] = ContainerSummary{
			ID:         c.ID,
			Names:      c.Names,
			Image:      c.Image,
			ImageID:    c.ImageID,
			Command:    commandToString(c.Command),
			Created:    c.Created.Unix(),
			State:      c.State,
			Status:     c.Status,
			Ports:      convertPodmanPorts(c.Ports),
			Labels:     c.Labels,
			SizeRw:     sizeRw,
			SizeRootFs: sizeRootFs,
			Pod:        c.Pod,
			PodName:    c.PodName,
			IsInfra:    c.IsInfra,
		}
	}
	return result
}

func commandToString(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	// Return the command as JSON for consistency with Docker
	data, _ := json.Marshal(cmd)
	return string(data)
}

func convertPodmanPorts(ports []nettypes.PortMapping) []PortMapping {
	result := make([]PortMapping, len(ports))
	for i, p := range ports {
		result[i] = PortMapping{
			IP:          p.HostIP,
			PrivatePort: p.ContainerPort,
			PublicPort:  p.HostPort,
			Type:        p.Protocol,
		}
	}
	return result
}

func convertPodmanContainerInspect(data *define.InspectContainerData) *ContainerDetails {
	if data == nil {
		return nil
	}

	details := &ContainerDetails{
		ID:              data.ID,
		Name:            data.Name,
		Created:         data.Created,
		Path:            data.Path,
		Args:            data.Args,
		Image:           data.Image,
		ImageID:         data.ImageName,
		ResolvConfPath:  data.ResolvConfPath,
		HostnamePath:    data.HostnamePath,
		HostsPath:       data.HostsPath,
		RestartCount:    int(data.RestartCount),
		Driver:          data.Driver,
		MountLabel:      data.MountLabel,
		ProcessLabel:    data.ProcessLabel,
		AppArmorProfile: data.AppArmorProfile,
	}

	if data.State != nil {
		details.State = &ContainerState{
			Status:     data.State.Status,
			Running:    data.State.Running,
			Paused:     data.State.Paused,
			Restarting: data.State.Restarting,
			OOMKilled:  data.State.OOMKilled,
			Dead:       data.State.Dead,
			Pid:        data.State.Pid,
			ExitCode:   int(data.State.ExitCode),
			Error:      data.State.Error,
			StartedAt:  data.State.StartedAt,
			FinishedAt: data.State.FinishedAt,
		}

		if data.State.Health != nil {
			details.State.Health = &HealthState{
				Status:        data.State.Health.Status,
				FailingStreak: data.State.Health.FailingStreak,
			}
		}
	}

	if data.Config != nil {
		var onBuild []string
		if data.Config.OnBuild != nil {
			onBuild = []string{*data.Config.OnBuild}
		}
		details.Config = &ContainerConfig{
			Hostname:     data.Config.Hostname,
			Domainname:   data.Config.DomainName,
			User:         data.Config.User,
			AttachStdin:  data.Config.AttachStdin,
			AttachStdout: data.Config.AttachStdout,
			AttachStderr: data.Config.AttachStderr,
			Tty:          data.Config.Tty,
			OpenStdin:    data.Config.OpenStdin,
			StdinOnce:    data.Config.StdinOnce,
			Env:          data.Config.Env,
			Cmd:          data.Config.Cmd,
			Image:        data.Config.Image,
			WorkingDir:   data.Config.WorkingDir,
			Entrypoint:   data.Config.Entrypoint,
			OnBuild:      onBuild,
			Labels:       data.Config.Labels,
			StopSignal:   data.Config.StopSignal,
		}
	}

	return details
}

func convertPodmanContainerStats(stat define.ContainerStats) ContainerStatsEntry {
	return ContainerStatsEntry{
		Read:    time.Now(),
		PreRead: time.Now().Add(-time.Second),
		CPUStats: CPUStats{
			CPUUsage: CPUUsage{
				TotalUsage: int64(stat.CPU),
			},
			SystemCPUUsage: int64(stat.CPUSystemNano),
			OnlineCpus:     int(stat.CPUNano),
		},
		PreCPUStats: CPUStats{},
		MemoryStats: MemoryStats{
			Usage: int64(stat.MemUsage),
			Limit: int64(stat.MemLimit),
		},
		PidsStats: PidsStats{
			Current: int(stat.PIDs),
		},
		Name: stat.Name,
		ID:   stat.ContainerID,
	}
}

func convertPodmanImageList(podmanImages []*entities.ImageSummary) []ImageSummary {
	result := make([]ImageSummary, len(podmanImages))
	for i, img := range podmanImages {
		result[i] = ImageSummary{
			ID:          img.ID,
			ParentID:    img.ParentId,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Created:     img.Created,
			Size:        img.Size,
			SharedSize:  int64(img.SharedSize),
			VirtualSize: img.VirtualSize,
			Labels:      img.Labels,
			Containers:  int64(img.Containers),
		}
	}
	return result
}

func convertPodmanImageInspect(data *entities.ImageInspectReport) *ImageDetails {
	if data == nil {
		return nil
	}

	details := &ImageDetails{
		ID:           data.ID,
		RepoTags:     data.RepoTags,
		RepoDigests:  data.RepoDigests,
		Parent:       data.Parent,
		Comment:      data.Comment,
		Created:      *data.Created,
		Author:       data.Author,
		Architecture: data.Architecture,
		Os:           data.Os,
		Size:         data.Size,
		VirtualSize:  data.VirtualSize,
	}

	if data.RootFS != nil {
		layers := make([]string, len(data.RootFS.Layers))
		for i, layer := range data.RootFS.Layers {
			layers[i] = layer.String()
		}
		details.RootFS = RootFS{
			Type:   data.RootFS.Type,
			Layers: layers,
		}
	}

	return details
}

func convertPodmanImageHistory(history []*handlertypes.HistoryResponse) []ImageHistoryEntry {
	result := make([]ImageHistoryEntry, len(history))
	for i, layer := range history {
		result[i] = ImageHistoryEntry{
			ID:        layer.ID,
			Created:   layer.Created,
			CreatedBy: layer.CreatedBy,
			Tags:      layer.Tags,
			Size:      layer.Size,
			Comment:   layer.Comment,
		}
	}
	return result
}

func convertPodmanVolumeList(podmanVolumes []*entities.VolumeListReport) []VolumeSummary {
	result := make([]VolumeSummary, len(podmanVolumes))
	for i, vol := range podmanVolumes {
		result[i] = VolumeSummary{
			Name:       vol.Name,
			Driver:     vol.Driver,
			Mountpoint: vol.Mountpoint,
			CreatedAt:  vol.CreatedAt,
			Labels:     vol.Labels,
			Scope:      vol.Scope,
			Options:    vol.Options,
		}
	}
	return result
}

func convertPodmanNetworkList(podmanNetworks []nettypes.Network) []NetworkSummary {
	result := make([]NetworkSummary, len(podmanNetworks))
	for i, nw := range podmanNetworks {
		result[i] = NetworkSummary{
			Name:    nw.Name,
			ID:      nw.ID,
			Created: nw.Created,
			Driver:  nw.Driver,
			Labels:  nw.Labels,
		}
	}
	return result
}
