package containers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"go.podman.io/storage/pkg/regexp"
)

var iidRegex = regexp.Delayed(`^[0-9a-f]{12}`)

// Commit creates a container image from a container.  The container is defined by nameOrID.  Use
// the CommitOptions for finer grain control on characteristics of the resulting image.
func Commit(ctx context.Context, nameOrID string, options *CommitOptions) (types.IDResponse, error) {
	if options == nil {
		options = new(CommitOptions)
	}
	id := types.IDResponse{}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return id, err
	}
	params, err := options.ToParams()
	if err != nil {
		return types.IDResponse{}, err
	}
	params.Set("container", nameOrID)
	var requestBody io.Reader
	if options.Config != nil {
		requestBody = *options.Config
	}
	response, err := conn.DoRequest(ctx, requestBody, http.MethodPost, "/commit", params, nil)
	if err != nil {
		return id, err
	}
	defer response.Body.Close()

	if !response.IsSuccess() {
		return id, response.Process(err)
	}

	if !options.GetStream() {
		return id, response.Process(&id)
	}
	stderr := os.Stderr
	body := response.Body.(io.Reader)
	dec := json.NewDecoder(body)
	for {
		var s images.BuildResponse
		select {
		// FIXME(vrothberg): it seems we always hit the EOF case below,
		// even when the server quit but it seems desirable to
		// distinguish a proper build from a transient EOF.
		case <-response.Request.Context().Done():
			return id, nil
		default:
			// non-blocking select
		}

		if err := dec.Decode(&s); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return id, fmt.Errorf("server probably quit: %w", err)
			}
			// EOF means the stream is over in which case we need
			// to have read the id.
			if errors.Is(err, io.EOF) && id.ID != "" {
				break
			}
			return id, fmt.Errorf("decoding stream: %w", err)
		}

		switch {
		case s.Stream != "":
			raw := []byte(s.Stream)
			stderr.Write(raw)
			if iidRegex.Match(raw) {
				id.ID = strings.TrimSuffix(s.Stream, "\n")
				return id, nil
			}
		case s.Error != nil:
			// If there's an error, return directly.  The stream
			// will be closed on return.
			return id, errors.New(s.Error.Message)
		default:
			return id, errors.New("failed to parse build results stream, unexpected input")
		}
	}
	return id, response.Process(&id)
}
