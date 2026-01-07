package containers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/api/handlers"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/reports"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

var (
	ErrLostSync = errors.New("lost synchronization with multiplexed stream")
)

// List obtains a list of containers in local storage.  All parameters to this method are optional.
// The filters are used to determine which containers are listed. The last parameter indicates to only return
// the most recent number of containers.  The pod and size booleans indicate that pod information and rootfs
// size information should also be included.  Finally, the sync bool synchronizes the OCI runtime and
// container state.
func List(ctx context.Context, options *ListOptions) ([]types.ListContainer, error) {
	if options == nil {
		options = new(ListOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	var containers []types.ListContainer
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/json", params, nil)
	if err != nil {
		return containers, err
	}
	defer response.Body.Close()

	return containers, response.Process(&containers)
}

// Prune removes stopped and exited containers from local storage.  The optional filters can be
// used for more granular selection of containers.  The main error returned indicates if there were runtime
// errors like finding containers.  Errors specific to the removal of a container are in the PruneContainerResponse
// structure.
func Prune(ctx context.Context, options *PruneOptions) ([]*reports.PruneReport, error) {
	if options == nil {
		options = new(PruneOptions)
	}
	var reports []*reports.PruneReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/prune", params, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return reports, response.Process(&reports)
}

// Remove removes a container from local storage.  The force bool designates
// that the container should be removed forcibly (example, even it is running).
// The volumes bool dictates that a container's volumes should also be removed.
// The All option indicates that all containers should be removed
// The Ignore option indicates that if a container did not exist, ignore the error
func Remove(ctx context.Context, nameOrID string, options *RemoveOptions) ([]*reports.RmReport, error) {
	if options == nil {
		options = new(RemoveOptions)
	}
	var reports []*reports.RmReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return reports, err
	}
	params, err := options.ToParams()
	if err != nil {
		return reports, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodDelete, "/containers/%s", params, nil, nameOrID)
	if err != nil {
		return reports, err
	}
	defer response.Body.Close()

	return reports, response.Process(&reports)
}

// Inspect returns low level information about a Container.  The nameOrID can be a container name
// or a partial/full ID.  The size bool determines whether the size of the container's root filesystem
// should be calculated.  Calculating the size of a container requires extra work from the filesystem and
// is therefore slower.
func Inspect(ctx context.Context, nameOrID string, options *InspectOptions) (*define.InspectContainerData, error) {
	if options == nil {
		options = new(InspectOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/%s/json", params, nil, nameOrID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	inspect := define.InspectContainerData{}
	return &inspect, response.Process(&inspect)
}

// Kill sends a given signal to a given container.  The signal should be the string
// representation of a signal like 'SIGKILL'. The nameOrID can be a container name
// or a partial/full ID
func Kill(ctx context.Context, nameOrID string, options *KillOptions) error {
	if options == nil {
		options = new(KillOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params, err := options.ToParams()
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/kill", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

// Pause pauses a given container.  The nameOrID can be a container name
// or a partial/full ID.
func Pause(ctx context.Context, nameOrID string, options *PauseOptions) error {
	if options == nil {
		options = new(PauseOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/pause", nil, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

// Restart restarts a running container. The nameOrID can be a container name
// or a partial/full ID.  The optional timeout specifies the number of seconds to wait
// for the running container to stop before killing it.
func Restart(ctx context.Context, nameOrID string, options *RestartOptions) error {
	if options == nil {
		options = new(RestartOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params, err := options.ToParams()
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/restart", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

// Start starts a non-running container.The nameOrID can be a container name
// or a partial/full ID. The optional parameter for detach keys are to override the default
// detach key sequence.
func Start(ctx context.Context, nameOrID string, options *StartOptions) error {
	if options == nil {
		options = new(StartOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params, err := options.ToParams()
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/start", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

func Stats(ctx context.Context, containers []string, options *StatsOptions) (chan types.ContainerStatsReport, error) {
	if options == nil {
		options = new(StatsOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		params.Add("containers", c)
	}

	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/stats", params, nil)
	if err != nil {
		return nil, err
	}
	if !response.IsSuccess() {
		return nil, response.Process(nil)
	}

	statsChan := make(chan types.ContainerStatsReport)

	go func() {
		defer close(statsChan)
		defer response.Body.Close()

		dec := json.NewDecoder(response.Body)
		doStream := true
		if options.Changed("Stream") {
			doStream = options.GetStream()
		}

	streamLabel: // label to flatten the scope
		select {
		case <-response.Request.Context().Done():
			return // lost connection - maybe the server quit
		default:
			// fall through and do some work
		}

		var report types.ContainerStatsReport
		if err := dec.Decode(&report); err != nil {
			report = types.ContainerStatsReport{Error: err}
		}
		statsChan <- report

		if report.Error != nil || !doStream {
			return
		}
		goto streamLabel
	}()

	return statsChan, nil
}

// Top gathers statistics about the running processes in a container. The nameOrID can be a container name
// or a partial/full ID.  The descriptors allow for specifying which data to collect from the process.
func Top(ctx context.Context, nameOrID string, options *TopOptions) ([]string, error) {
	if options == nil {
		options = new(TopOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	if options.Changed("Descriptors") {
		psArgs := options.GetDescriptors()
		for _, arg := range psArgs {
			params.Add("ps_args", arg)
		}
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/%s/top", params, nil, nameOrID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body := handlers.ContainerTopOKBody{}
	if err = response.Process(&body); err != nil {
		return nil, err
	}

	// handlers.ContainerTopOKBody{} returns a slice of slices where each cell in the top table is an item.
	// In libpod land, we're just using a slice with cells being split by tabs, which allows for an idiomatic
	// usage of the tabwriter.
	topOutput := []string{strings.Join(body.Titles, "\t")}
	for _, out := range body.Processes {
		topOutput = append(topOutput, strings.Join(out, "\t"))
	}

	return topOutput, err
}

// Unpause resumes the given paused container.  The nameOrID can be a container name
// or a partial/full ID.
func Unpause(ctx context.Context, nameOrID string, options *UnpauseOptions) error {
	if options == nil {
		options = new(UnpauseOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/unpause", nil, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	return response.Process(nil)
}

// Wait blocks until the given container reaches a condition. If not provided, the condition will
// default to stopped.  If the condition is stopped, an exit code for the container will be provided. The
// nameOrID can be a container name or a partial/full ID.
func Wait(ctx context.Context, nameOrID string, options *WaitOptions) (int32, error) {
	if options == nil {
		options = new(WaitOptions)
	} else if len(options.Condition) > 0 && len(options.Conditions) > 0 {
		return -1, fmt.Errorf("%q field cannot be used with deprecated %q field", "Conditions", "Condition")
	}
	var exitCode int32
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return exitCode, err
	}
	params, err := options.ToParams()
	if err != nil {
		return exitCode, err
	}
	delete(params, "conditions") // They're called "condition"
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/wait", params, nil, nameOrID)
	if err != nil {
		return exitCode, err
	}
	defer response.Body.Close()

	return exitCode, response.Process(&exitCode)
}

// Exists is a quick, light-weight way to determine if a given container
// exists in local storage.  The nameOrID can be a container name
// or a partial/full ID.
func Exists(ctx context.Context, nameOrID string, options *ExistsOptions) (bool, error) {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return false, err
	}
	params, err := options.ToParams()
	if err != nil {
		return false, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/%s/exists", params, nil, nameOrID)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	return response.IsSuccess(), nil
}

// Stop stops a running container.  The timeout is optional. The nameOrID can be a container name
// or a partial/full ID
func Stop(ctx context.Context, nameOrID string, options *StopOptions) error {
	if options == nil {
		options = new(StopOptions)
	}
	params, err := options.ToParams()
	if err != nil {
		return err
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/stop", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if options.GetIgnore() && response.StatusCode == http.StatusNotFound {
		return nil
	}

	return response.Process(nil)
}

// Export creates a tarball of the given name or ID of a container.  It
// requires an io.Writer be provided to write the tarball.
func Export(ctx context.Context, nameOrID string, w io.Writer, options *ExportOptions) error {
	if options == nil {
		options = new(ExportOptions)
	}
	_ = options
	params := url.Values{}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/%s/export", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.IsSuccess() {
		_, err = io.Copy(w, response.Body)
		return err
	}
	return response.Process(nil)
}

// ContainerInit takes a created container and executes all of the
// preparations to run the container except it will not start
// or attach to the container
func ContainerInit(ctx context.Context, nameOrID string, options *InitOptions) error {
	if options == nil {
		options = new(InitOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/containers/%s/init", nil, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotModified {
		return fmt.Errorf("container %s has already been created in runtime: %w", nameOrID, define.ErrCtrStateInvalid)
	}
	return response.Process(nil)
}

// Deprecated: This function always returns false, the server API endpoint never existed.
func ShouldRestart(_ context.Context, _ string, _ *ShouldRestartOptions) (bool, error) {
	return false, nil
}
