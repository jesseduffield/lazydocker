package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/internal/tarfile"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/types"
)

type daemonImageDestination struct {
	ref                  daemonReference
	mustMatchRuntimeOS   bool
	*tarfile.Destination // Implements most of types.ImageDestination
	archive              *tarfile.Writer
	// For talking to imageLoadGoroutine
	goroutineCancel context.CancelFunc
	statusChannel   <-chan error
	writer          *io.PipeWriter
	// Other state
	committed bool // writer has been closed
}

// newImageDestination returns a types.ImageDestination for the specified image reference.
func newImageDestination(ctx context.Context, sys *types.SystemContext, ref daemonReference) (private.ImageDestination, error) {
	if ref.ref == nil {
		return nil, fmt.Errorf("Invalid destination docker-daemon:%s: a destination must be a name:tag", ref.StringWithinTransport())
	}
	namedTaggedRef, ok := ref.ref.(reference.NamedTagged)
	if !ok {
		return nil, fmt.Errorf("Invalid destination docker-daemon:%s: a destination must be a name:tag", ref.StringWithinTransport())
	}

	var mustMatchRuntimeOS = true
	if sys != nil && sys.DockerDaemonHost != client.DefaultDockerHost {
		mustMatchRuntimeOS = false
	}

	c, err := newDockerClient(sys)
	if err != nil {
		return nil, fmt.Errorf("initializing docker engine client: %w", err)
	}

	reader, writer := io.Pipe()
	archive := tarfile.NewWriter(writer)
	// Commit() may never be called, so we may never read from this channel; so, make this buffered to allow imageLoadGoroutine to write status and terminate even if we never read it.
	statusChannel := make(chan error, 1)

	goroutineContext, goroutineCancel := context.WithCancel(ctx)
	go imageLoadGoroutine(goroutineContext, c, reader, statusChannel)

	d := &daemonImageDestination{
		ref:                ref,
		mustMatchRuntimeOS: mustMatchRuntimeOS,
		archive:            archive,
		goroutineCancel:    goroutineCancel,
		statusChannel:      statusChannel,
		writer:             writer,
		committed:          false,
	}
	d.Destination = tarfile.NewDestination(sys, archive, ref.Transport().Name(), namedTaggedRef, d.CommitWithOptions)
	return d, nil
}

// imageLoadGoroutine accepts tar stream on reader, sends it to c, and reports error or success by writing to statusChannel
func imageLoadGoroutine(ctx context.Context, c *client.Client, reader *io.PipeReader, statusChannel chan<- error) {
	defer c.Close()
	err := errors.New("Internal error: unexpected panic in imageLoadGoroutine")
	defer func() {
		logrus.Debugf("docker-daemon: sending done, status %v", err)
		statusChannel <- err
	}()
	defer func() {
		if err == nil {
			reader.Close()
		} else {
			if err := reader.CloseWithError(err); err != nil {
				logrus.Debugf("imageLoadGoroutine: Error during reader.CloseWithError: %v", err)
			}
		}
	}()

	err = imageLoad(ctx, c, reader)
}

// imageLoad accepts tar stream on reader and sends it to c
func imageLoad(ctx context.Context, c *client.Client, reader *io.PipeReader) error {
	resp, err := c.ImageLoad(ctx, reader, client.ImageLoadWithQuiet(true))
	if err != nil {
		return fmt.Errorf("starting a load operation in docker engine: %w", err)
	}
	defer resp.Body.Close()

	// jsonError and jsonMessage are small subsets of docker/docker/pkg/jsonmessage.JSONError and JSONMessage,
	// copied here to minimize dependencies.
	type jsonError struct {
		Message string `json:"message,omitempty"`
	}
	type jsonMessage struct {
		Error *jsonError `json:"errorDetail,omitempty"`
	}

	dec := json.NewDecoder(resp.Body)
	for {
		var msg jsonMessage
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("parsing docker load progress: %w", err)
		}
		if msg.Error != nil {
			return fmt.Errorf("docker engine reported: %q", msg.Error.Message)
		}
	}
	return nil // No error reported = success
}

// DesiredLayerCompression indicates if layers must be compressed, decompressed or preserved
func (d *daemonImageDestination) DesiredLayerCompression() types.LayerCompression {
	return types.PreserveOriginal
}

// MustMatchRuntimeOS returns true iff the destination can store only images targeted for the current runtime architecture and OS. False otherwise.
func (d *daemonImageDestination) MustMatchRuntimeOS() bool {
	return d.mustMatchRuntimeOS
}

// Close removes resources associated with an initialized ImageDestination, if any.
func (d *daemonImageDestination) Close() error {
	if !d.committed {
		logrus.Debugf("docker-daemon: Closing tar stream to abort loading")
		// In principle, goroutineCancel() should abort the HTTP request and stop the process from continuing.
		// In practice, though, various HTTP implementations used by client.Client.ImageLoad() (including
		// https://github.com/golang/net/blob/master/context/ctxhttp/ctxhttp_pre17.go and the
		// net/http version with native Context support in Go 1.7) do not always actually immediately cancel
		// the operation: they may process the HTTP request, or a part of it, to completion in a goroutine, and
		// return early if the context is canceled without terminating the goroutine at all.
		// So we need this CloseWithError to terminate sending the HTTP request Body
		// immediately, and hopefully, through terminating the sending which uses "Transfer-Encoding: chunked"" without sending
		// the terminating zero-length chunk, prevent the docker daemon from processing the tar stream at all.
		// Whether that works or not, closing the PipeWriter seems desirable in any case.
		if err := d.writer.CloseWithError(errors.New("Aborting upload, daemonImageDestination closed without a previous .CommitWithOptions()")); err != nil {
			return err
		}
	}
	d.goroutineCancel()

	return nil
}

func (d *daemonImageDestination) Reference() types.ImageReference {
	return d.ref
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (d *daemonImageDestination) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	logrus.Debugf("docker-daemon: Closing tar stream")
	if err := d.archive.Close(); err != nil {
		return err
	}
	if err := d.writer.Close(); err != nil {
		return err
	}
	d.committed = true // We may still fail, but we are done sending to imageLoadGoroutine.

	logrus.Debugf("docker-daemon: Waiting for status")
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-d.statusChannel:
		return err
	}
}
