//go:build !remote

package libimage

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
	dockerArchiveTransport "go.podman.io/image/v5/docker/archive"
	dockerDaemonTransport "go.podman.io/image/v5/docker/daemon"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/transports/alltransports"
)

// PushOptions allows for customizing image pushes.
type PushOptions struct {
	CopyOptions
}

// Push pushes the specified source which must refer to an image in the local
// containers storage.  It may or may not have the `containers-storage:`
// prefix.  Use destination to push to a custom destination.  The destination
// can refer to any supported transport.  If not transport is specified, the
// docker transport (i.e., a registry) is implied.  If destination is left
// empty, the docker destination will be extrapolated from the source.
//
// Return storage.ErrImageUnknown if source could not be found in the local
// containers storage.
func (r *Runtime) Push(ctx context.Context, source, destination string, options *PushOptions) ([]byte, error) {
	if options == nil {
		options = &PushOptions{}
	}

	defaultConfig, err := config.Default()
	if err != nil {
		return nil, err
	}
	if options.MaxRetries == nil {
		options.MaxRetries = &defaultConfig.Engine.Retry
	}
	if options.RetryDelay == nil {
		if defaultConfig.Engine.RetryDelay != "" {
			duration, err := time.ParseDuration(defaultConfig.Engine.RetryDelay)
			if err != nil {
				return nil, fmt.Errorf("failed to parse containers.conf retry_delay: %w", err)
			}
			options.RetryDelay = &duration
		}
	}

	// Look up the local image.  Note that we need to ignore the platform
	// and push what the user specified (containers/podman/issues/10344).
	image, resolvedSource, err := r.LookupImage(source, nil)
	if err != nil {
		return nil, err
	}

	srcRef, err := image.StorageReference()
	if err != nil {
		return nil, err
	}

	// Make sure we have a proper destination, and parse it into an image
	// reference for copying.
	if destination == "" {
		// Doing an ID check here is tempting but false positives (due
		// to a short partial IDs) are more painful than false
		// negatives.
		destination = resolvedSource
	}

	logrus.Debugf("Pushing image %s to %s", source, destination)

	destRef, err := alltransports.ParseImageName(destination)
	if err != nil {
		// If the input does not include a transport assume it refers
		// to a registry.
		dockerRef, dockerErr := alltransports.ParseImageName("docker://" + destination)
		if dockerErr != nil {
			return nil, err
		}
		destRef = dockerRef
	}

	// docker-archive and DockerV2Schema2MediaType support only Gzip compression
	// If the CompressionFormat has come from containers.conf (set as a default),
	// but isn't supported for this push, we want to ignore it.
	// If the CompressionFormat has come from the CLI (ForceCompressionFormat
	// requires CompressionFormat to be set), we want to strip the invalid value
	// so that the push attempt fails.
	//
	// Ideally this should all happen at a much higher layer, where the code can differentiate
	// between a value coming from containers.conf vs. the CLI.
	if options.CompressionFormat != nil && options.CompressionFormat.Name() != compressiontypes.GzipAlgorithmName &&
		(destRef.Transport().Name() == dockerArchiveTransport.Transport.Name() ||
			destRef.Transport().Name() == dockerDaemonTransport.Transport.Name() ||
			options.ManifestMIMEType == manifest.DockerV2Schema2MediaType) {
		options.CompressionFormat = nil
	}

	if r.eventChannel != nil {
		defer r.writeEvent(&Event{ID: image.ID(), Name: destination, Time: time.Now(), Type: EventTypeImagePush})
	}

	// Buildah compat: Make sure to tag the destination image if it's a
	// Docker archive. This way, we preserve the image name.
	if destRef.Transport().Name() == dockerArchiveTransport.Transport.Name() {
		if named, err := reference.ParseNamed(resolvedSource); err == nil {
			tagged, isTagged := named.(reference.NamedTagged)
			if isTagged {
				options.dockerArchiveAdditionalTags = []reference.NamedTagged{tagged}
			}
		}
	}

	c, err := r.newCopier(&options.CopyOptions)
	if err != nil {
		return nil, err
	}

	defer c.Close()

	return c.Copy(ctx, srcRef, destRef)
}
