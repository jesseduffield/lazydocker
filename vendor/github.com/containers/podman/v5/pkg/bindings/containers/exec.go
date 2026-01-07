package containers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/api/handlers"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	jsoniter "github.com/json-iterator/go"
	"github.com/sirupsen/logrus"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// ExecCreate creates a new exec session in an existing container.
// The exec session will not be started; that is done with ExecStart.
// Returns ID of new exec session, or an error if one occurred.
func ExecCreate(ctx context.Context, nameOrID string, config *handlers.ExecCreateConfig) (string, error) {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return "", err
	}

	if config == nil {
		return "", errors.New("must provide a configuration for exec session")
	}

	requestJSON, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshalling exec config to JSON: %w", err)
	}
	jsonReader := strings.NewReader(string(requestJSON))

	resp, err := conn.DoRequest(ctx, jsonReader, http.MethodPost, "/containers/%s/exec", nil, nil, nameOrID)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respStruct := new(types.IDResponse)
	if err := resp.Process(respStruct); err != nil {
		return "", err
	}

	return respStruct.ID, nil
}

// ExecInspect inspects an existing exec session, returning detailed information
// about it.
func ExecInspect(ctx context.Context, sessionID string, options *ExecInspectOptions) (*define.InspectExecSession, error) {
	if options == nil {
		options = new(ExecInspectOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	logrus.Debugf("Inspecting session ID %q", sessionID)

	resp, err := conn.DoRequest(ctx, nil, http.MethodGet, "/exec/%s/json", nil, nil, sessionID)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respStruct := new(define.InspectExecSession)
	if err := resp.Process(respStruct); err != nil {
		return nil, err
	}

	return respStruct, nil
}

// ExecStart starts (but does not attach to) a given exec session.
func ExecStart(ctx context.Context, sessionID string, options *ExecStartOptions) error {
	if options == nil {
		options = new(ExecStartOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}

	logrus.Debugf("Starting exec session ID %q", sessionID)

	// We force Detach to true
	body := struct {
		Detach bool `json:"Detach"`
	}{
		Detach: true,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return err
	}

	resp, err := conn.DoRequest(ctx, bytes.NewReader(bodyJSON), http.MethodPost, "/exec/%s/start", nil, nil, sessionID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return resp.Process(nil)
}

// ExecRemove removes a given exec session.
func ExecRemove(ctx context.Context, sessionID string, options *ExecRemoveOptions) error {
	v := bindings.ServiceVersion(ctx)
	// The exec remove endpoint was added in 4.8.
	if v.Major < 4 || (v.Major == 4 && v.Minor < 8) {
		// Do no call this endpoint as it will not be supported on the server and throw an "NOT FOUND" error.
		return bindings.NewAPIVersionError("/exec/{id}/remove", v, "4.8.0")
	}
	if options == nil {
		options = new(ExecRemoveOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}

	logrus.Debugf("Removing exec session ID %q", sessionID)

	// We force Detach to true
	body := struct {
		Force bool `json:"Force"`
	}{
		Force: false,
	}

	if options.Force != nil {
		body.Force = *options.Force
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return err
	}

	resp, err := conn.DoRequest(ctx, bytes.NewReader(bodyJSON), http.MethodPost, "/exec/%s/remove", nil, nil, sessionID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return resp.Process(nil)
}
