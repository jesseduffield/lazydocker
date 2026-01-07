package images

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/containers/podman/v5/pkg/auth"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/containers/podman/v5/pkg/errorhandling"
	imgTypes "go.podman.io/image/v5/types"
)

// Pull is the binding for libpod's v2 endpoints for pulling images.  Note that
// `rawImage` must be a reference to a registry (i.e., of docker transport or be
// normalized to one).  Other transports are rejected as they do not make sense
// in a remote context. Progress reported on stderr
func Pull(ctx context.Context, rawImage string, options *PullOptions) ([]string, error) {
	if options == nil {
		options = new(PullOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params, err := options.ToParams()
	if err != nil {
		return nil, err
	}
	params.Set("reference", rawImage)

	// SkipTLSVerify is special.  It's not being serialized by ToParams()
	// because we need to flip the boolean.
	if options.SkipTLSVerify != nil {
		params.Set("tlsVerify", strconv.FormatBool(!options.GetSkipTLSVerify()))
	}

	header, err := auth.MakeXRegistryAuthHeader(&imgTypes.SystemContext{AuthFilePath: options.GetAuthfile()}, options.GetUsername(), options.GetPassword())
	if err != nil {
		return nil, err
	}

	response, err := conn.DoRequest(ctx, nil, http.MethodPost, "/images/pull", params, header)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if !response.IsSuccess() {
		return nil, response.Process(err)
	}

	var writer io.Writer
	if options.GetQuiet() {
		writer = io.Discard
	} else if progressWriter := options.GetProgressWriter(); progressWriter != nil {
		writer = progressWriter
	} else {
		// Historically push writes status to stderr
		writer = os.Stderr
	}

	dec := json.NewDecoder(response.Body)
	var images []string
	var pullErrors []error
LOOP:
	for {
		var report types.ImagePullReport
		if err := dec.Decode(&report); err != nil {
			if errors.Is(err, io.EOF) {
				// end of stream, exit loop
				break
			}
			// Decoder error, it is unlikely that the next call would work again
			// so exit here as well, the Decoder can store the error and always
			// return the same one for all future calls which then causes a
			// infinity loop and memory leak in pullErrors.
			// https://github.com/containers/podman/issues/25974
			pullErrors = append(pullErrors, fmt.Errorf("failed to decode message from stream: %w", err))
			break
		}

		select {
		case <-response.Request.Context().Done():
			break LOOP
		default:
			// non-blocking select
		}

		switch {
		case report.Stream != "":
			fmt.Fprint(writer, report.Stream)
		case report.Error != "":
			pullErrors = append(pullErrors, errors.New(report.Error))
		case len(report.Images) > 0:
			images = report.Images
		case report.ID != "":
		default:
			return images, fmt.Errorf("failed to parse pull results stream, unexpected input: %v", report)
		}
	}
	return images, errorhandling.JoinErrors(pullErrors)
}
