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
	imageTypes "go.podman.io/image/v5/types"
)

// Push is the binding for libpod's endpoints for push images.  Note that
// `source` must be a referring to an image in the remote's container storage.
// The destination must be a reference to a registry (i.e., of docker transport
// or be normalized to one).  Other transports are rejected as they do not make
// sense in a remote context.
func Push(ctx context.Context, source string, destination string, options *PushOptions) error {
	if options == nil {
		options = new(PushOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	header, err := auth.MakeXRegistryAuthHeader(&imageTypes.SystemContext{AuthFilePath: options.GetAuthfile()}, options.GetUsername(), options.GetPassword())
	if err != nil {
		return err
	}

	params, err := options.ToParams()
	if err != nil {
		return err
	}
	// SkipTLSVerify is special.  It's not being serialized by ToParams()
	// because we need to flip the boolean.
	if options.SkipTLSVerify != nil {
		params.Set("tlsVerify", strconv.FormatBool(!options.GetSkipTLSVerify()))
	}
	params.Set("destination", destination)

	path := fmt.Sprintf("/images/%s/push", source)
	response, err := conn.DoRequest(ctx, nil, http.MethodPost, path, params, header)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if !response.IsSuccess() {
		return response.Process(err)
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
LOOP:
	for {
		var report types.ImagePushStream
		if err := dec.Decode(&report); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("failed to decode message from stream: %w", err)
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
		case report.ManifestDigest != "":
			options.ManifestDigest = &report.ManifestDigest
		case report.Error != "":
			// There can only be one error.
			return errors.New(report.Error)
		default:
			return fmt.Errorf("failed to parse push results stream, unexpected input: %v", report)
		}
	}

	return nil
}
