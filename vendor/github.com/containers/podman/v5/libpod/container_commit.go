//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/containers/buildah"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
)

// ContainerCommitOptions is a struct used to commit a container to an image
// It uses buildah's CommitOptions as a base. Long-term we might wish to
// decouple these because it includes duplicates of fields that are in, or
// could later be added, to buildah's CommitOptions, which gets confusing
type ContainerCommitOptions struct {
	buildah.CommitOptions
	Pause          bool
	IncludeVolumes bool
	Author         string
	Message        string
	Changes        []string // gets merged with CommitOptions.OverrideChanges
	Squash         bool     // always used instead of CommitOptions.Squash
}

// Commit commits the changes between a container and its image, creating a new
// image
func (c *Container) Commit(ctx context.Context, destImage string, options ContainerCommitOptions) (*libimage.Image, error) {
	if c.config.Rootfs != "" {
		return nil, errors.New("cannot commit a container that uses an exploded rootfs")
	}

	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	if c.state.State == define.ContainerStateRunning && options.Pause {
		if err := c.pause(); err != nil {
			return nil, fmt.Errorf("pausing container %q to commit: %w", c.ID(), err)
		}
		defer func() {
			if err := c.unpause(); err != nil {
				logrus.Errorf("Unpausing container %q: %v", c.ID(), err)
			}
		}()
	}

	builderOptions := buildah.ImportOptions{
		Container:           c.ID(),
		SignaturePolicyPath: options.SignaturePolicyPath,
	}
	commitOptions := buildah.CommitOptions{
		SignaturePolicyPath:   options.SignaturePolicyPath,
		ReportWriter:          options.ReportWriter,
		Squash:                options.Squash,
		SystemContext:         c.runtime.imageContext,
		PreferredManifestType: options.PreferredManifestType,
		OverrideChanges:       append(append([]string{}, options.Changes...), options.CommitOptions.OverrideChanges...),
		OverrideConfig:        options.CommitOptions.OverrideConfig,
	}
	importBuilder, err := buildah.ImportBuilder(ctx, c.runtime.store, builderOptions)
	if err != nil {
		return nil, err
	}
	importBuilder.Format = options.PreferredManifestType
	if options.Author != "" {
		importBuilder.SetMaintainer(options.Author)
	}
	if options.Message != "" {
		importBuilder.SetComment(options.Message)
	}

	// We need to take meta we find in the current container and
	// add it to the resulting image.

	// Entrypoint - always set this first or cmd will get wiped out
	importBuilder.SetEntrypoint(c.config.Entrypoint)

	// Cmd
	importBuilder.SetCmd(c.config.Command)

	// Env
	// TODO - this includes all the default environment vars as well
	// Should we store the ENV we actually want in the spec separately?
	if c.config.Spec.Process != nil {
		for _, e := range c.config.Spec.Process.Env {
			key, val, _ := strings.Cut(e, "=")
			importBuilder.SetEnv(key, val)
		}
	}
	// Expose ports
	for _, p := range c.config.PortMappings {
		importBuilder.SetPort(fmt.Sprintf("%d/%s", p.ContainerPort, p.Protocol))
	}
	for port, protocols := range c.config.ExposedPorts {
		for _, protocol := range protocols {
			importBuilder.SetPort(fmt.Sprintf("%d/%s", port, protocol))
		}
	}
	// Labels
	for k, v := range c.Labels() {
		importBuilder.SetLabel(k, v)
	}
	// No stop signal
	// User
	if c.config.User != "" {
		importBuilder.SetUser(c.config.User)
	}
	// Volumes
	if options.IncludeVolumes {
		for _, v := range c.config.UserVolumes {
			if v != "" {
				importBuilder.AddVolume(v)
			}
		}
	} else {
		// Only include anonymous named volumes added by the user by
		// default.
		for _, v := range c.config.NamedVolumes {
			if slices.Contains(c.config.UserVolumes, v.Dest) {
				vol, err := c.runtime.GetVolume(v.Name)
				if err != nil {
					return nil, fmt.Errorf("volume %s used in container %s has been removed: %w", v.Name, c.ID(), err)
				}
				if vol.Anonymous() {
					importBuilder.AddVolume(v.Dest)
				}
			}
		}
	}
	// Workdir
	importBuilder.SetWorkDir(c.config.Spec.Process.Cwd)

	var commitRef types.ImageReference
	if destImage != "" {
		// Now resolve the name.
		resolvedImageName, err := c.runtime.LibimageRuntime().ResolveName(destImage)
		if err != nil {
			return nil, err
		}

		imageRef, err := is.Transport.ParseStoreReference(c.runtime.store, resolvedImageName)
		if err != nil {
			return nil, fmt.Errorf("parsing target image name %q: %w", destImage, err)
		}
		commitRef = imageRef
	}
	id, _, _, err := importBuilder.Commit(ctx, commitRef, commitOptions)
	if err != nil {
		return nil, err
	}
	defer c.newContainerEvent(events.Commit)
	img, _, err := c.runtime.libimageRuntime.LookupImage(id, nil)
	if err != nil {
		return nil, err
	}
	return img, nil
}
