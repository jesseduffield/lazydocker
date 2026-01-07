package system

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/sirupsen/logrus"
)

// Events allows you to monitor libdpod related events like container creation and
// removal.  The events are then passed to the eventChan provided. The optional cancelChan
// can be used to cancel the read of events and close down the HTTP connection.
func Events(ctx context.Context, eventChan chan types.Event, cancelChan chan bool, options *EventsOptions) error {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params, err := options.ToParams()
	if err != nil {
		return err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/events", params, nil)
	if err != nil {
		return err
	}

	if cancelChan != nil {
		go func() {
			<-cancelChan
			if err := response.Body.Close(); err != nil {
				logrus.Errorf("Unable to close event response body: %v", err)
			}
		}()
	}

	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		return response.Process(nil)
	}

	go func() {
		defer response.Body.Close()
		defer close(eventChan)
		dec := json.NewDecoder(response.Body)
		for err = (error)(nil); err == nil; {
			var e = types.Event{}
			err = dec.Decode(&e)
			if err == nil {
				eventChan <- e
			}
		}
	}()
	return nil
}

// Prune removes all unused system data.
func Prune(ctx context.Context, options *PruneOptions) (*types.SystemPruneReport, error) {
	var (
		report types.SystemPruneReport
	)
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/system/prune", params, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}

func Check(ctx context.Context, options *CheckOptions) (*types.SystemCheckReport, error) {
	var report types.SystemCheckReport

	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/system/check", params, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}

func Version(ctx context.Context, options *VersionOptions) (*types.SystemVersionReport, error) {
	var (
		component types.SystemComponentVersion
		report    types.SystemVersionReport
	)
	if options == nil {
		options = new(VersionOptions)
	}
	_ = options
	version, err := define.GetVersion()
	if err != nil {
		return nil, err
	}
	report.Client = &version

	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/version", nil, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if err = response.Process(&component); err != nil {
		return nil, err
	}

	b, _ := time.Parse(time.RFC3339, component.BuildTime)
	report.Server = &define.Version{
		APIVersion: component.APIVersion,
		Version:    component.Version.Version,
		GoVersion:  component.GoVersion,
		GitCommit:  component.GitCommit,
		BuiltTime:  time.Unix(b.Unix(), 0).Format(time.ANSIC),
		Built:      b.Unix(),
		OsArch:     fmt.Sprintf("%s/%s", component.Os, component.Arch),
		Os:         component.Os,
	}

	for _, c := range component.Components {
		if c.Name == "Podman Engine" {
			report.Server.APIVersion = c.Details["APIVersion"]
		}
	}
	return &report, err
}

// DiskUsage returns information about image, container, and volume disk
// consumption
func DiskUsage(ctx context.Context, options *DiskOptions) (*types.SystemDfReport, error) {
	var report types.SystemDfReport
	if options == nil {
		options = new(DiskOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/system/df", nil, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &report, response.Process(&report)
}
