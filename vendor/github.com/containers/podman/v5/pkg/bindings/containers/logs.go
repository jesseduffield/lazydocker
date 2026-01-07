package containers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/containers/podman/v5/pkg/bindings"
)

// Logs obtains a container's logs given the options provided.  The logs are then sent to the
// stdout|stderr channels as strings.
func Logs(ctx context.Context, nameOrID string, options *LogOptions, stdoutChan, stderrChan chan string) error {
	if options == nil {
		options = new(LogOptions)
	}
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return err
	}
	params, err := options.ToParams()
	if err != nil {
		return err
	}
	// The API requires either stdout|stderr be used. If neither are specified, we specify stdout
	if options.Stdout == nil && options.Stderr == nil {
		params.Set("stdout", strconv.FormatBool(true))
	}
	response, err := conn.DoRequest(ctx, nil, http.MethodGet, "/containers/%s/logs", params, nil, nameOrID)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	// if not success handle and return possible error message
	if !response.IsSuccess() && !response.IsInformational() {
		return response.Process(nil)
	}

	buffer := make([]byte, 1024)
	for {
		fd, l, err := DemuxHeader(response.Body, buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		frame, err := DemuxFrame(response.Body, buffer, l)
		if err != nil {
			return err
		}

		switch fd {
		case 0:
			stdoutChan <- string(frame)
		case 1:
			stdoutChan <- string(frame)
		case 2:
			stderrChan <- string(frame)
		case 3:
			return errors.New("from service in stream: " + string(frame))
		default:
			return fmt.Errorf("unrecognized input header: %d", fd)
		}
	}
}
