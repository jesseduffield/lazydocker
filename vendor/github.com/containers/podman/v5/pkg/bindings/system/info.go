package system

import (
	"context"
	"net/http"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings"
)

// Info returns information about the libpod environment and its stores
func Info(ctx context.Context, _ *InfoOptions) (*define.Info, error) {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/info", nil, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	info := define.Info{}
	return &info, response.Process(&info)
}
