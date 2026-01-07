package buildah

import (
	"context"
	"errors"
	"fmt"

	"github.com/containers/buildah/define"
	"github.com/containers/buildah/docker"
	"github.com/containers/buildah/util"
	digest "github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

func importBuilderDataFromImage(ctx context.Context, store storage.Store, systemContext *types.SystemContext, imageID, containerName, containerID string) (*Builder, error) {
	if imageID == "" {
		return nil, errors.New("internal error: imageID is empty in importBuilderDataFromImage")
	}

	storeopts, err := storage.DefaultStoreOptions()
	if err != nil {
		return nil, err
	}
	uidmap, gidmap := convertStorageIDMaps(storeopts.UIDMap, storeopts.GIDMap)

	ref, err := is.Transport.ParseStoreReference(store, imageID)
	if err != nil {
		return nil, fmt.Errorf("no such image %q: %w", imageID, err)
	}
	src, err := ref.NewImageSource(ctx, systemContext)
	if err != nil {
		return nil, fmt.Errorf("instantiating image source: %w", err)
	}
	defer src.Close()

	imageDigest := ""
	unparsedTop := image.UnparsedInstance(src, nil)
	manifestBytes, manifestType, err := unparsedTop.Manifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading image manifest for %q: %w", transports.ImageName(ref), err)
	}
	if manifestDigest, err := manifest.Digest(manifestBytes); err == nil {
		imageDigest = manifestDigest.String()
	}

	var instanceDigest *digest.Digest
	unparsedInstance := unparsedTop // for instanceDigest
	if manifest.MIMETypeIsMultiImage(manifestType) {
		list, err := manifest.ListFromBlob(manifestBytes, manifestType)
		if err != nil {
			return nil, fmt.Errorf("parsing image manifest for %q as list: %w", transports.ImageName(ref), err)
		}
		instance, err := list.ChooseInstance(systemContext)
		if err != nil {
			return nil, fmt.Errorf("finding an appropriate image in manifest list %q: %w", transports.ImageName(ref), err)
		}
		instanceDigest = &instance
		unparsedInstance = image.UnparsedInstance(src, instanceDigest)
	}

	image, err := image.FromUnparsedImage(ctx, systemContext, unparsedInstance)
	if err != nil {
		return nil, fmt.Errorf("instantiating image for %q instance %q: %w", transports.ImageName(ref), instanceDigest, err)
	}

	imageName := ""
	if img, err3 := store.Image(imageID); err3 == nil {
		if len(img.Names) > 0 {
			imageName = img.Names[0]
		}
		if img.TopLayer != "" {
			layer, err4 := store.Layer(img.TopLayer)
			if err4 != nil {
				return nil, fmt.Errorf("reading information about image's top layer: %w", err4)
			}
			uidmap, gidmap = convertStorageIDMaps(layer.UIDMap, layer.GIDMap)
		}
	}

	defaultNamespaceOptions, err := DefaultNamespaceOptions()
	if err != nil {
		return nil, err
	}

	netInt, err := getNetworkInterface(store, "", "")
	if err != nil {
		return nil, err
	}

	builder := &Builder{
		store:            store,
		Type:             containerType,
		FromImage:        imageName,
		FromImageID:      imageID,
		FromImageDigest:  imageDigest,
		Container:        containerName,
		ContainerID:      containerID,
		ImageCreatedBy:   "",
		NamespaceOptions: defaultNamespaceOptions,
		IDMappingOptions: define.IDMappingOptions{
			HostUIDMapping: len(uidmap) == 0,
			HostGIDMapping: len(uidmap) == 0,
			UIDMap:         uidmap,
			GIDMap:         gidmap,
		},
		NetworkInterface: netInt,
		CommonBuildOpts:  &CommonBuildOptions{},
	}

	if err := builder.initConfig(ctx, systemContext, image, nil); err != nil {
		return nil, fmt.Errorf("preparing image configuration: %w", err)
	}

	return builder, nil
}

func importBuilder(ctx context.Context, store storage.Store, options ImportOptions) (*Builder, error) {
	if options.Container == "" {
		return nil, errors.New("container name must be specified")
	}

	c, err := store.Container(options.Container)
	if err != nil {
		return nil, err
	}

	systemContext := getSystemContext(store, &types.SystemContext{}, options.SignaturePolicyPath)

	builder, err := importBuilderDataFromImage(ctx, store, systemContext, c.ImageID, options.Container, c.ID)
	if err != nil {
		return nil, err
	}

	if builder.FromImageID != "" {
		if d, err2 := digest.Parse(builder.FromImageID); err2 == nil {
			builder.Docker.Parent = docker.ID(d)
		} else {
			builder.Docker.Parent = docker.ID(digest.NewDigestFromHex(digest.Canonical.String(), builder.FromImageID))
		}
	}
	if builder.FromImage != "" {
		builder.Docker.ContainerConfig.Image = builder.FromImage
	}
	builder.IDMappingOptions.UIDMap, builder.IDMappingOptions.GIDMap = convertStorageIDMaps(c.UIDMap, c.GIDMap)

	err = builder.Save()
	if err != nil {
		return nil, fmt.Errorf("saving builder state: %w", err)
	}

	return builder, nil
}

func importBuilderFromImage(ctx context.Context, store storage.Store, options ImportFromImageOptions) (*Builder, error) {
	if options.Image == "" {
		return nil, errors.New("image name must be specified")
	}

	systemContext := getSystemContext(store, options.SystemContext, options.SignaturePolicyPath)

	_, img, err := util.FindImage(store, "", systemContext, options.Image)
	if err != nil {
		return nil, fmt.Errorf("importing settings: %w", err)
	}

	builder, err := importBuilderDataFromImage(ctx, store, systemContext, img.ID, "", "")
	if err != nil {
		return nil, fmt.Errorf("importing build settings from image %q: %w", options.Image, err)
	}

	builder.setupLogger()
	return builder, nil
}
