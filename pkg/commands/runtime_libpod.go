//go:build linux && cgo

package commands

import (
	"context"
	"time"

	"github.com/containers/podman/v5/libpod"
	"github.com/containers/podman/v5/libpod/define"
	"go.podman.io/common/libimage"
	nettypes "go.podman.io/common/libnetwork/types"
)

// LibpodRuntime implements ContainerRuntime using libpod directly.
// This mode does not require a Podman socket and works directly with the container runtime.
// It requires CGO and is only available on Linux.
type LibpodRuntime struct {
	runtime *libpod.Runtime
}

// NewLibpodRuntime creates a new LibpodRuntime.
func NewLibpodRuntime() (*LibpodRuntime, error) {
	runtime, err := libpod.NewRuntime(context.Background())
	if err != nil {
		return nil, err
	}
	return &LibpodRuntime{runtime: runtime}, nil
}

// Mode returns "libpod" to indicate this is the direct libpod runtime.
func (r *LibpodRuntime) Mode() string {
	return "libpod"
}

// Close shuts down the libpod runtime.
func (r *LibpodRuntime) Close() error {
	if r.runtime != nil {
		return r.runtime.Shutdown(false)
	}
	return nil
}

// Container operations

// ListContainers returns all containers.
func (r *LibpodRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	ctrs, err := r.runtime.GetAllContainers()
	if err != nil {
		return nil, err
	}
	return convertLibpodContainerList(ctrs)
}

// InspectContainer returns detailed information about a container.
func (r *LibpodRuntime) InspectContainer(ctx context.Context, id string) (*ContainerDetails, error) {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return nil, err
	}
	data, err := ctr.Inspect(false)
	if err != nil {
		return nil, err
	}
	return convertLibpodContainerInspect(data), nil
}

// StartContainer starts a stopped container.
func (r *LibpodRuntime) StartContainer(ctx context.Context, id string) error {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return err
	}
	return ctr.Start(ctx, false)
}

// StopContainer stops a running container.
func (r *LibpodRuntime) StopContainer(ctx context.Context, id string, timeout *int) error {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return err
	}
	t := uint(10) // default timeout
	if timeout != nil {
		t = uint(*timeout)
	}
	return ctr.StopWithTimeout(t)
}

// PauseContainer pauses a running container.
func (r *LibpodRuntime) PauseContainer(ctx context.Context, id string) error {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return err
	}
	return ctr.Pause()
}

// UnpauseContainer unpauses a paused container.
func (r *LibpodRuntime) UnpauseContainer(ctx context.Context, id string) error {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return err
	}
	return ctr.Unpause()
}

// RestartContainer restarts a container.
func (r *LibpodRuntime) RestartContainer(ctx context.Context, id string, timeout *int) error {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return err
	}
	t := uint(10) // default timeout
	if timeout != nil {
		t = uint(*timeout)
	}
	return ctr.RestartWithTimeout(ctx, t)
}

// RemoveContainer removes a container.
func (r *LibpodRuntime) RemoveContainer(ctx context.Context, id string, force bool, removeVolumes bool) error {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return err
	}
	var timeout uint = 10
	_, _, err = r.runtime.RemoveContainerAndDependencies(ctx, ctr, force, removeVolumes, &timeout)
	return err
}

// ContainerTop returns process information from a container.
func (r *LibpodRuntime) ContainerTop(ctx context.Context, id string) ([]string, [][]string, error) {
	ctr, err := r.runtime.LookupContainer(id)
	if err != nil {
		return nil, nil, err
	}
	// Top returns []string where each string is a line (header + processes)
	result, err := ctr.Top([]string{})
	if err != nil {
		return nil, nil, err
	}
	if len(result) == 0 {
		return nil, nil, nil
	}
	// Parse the result: first line is headers, rest are process rows
	// Each line is space-separated fields
	headers := splitFields(result[0])
	processes := make([][]string, 0, len(result)-1)
	for _, line := range result[1:] {
		processes = append(processes, splitFields(line))
	}
	return headers, processes, nil
}

// PruneContainers removes all stopped containers.
func (r *LibpodRuntime) PruneContainers(ctx context.Context) error {
	_, err := r.runtime.PruneContainers(nil)
	return err
}

// ContainerStats streams container statistics.
func (r *LibpodRuntime) ContainerStats(ctx context.Context, id string, stream bool) (<-chan ContainerStatsEntry, <-chan error) {
	statsChan := make(chan ContainerStatsEntry)
	errChan := make(chan error, 1)

	go func() {
		defer close(statsChan)
		defer close(errChan)

		ctr, err := r.runtime.LookupContainer(id)
		if err != nil {
			errChan <- err
			return
		}

		for {
			stats, err := ctr.GetContainerStats(nil)
			if err != nil {
				errChan <- err
				return
			}

			entry := convertLibpodContainerStats(stats)
			select {
			case statsChan <- entry:
			case <-ctx.Done():
				return
			}

			if !stream {
				return
			}
			time.Sleep(time.Second)
		}
	}()

	return statsChan, errChan
}

// Image operations

// ListImages returns all images.
func (r *LibpodRuntime) ListImages(ctx context.Context) ([]ImageSummary, error) {
	imgs, err := r.runtime.LibimageRuntime().ListImages(ctx, nil)
	if err != nil {
		return nil, err
	}
	return convertLibpodImageList(ctx, imgs), nil
}

// InspectImage returns detailed information about an image.
func (r *LibpodRuntime) InspectImage(ctx context.Context, id string) (*ImageDetails, error) {
	img, _, err := r.runtime.LibimageRuntime().LookupImage(id, nil)
	if err != nil {
		return nil, err
	}
	return convertLibpodImageInspect(ctx, img), nil
}

// ImageHistory returns the history of an image.
func (r *LibpodRuntime) ImageHistory(ctx context.Context, id string) ([]ImageHistoryEntry, error) {
	img, _, err := r.runtime.LibimageRuntime().LookupImage(id, nil)
	if err != nil {
		return nil, err
	}
	history, err := img.History(ctx)
	if err != nil {
		return nil, err
	}
	return convertLibpodImageHistory(history), nil
}

// RemoveImage removes an image.
func (r *LibpodRuntime) RemoveImage(ctx context.Context, id string, force bool) error {
	img, _, err := r.runtime.LibimageRuntime().LookupImage(id, nil)
	if err != nil {
		return err
	}
	_, errs := r.runtime.LibimageRuntime().RemoveImages(ctx, []string{img.ID()}, &libimage.RemoveImagesOptions{Force: force})
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// PruneImages removes unused images.
func (r *LibpodRuntime) PruneImages(ctx context.Context) error {
	// RemoveImages with no names and dangling=true filter prunes dangling images
	opts := &libimage.RemoveImagesOptions{
		Filters: []string{"dangling=true"},
	}
	_, errs := r.runtime.LibimageRuntime().RemoveImages(ctx, nil, opts)
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Volume operations

// ListVolumes returns all volumes.
func (r *LibpodRuntime) ListVolumes(ctx context.Context) ([]VolumeSummary, error) {
	vols, err := r.runtime.GetAllVolumes()
	if err != nil {
		return nil, err
	}
	return convertLibpodVolumeList(vols), nil
}

// RemoveVolume removes a volume.
func (r *LibpodRuntime) RemoveVolume(ctx context.Context, name string, force bool) error {
	vol, err := r.runtime.LookupVolume(name)
	if err != nil {
		return err
	}
	var timeout uint = 10
	return r.runtime.RemoveVolume(ctx, vol, force, &timeout)
}

// PruneVolumes removes unused volumes.
func (r *LibpodRuntime) PruneVolumes(ctx context.Context) error {
	_, err := r.runtime.PruneVolumes(ctx, nil)
	return err
}

// Network operations

// ListNetworks returns all networks.
func (r *LibpodRuntime) ListNetworks(ctx context.Context) ([]NetworkSummary, error) {
	nets, err := r.runtime.Network().NetworkList()
	if err != nil {
		return nil, err
	}
	return convertLibpodNetworkList(nets), nil
}

// RemoveNetwork removes a network.
func (r *LibpodRuntime) RemoveNetwork(ctx context.Context, name string) error {
	return r.runtime.Network().NetworkRemove(name)
}

// PruneNetworks removes unused networks.
// Note: libnetwork doesn't have a direct prune method, so we skip this for now.
// Proper implementation would require checking which networks are in use.
func (r *LibpodRuntime) PruneNetworks(ctx context.Context) error {
	// NetworkPrune is not available in libnetwork interface
	// TODO: Implement manual pruning by listing networks and checking usage
	return nil
}

// Pod operations

// ListPods returns all pods.
func (r *LibpodRuntime) ListPods(ctx context.Context) ([]PodSummary, error) {
	pods, err := r.runtime.GetAllPods()
	if err != nil {
		return nil, err
	}
	result := make([]PodSummary, len(pods))
	for i, pod := range pods {
		status, err := pod.GetPodStatus()
		if err != nil {
			status = "unknown"
		}
		infraID, _ := pod.InfraContainerID()
		result[i] = PodSummary{
			ID:      pod.ID(),
			Name:    pod.Name(),
			Status:  status,
			Created: pod.CreatedTime(),
			InfraID: infraID,
			Labels:  pod.Labels(),
		}
	}
	return result, nil
}

// Events streams container runtime events.
// For libpod, we use a polling approach since direct event streaming requires more setup.
func (r *LibpodRuntime) Events(ctx context.Context) (<-chan Event, <-chan error) {
	eventChan := make(chan Event)
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errChan)

		// Libpod events require more complex setup; for now use periodic polling
		// by sending empty events that trigger refreshes
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Send a synthetic event to trigger refresh
				event := Event{
					Type:   "refresh",
					Action: "poll",
					Time:   time.Now().Unix(),
				}
				select {
				case eventChan <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return eventChan, errChan
}

// Conversion functions

func convertLibpodContainerList(ctrs []*libpod.Container) ([]ContainerSummary, error) {
	result := make([]ContainerSummary, len(ctrs))
	for i, ctr := range ctrs {
		state, err := ctr.State()
		if err != nil {
			return nil, err
		}
		config := ctr.Config()
		command := ""
		if len(config.Command) > 0 {
			command = config.Command[0]
		}
		// Get pod name if container is in a pod
		podName := ""
		if podID := ctr.PodID(); podID != "" {
			// Try to get pod name from config labels or leave empty
			if pn, ok := config.Labels["io.kubernetes.pod.name"]; ok {
				podName = pn
			}
		}
		result[i] = ContainerSummary{
			ID:      ctr.ID(),
			Names:   []string{ctr.Name()},
			Image:   config.RootfsImageName,
			ImageID: config.RootfsImageID,
			Command: command,
			Created: config.CreatedTime.Unix(),
			State:   state.String(),
			Status:  state.String(),
			Labels:  config.Labels,
			Pod:     ctr.PodID(),
			PodName: podName,
			IsInfra: ctr.IsInfra(),
		}
	}
	return result, nil
}

func convertLibpodContainerInspect(data *define.InspectContainerData) *ContainerDetails {
	if data == nil {
		return nil
	}

	// Get LogPath from HostConfig.LogConfig if available
	logPath := ""
	if data.HostConfig != nil && data.HostConfig.LogConfig != nil {
		logPath = data.HostConfig.LogConfig.Path
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
		LogPath:         logPath,
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
	}

	if data.Config != nil {
		// OnBuild is *string in Podman, convert to []string for our interface
		var onBuild []string
		if data.Config.OnBuild != nil && *data.Config.OnBuild != "" {
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

func convertLibpodContainerStats(stats *define.ContainerStats) ContainerStatsEntry {
	return ContainerStatsEntry{
		Read:    time.Now(),
		PreRead: time.Now().Add(-time.Second),
		CPUStats: CPUStats{
			CPUUsage: CPUUsage{
				TotalUsage: int64(stats.CPU),
			},
			SystemCPUUsage: int64(stats.SystemNano),
		},
		MemoryStats: MemoryStats{
			Usage: int64(stats.MemUsage),
			Limit: int64(stats.MemLimit),
		},
		PidsStats: PidsStats{
			Current: int(stats.PIDs),
		},
		Name: stats.Name,
		ID:   stats.ContainerID,
	}
}

func convertLibpodImageList(ctx context.Context, imgs []*libimage.Image) []ImageSummary {
	result := make([]ImageSummary, len(imgs))
	for i, img := range imgs {
		size, _ := img.Size()           // Ignore error for list view
		labels, _ := img.Labels(ctx)    // Ignore error for list view
		result[i] = ImageSummary{
			ID:       img.ID(),
			RepoTags: img.Names(),
			Created:  img.Created().Unix(),
			Size:     size,
			Labels:   labels,
		}
	}
	return result
}

func convertLibpodImageInspect(ctx context.Context, img *libimage.Image) *ImageDetails {
	if img == nil {
		return nil
	}
	data, err := img.Inspect(ctx, nil)
	if err != nil {
		return nil
	}
	var created time.Time
	if data.Created != nil {
		created = *data.Created
	}
	return &ImageDetails{
		ID:           data.ID,
		RepoTags:     data.RepoTags,
		RepoDigests:  data.RepoDigests,
		Parent:       data.Parent,
		Comment:      data.Comment,
		Created:      created,
		Author:       data.Author,
		Architecture: data.Architecture,
		Os:           data.Os,
		Size:         data.Size,
		VirtualSize:  data.VirtualSize,
	}
}

func convertLibpodImageHistory(history []libimage.ImageHistory) []ImageHistoryEntry {
	result := make([]ImageHistoryEntry, len(history))
	for i, layer := range history {
		created := int64(0)
		if layer.Created != nil {
			created = layer.Created.Unix()
		}
		result[i] = ImageHistoryEntry{
			ID:        layer.ID,
			Created:   created,
			CreatedBy: layer.CreatedBy,
			Tags:      layer.Tags,
			Size:      layer.Size,
			Comment:   layer.Comment,
		}
	}
	return result
}

func convertLibpodVolumeList(vols []*libpod.Volume) []VolumeSummary {
	result := make([]VolumeSummary, len(vols))
	for i, vol := range vols {
		mountpoint, _ := vol.MountPoint()
		result[i] = VolumeSummary{
			Name:       vol.Name(),
			Driver:     vol.Driver(),
			Mountpoint: mountpoint,
			CreatedAt:  vol.CreatedTime(),
			Labels:     vol.Labels(),
			Scope:      vol.Scope(),
			Options:    vol.Options(),
		}
	}
	return result
}

func convertLibpodNetworkList(nets []nettypes.Network) []NetworkSummary {
	result := make([]NetworkSummary, len(nets))
	for i, nw := range nets {
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

// splitFields splits a string by whitespace into fields.
func splitFields(s string) []string {
	var fields []string
	var field []rune
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if len(field) > 0 {
				fields = append(fields, string(field))
				field = nil
			}
		} else {
			field = append(field, r)
		}
	}
	if len(field) > 0 {
		fields = append(fields, string(field))
	}
	return fields
}
