package buildah

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"strings"

	"github.com/containers/buildah/define"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/openshift/imagebuilder"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/shortnames"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/stringid"
)

const (
	// BaseImageFakeName is the "name" of a source image which we interpret
	// as "no image".
	BaseImageFakeName = imagebuilder.NoBaseImageSpecifier
)

func getImageName(name string, img *storage.Image) string {
	imageName := name
	if len(img.Names) > 0 {
		imageName = img.Names[0]
		// When the image used by the container is a tagged image
		// the container name might be set to the original image instead of
		// the image given in the "from" command line.
		// This loop is supposed to fix this.
		for _, n := range img.Names {
			if strings.Contains(n, name) {
				imageName = n
				break
			}
		}
	}
	return imageName
}

func imageNamePrefix(imageName string) string {
	prefix := imageName
	if d, err := digest.Parse(imageName); err == nil {
		prefix = d.Encoded()
		if len(prefix) > 12 {
			prefix = prefix[:12]
		}
	}
	if stringid.ValidateID(prefix) == nil {
		prefix = stringid.TruncateID(prefix)
	}
	s := strings.Split(prefix, ":")
	if len(s) > 0 {
		prefix = s[0]
	}
	s = strings.Split(prefix, "/")
	if len(s) > 0 {
		prefix = s[len(s)-1]
	}
	s = strings.Split(prefix, "@")
	if len(s) > 0 {
		prefix = s[0]
	}
	return prefix
}

func newContainerIDMappingOptions(idmapOptions *define.IDMappingOptions) storage.IDMappingOptions {
	var options storage.IDMappingOptions
	if idmapOptions != nil {
		if idmapOptions.AutoUserNs {
			options.AutoUserNs = true
			options.AutoUserNsOpts = idmapOptions.AutoUserNsOpts
		} else {
			options.HostUIDMapping = idmapOptions.HostUIDMapping
			options.HostGIDMapping = idmapOptions.HostGIDMapping
			uidmap, gidmap := convertRuntimeIDMaps(idmapOptions.UIDMap, idmapOptions.GIDMap)
			if len(uidmap) > 0 && len(gidmap) > 0 {
				options.UIDMap = uidmap
				options.GIDMap = gidmap
			} else {
				options.HostUIDMapping = true
				options.HostGIDMapping = true
			}
		}
	}
	return options
}

func containerNameExist(name string, containers []storage.Container) bool {
	for _, container := range containers {
		if slices.Contains(container.Names, name) {
			return true
		}
	}
	return false
}

func findUnusedContainer(name string, containers []storage.Container) string {
	suffix := 1
	tmpName := name
	for containerNameExist(tmpName, containers) {
		tmpName = fmt.Sprintf("%s-%d", name, suffix)
		suffix++
	}
	return tmpName
}

func newBuilder(ctx context.Context, store storage.Store, options BuilderOptions) (*Builder, error) {
	var (
		ref types.ImageReference
		img *storage.Image
		err error
	)

	if options.FromImage == BaseImageFakeName {
		options.FromImage = ""
	}

	if options.NetworkInterface == nil {
		// create the network interface
		// Note: It is important to do this before we pull any images/create containers.
		// The default backend detection logic needs an empty store to correctly detect
		// that we can use netavark, if the store was not empty it will use CNI to not break existing installs.
		options.NetworkInterface, err = getNetworkInterface(store, options.CNIConfigDir, options.CNIPluginPath)
		if err != nil {
			return nil, err
		}
	}

	systemContext := getSystemContext(store, options.SystemContext, options.SignaturePolicyPath)

	if options.FromImage != "" && options.FromImage != BaseImageFakeName {
		imageRuntime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
		if err != nil {
			return nil, err
		}

		pullPolicy, err := config.ParsePullPolicy(options.PullPolicy.String())
		if err != nil {
			return nil, err
		}

		// Note: options.Format does *not* relate to the image we're
		// about to pull (see tests/digests.bats).  So we're not
		// forcing a MIMEType in the pullOptions below.
		pullOptions := libimage.PullOptions{}
		pullOptions.RetryDelay = &options.PullRetryDelay
		pullOptions.OciDecryptConfig = options.OciDecryptConfig
		pullOptions.SignaturePolicyPath = options.SignaturePolicyPath
		pullOptions.Writer = options.ReportWriter
		pullOptions.DestinationLookupReferenceFunc = cacheLookupReferenceFunc(options.BlobDirectory, types.PreserveOriginal)

		maxRetries := uint(options.MaxPullRetries)
		pullOptions.MaxRetries = &maxRetries

		pulledImages, err := imageRuntime.Pull(ctx, options.FromImage, pullPolicy, &pullOptions)
		if err != nil {
			return nil, err
		}
		if len(pulledImages) > 0 {
			img = pulledImages[0].StorageImage()
			ref, err = pulledImages[0].StorageReference()
			if err != nil {
				return nil, err
			}
		}
	}

	imageSpec := options.FromImage
	imageID := ""
	imageDigest := ""
	topLayer := ""
	if img != nil {
		imageSpec = getImageName(imageNamePrefix(imageSpec), img)
		imageID = img.ID
		topLayer = img.TopLayer
	}
	var src types.Image
	if ref != nil {
		srcSrc, err := ref.NewImageSource(ctx, systemContext)
		if err != nil {
			return nil, fmt.Errorf("instantiating image for %q: %w", transports.ImageName(ref), err)
		}
		defer srcSrc.Close()
		unparsedTop := image.UnparsedInstance(srcSrc, nil)
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
			unparsedInstance = image.UnparsedInstance(srcSrc, instanceDigest)
		}
		src, err = image.FromUnparsedImage(ctx, systemContext, unparsedInstance)
		if err != nil {
			return nil, fmt.Errorf("instantiating image for %q instance %q: %w", transports.ImageName(ref), instanceDigest, err)
		}
	}

	name := "working-container"
	if options.ContainerSuffix != "" {
		name = options.ContainerSuffix
	}
	if options.Container != "" {
		name = options.Container
	} else {
		if imageSpec != "" {
			name = imageNamePrefix(imageSpec) + "-" + name
		}
	}
	var container *storage.Container
	tmpName := name
	if options.Container == "" {
		containers, err := store.Containers()
		if err != nil {
			return nil, fmt.Errorf("unable to check for container names: %w", err)
		}
		tmpName = findUnusedContainer(tmpName, containers)
	}

	suffixDigitsModulo := 100
	for {
		var flags map[string]any
		// check if we have predefined ProcessLabel and MountLabel
		// this could be true if this is another stage in a build
		if options.ProcessLabel != "" && options.MountLabel != "" {
			flags = map[string]any{
				"ProcessLabel": options.ProcessLabel,
				"MountLabel":   options.MountLabel,
			}
		}
		coptions := storage.ContainerOptions{
			LabelOpts:        options.CommonBuildOpts.LabelOpts,
			IDMappingOptions: newContainerIDMappingOptions(options.IDMappingOptions),
			Flags:            flags,
			Volatile:         true,
		}
		container, err = store.CreateContainer("", []string{tmpName}, imageID, "", "", &coptions)
		if err == nil {
			name = tmpName
			break
		}
		if !errors.Is(err, storage.ErrDuplicateName) || options.Container != "" {
			return nil, fmt.Errorf("creating container: %w", err)
		}
		tmpName = fmt.Sprintf("%s-%d", name, rand.Int()%suffixDigitsModulo)
		if suffixDigitsModulo < 1_000_000_000 {
			suffixDigitsModulo *= 10
		}
	}
	defer func() {
		if err != nil {
			if err2 := store.DeleteContainer(container.ID); err2 != nil {
				logrus.Errorf("error deleting container %q: %v", container.ID, err2)
			}
		}
	}()

	uidmap, gidmap := convertStorageIDMaps(container.UIDMap, container.GIDMap)

	defaultNamespaceOptions, err := DefaultNamespaceOptions()
	if err != nil {
		return nil, err
	}

	namespaceOptions := defaultNamespaceOptions
	namespaceOptions.AddOrReplace(options.NamespaceOptions...)

	builder := &Builder{
		store:                 store,
		Type:                  containerType,
		FromImage:             imageSpec,
		FromImageID:           imageID,
		FromImageDigest:       imageDigest,
		GroupAdd:              options.GroupAdd,
		Container:             name,
		ContainerID:           container.ID,
		ImageAnnotations:      map[string]string{},
		ImageCreatedBy:        "",
		ProcessLabel:          container.ProcessLabel(),
		MountLabel:            container.MountLabel(),
		DefaultMountsFilePath: options.DefaultMountsFilePath,
		Isolation:             options.Isolation,
		NamespaceOptions:      namespaceOptions,
		ConfigureNetwork:      options.ConfigureNetwork,
		CNIPluginPath:         options.CNIPluginPath,
		CNIConfigDir:          options.CNIConfigDir,
		IDMappingOptions: define.IDMappingOptions{
			HostUIDMapping: len(uidmap) == 0,
			HostGIDMapping: len(uidmap) == 0,
			UIDMap:         uidmap,
			GIDMap:         gidmap,
		},
		Capabilities:     slices.Clone(options.Capabilities),
		CommonBuildOpts:  options.CommonBuildOpts,
		TopLayer:         topLayer,
		Args:             maps.Clone(options.Args),
		Format:           options.Format,
		Devices:          options.Devices,
		DeviceSpecs:      options.DeviceSpecs,
		Logger:           options.Logger,
		NetworkInterface: options.NetworkInterface,
		CDIConfigDir:     options.CDIConfigDir,
	}

	if options.Mount {
		_, err = builder.Mount(container.MountLabel())
		if err != nil {
			return nil, fmt.Errorf("mounting build container %q: %w", builder.ContainerID, err)
		}
	}

	if err := builder.initConfig(ctx, systemContext, src, &options); err != nil {
		return nil, fmt.Errorf("preparing image configuration: %w", err)
	}

	if !options.PreserveBaseImageAnns {
		builder.SetAnnotation(v1.AnnotationBaseImageDigest, imageDigest)
		if !shortnames.IsShortName(imageSpec) {
			// If the base image was specified as a fully-qualified
			// image name, let's set it.
			builder.SetAnnotation(v1.AnnotationBaseImageName, imageSpec)
		} else {
			builder.UnsetAnnotation(v1.AnnotationBaseImageName)
		}
	}

	err = builder.Save()
	if err != nil {
		return nil, fmt.Errorf("saving builder state for container %q: %w", builder.ContainerID, err)
	}

	return builder, nil
}
