package images

import (
	"context"
	"net/http"

	handlersTypes "github.com/containers/podman/v5/pkg/api/handlers/types"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/containers/podman/v5/pkg/errorhandling"
)

// Remove removes one or more images from the local storage.  Use optional force option to remove an
// image, even if it's used by containers.
func Remove(ctx context.Context, images []string, options *RemoveOptions) (*types.ImageRemoveReport, []error) {
	if options == nil {
		options = new(RemoveOptions)
	}
	var report handlersTypes.LibpodImagesRemoveReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, []error{err}
	}

	params, err := options.ToParams()
	if err != nil {
		return nil, nil
	}
	for _, image := range images {
		params.Add("images", image)
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodDelete, "/images/remove", params, nil)
	if err != nil {
		return nil, []error{err}
	}
	defer response.Body.Close()

	if err := response.Process(&report); err != nil {
		return nil, []error{err}
	}

	return &report.ImageRemoveReport, errorhandling.StringsToErrors(report.Errors)
}
