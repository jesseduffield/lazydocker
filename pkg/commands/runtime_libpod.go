//go:build linux && cgo

package commands

import (
	"context"
	"time"

	"github.com/containers/podman/v5/libpod"
	"github.com/containers/podman/v5/libpod/define"
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
		_, err := r.runtime.Shutdown(false)
		return err
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
	result, err := ctr.Top([]string{})
	if err != nil {
		return nil, nil, err
	}
	if len(result) == 0 {
		return nil, nil, nil
	}
	return result[0], result[1:], nil
}

// PruneContainers removes all stopped containers.
func (r *LibpodRuntime) PruneContainers(ctx context.Context) error {
	_, err := r.runtime.PruneContainers(ctx, nil)
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
	imgs, err := r.runtime.ImageRuntime().GetImages()
	if err != nil {
		return nil, err
	}
	return convertLibpodImageList(imgs), nil
}

// InspectImage returns detailed information about an image.
func (r *LibpodRuntime) InspectImage(ctx context.Context, id string) (*ImageDetails, error) {
	img, _, err := r.runtime.ImageRuntime().LookupImage(id, nil)
	if err != nil {
		return nil, err
	}
	return convertLibpodImageInspect(img), nil
}

// ImageHistory returns the history of an image.
func (r *LibpodRuntime) ImageHistory(ctx context.Context, id string) ([]ImageHistoryEntry, error) {
	img, _, err := r.runtime.ImageRuntime().LookupImage(id, nil)
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
	img, _, err := r.runtime.ImageRuntime().LookupImage(id, nil)
	if err != nil {
		return err
	}
	_, err = r.runtime.ImageRuntime().RemoveImages(ctx, []string{img.ID()}, &libimage.RemoveImagesOptions{Force: force})
	return err
}

// PruneImages removes unused images.
func (r *LibpodRuntime) PruneImages(ctx context.Context) error {
	_, err := r.runtime.ImageRuntime().PruneImages(ctx, nil)
	return err
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
	_, err := r.runtime.Network().NetworkRemove(name)
	return err
}

// PruneNetworks removes unused networks.
func (r *LibpodRuntime) PruneNetworks(ctx context.Context) error {
	_, err := r.runtime.Network().NetworkPrune(nil)
	return err
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
		}
	}
	return result, nil
}

func convertLibpodContainerInspect(data *define.InspectContainerData) *ContainerDetails {
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
		LogPath:         data.LogPath,
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
		details.Config = &ContainerConfig{
			Hostname:     data.Config.Hostname,
			Domainname:   data.Config.Domainname,
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
			OnBuild:      data.Config.OnBuild,
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

func convertLibpodImageList(imgs []*libimage.Image) []ImageSummary {
	result := make([]ImageSummary, len(imgs))
	for i, img := range imgs {
		result[i] = ImageSummary{
			ID:       img.ID(),
			RepoTags: img.Names(),
			Created:  img.Created().Unix(),
			Size:     img.Size(),
			Labels:   img.Labels(),
		}
	}
	return result
}

func convertLibpodImageInspect(img *libimage.Image) *ImageDetails {
	if img == nil {
		return nil
	}
	return &ImageDetails{
		ID:           img.ID(),
		RepoTags:     img.Names(),
		Created:      img.Created(),
		Size:         img.Size(),
		Architecture: img.Architecture(),
		Os:           img.OS(),
	}
}

func convertLibpodImageHistory(history []*libimage.ImageHistoryLayer) []ImageHistoryEntry {
	result := make([]ImageHistoryEntry, len(history))
	for i, layer := range history {
		result[i] = ImageHistoryEntry{
			ID:        layer.ID,
			Created:   layer.Created.Unix(),
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
		result[i] = VolumeSummary{
			Name:       vol.Name(),
			Driver:     vol.Driver(),
			Mountpoint: vol.MountPoint(),
			CreatedAt:  vol.CreatedTime(),
			Labels:     vol.Labels(),
			Scope:      vol.Scope(),
			Options:    vol.Options(),
		}
	}
	return result
}

func convertLibpodNetworkList(nets []types.Network) []NetworkSummary {
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
