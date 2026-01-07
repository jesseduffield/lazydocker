package containers

import (
	"context"
	"net/http"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings"
)

// RunHealthCheck executes the container's healthcheck and returns the health status of the
// container.
func RunHealthCheck(ctx context.Context, nameOrID string, options *HealthCheckOptions) (*define.HealthCheckResults, error) {
	if options == nil {
		options = new(HealthCheckOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	var (
		status define.HealthCheckResults
	)
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/%s/healthcheck", nil, nil, nameOrID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	return &status, response.Process(&status)
}
