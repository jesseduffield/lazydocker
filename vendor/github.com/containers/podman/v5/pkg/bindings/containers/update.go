package containers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/containers/podman/v5/pkg/api/handlers"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	jsoniter "github.com/json-iterator/go"
)

func Update(ctx context.Context, options *types.ContainerUpdateOptions) (string, error) {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return "", err
	}

	params := url.Values{}
	if options.RestartPolicy != nil {
		params.Set("restartPolicy", *options.RestartPolicy)
		if options.RestartRetries != nil {
			params.Set("restartRetries", strconv.Itoa(int(*options.RestartRetries)))
		}
	}

	updateEntities := &handlers.UpdateEntities{
		Env:      options.Env,
		UnsetEnv: options.UnsetEnv,
	}
	if options.Resources != nil {
		updateEntities.LinuxResources = *options.Resources
	}
	if options.ChangedHealthCheckConfiguration != nil {
		updateEntities.UpdateHealthCheckConfig = *options.ChangedHealthCheckConfiguration
	}
	if options.DevicesLimits != nil {
		updateEntities.UpdateContainerDevicesLimits = *options.DevicesLimits
	}

	requestData, err := jsoniter.MarshalToString(updateEntities)
	if err != nil {
		return "", err
	}
	stringReader := strings.NewReader(requestData)
	response, err := conn.DoRequest(ctx, stringReader, http.MethodPost, "/containers/%s/update", params, nil, options.NameOrID)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	return options.NameOrID, response.Process(nil)
}
