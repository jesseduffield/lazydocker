package containers

import (
	"context"
	"net/http"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/containers/podman/v5/pkg/specgen"
	jsoniter "github.com/json-iterator/go"
)

func CreateWithSpec(ctx context.Context, s *specgen.SpecGenerator, options *CreateOptions) (types.ContainerCreateResponse, error) {
	var ccr types.ContainerCreateResponse
	if options == nil {
		options = new(CreateOptions)
	}
	_ = options
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return ccr, err
	}
	specgenString, err := jsoniter.MarshalToString(s)
	if err != nil {
		return ccr, err
	}
	stringReader := strings.NewReader(specgenString)
	response, err := conn.DoRequest(ctx, stringReader, http.MethodPost, "/containers/create", nil, nil)
	if err != nil {
		return ccr, err
	}
	defer response.Body.Close()

	return ccr, response.Process(&ccr)
}
