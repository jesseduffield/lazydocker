//go:build !remote

package libimage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	ociSpec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
	registryTransport "go.podman.io/image/v5/docker"
	dockerArchiveTransport "go.podman.io/image/v5/docker/archive"
	dockerDaemonTransport "go.podman.io/image/v5/docker/daemon"
	"go.podman.io/image/v5/docker/reference"
	ociArchiveTransport "go.podman.io/image/v5/oci/archive"
	ociTransport "go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/pkg/shortnames"
	storageTransport "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

// PullOptions allows for customizing image pulls.
type PullOptions struct {
	CopyOptions

	// If true, all tags of the image will be pulled from the container
	// registry.  Only supported for the docker transport.
	AllTags bool
}

// Pull pulls the specified name.  Name may refer to any of the supported
// transports from github.com/containers/image.  If no transport is encoded,
// name will be treated as a reference to a registry (i.e., docker transport).
//
// Note that pullPolicy is only used when pulling from a container registry but
// it *must* be different than the default value `config.PullPolicyUnsupported`.  This
// way, callers are forced to decide on the pull behaviour.  The reasoning
// behind is that some (commands of some) tools have different default pull
// policies (e.g., buildah-bud versus podman-build).  Making the pull-policy
// choice explicit is an attempt to prevent silent regressions.
//
// The error is storage.ErrImageUnknown iff the pull policy is set to "never"
// and no local image has been found.  This allows for an easier integration
// into some users of this package (e.g., Buildah).
//
// Pull returns a slice of the pulled images.
//
// WARNING: the Digest field of the returned image might not be a value relevant to the user issuing the pull.
func (r *Runtime) Pull(ctx context.Context, name string, pullPolicy config.PullPolicy, options *PullOptions) (_ []*Image, pullError error) {
	logrus.Debugf("Pulling image %s (policy: %s)", name, pullPolicy)
	if r.eventChannel != nil {
		defer func() {
			if pullError != nil {
				// Note that we use the input name here to preserve the transport data.
				r.writeEvent(&Event{Name: name, Time: time.Now(), Type: EventTypeImagePullError, Error: pullError})
			}
		}()
	}
	if options == nil {
		options = &PullOptions{}
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

	var possiblyUnqualifiedName string // used for short-name resolution
	ref, err := alltransports.ParseImageName(name)
	if err != nil {
		// Check whether `name` points to a transport.  If so, we
		// return the error.  Otherwise we assume that `name` refers to
		// an image on a registry (e.g., "fedora").
		//
		// NOTE: the `docker` transport is an exception to support a
		// `pull docker:latest` which would otherwise return an error.
		if t := alltransports.TransportFromImageName(name); t != nil && t.Name() != registryTransport.Transport.Name() {
			return nil, err
		}

		// If the image clearly refers to a local one, we can look it up directly.
		// In fact, we need to since they are not parseable.
		if strings.HasPrefix(name, "sha256:") || (len(name) == 64 && !strings.ContainsAny(name, "/.:@")) {
			if pullPolicy == config.PullPolicyAlways {
				return nil, fmt.Errorf("pull policy is always but image has been referred to by ID (%s)", name)
			}
			local, _, err := r.LookupImage(name, nil)
			if err != nil {
				return nil, err
			}
			return []*Image{local}, err
		}

		// Docker compat: strip off the tag iff name is tagged and digested
		// (e.g., fedora:latest@sha256...).  In that case, the tag is stripped
		// off and entirely ignored.  The digest is the sole source of truth.
		normalizedName, _, normalizeError := normalizeTaggedDigestedString(name)
		if normalizeError != nil {
			return nil, fmt.Errorf(`parsing reference %q: %w`, name, normalizeError)
		}
		name = normalizedName

		// If the input does not include a transport assume it refers
		// to a registry.
		dockerRef, dockerErr := alltransports.ParseImageName("docker://" + name)
		if dockerErr != nil {
			return nil, err
		}
		ref = dockerRef
		possiblyUnqualifiedName = name
	} else if ref.Transport().Name() == registryTransport.Transport.Name() {
		// Normalize the input if we're referring to the docker
		// transport directly. That makes sure that a `docker://fedora`
		// will resolve directly to `docker.io/library/fedora:latest`
		// and not be subject to short-name resolution.
		named := ref.DockerReference()
		if named == nil {
			return nil, errors.New("internal error: unexpected nil reference")
		}
		possiblyUnqualifiedName = named.String()
	}

	if options.AllTags && ref.Transport().Name() != registryTransport.Transport.Name() {
		return nil, fmt.Errorf("pulling all tags is not supported for %s transport", ref.Transport().Name())
	}

	// Some callers may set the platform via the system context at creation
	// time of the runtime.  We need this information to decide whether we
	// need to enforce pulling from a registry (see
	// containers/podman/issues/10682).
	if options.Architecture == "" {
		options.Architecture = r.systemContext.ArchitectureChoice
	}
	if options.OS == "" {
		options.OS = r.systemContext.OSChoice
	}
	if options.Variant == "" {
		options.Variant = r.systemContext.VariantChoice
	}

	var pulledImages []*Image

	// Dispatch the copy operation.
	switch ref.Transport().Name() {
	// DOCKER REGISTRY
	case registryTransport.Transport.Name():
		pulledImages, err = r.copyFromRegistry(ctx, ref, possiblyUnqualifiedName, pullPolicy, options)

	// DOCKER ARCHIVE
	case dockerArchiveTransport.Transport.Name():
		pulledImages, _, err = r.copyFromDockerArchive(ctx, ref, &options.CopyOptions)

	// ALL OTHER TRANSPORTS
	default:
		pulledImages, _, err = r.copyFromDefault(ctx, ref, &options.CopyOptions)
	}

	if err != nil {
		return nil, err
	}

	for _, image := range pulledImages {
		// Note that we can ignore the 2nd return value here. Some
		// images may ship with "wrong" platform, but we already warn
		// about it. Throwing an error is not (yet) the plan.
		matchError, _, err := image.matchesPlatform(ctx, options.OS, options.Architecture, options.Variant)
		if err != nil {
			return nil, fmt.Errorf("checking platform of image %s: %w", name, err)
		}

		// If the image does not match the expected/requested platform,
		// make sure to leave some breadcrumbs for the user.
		if matchError != nil {
			if options.Writer == nil {
				logrus.Warnf("%v", matchError)
			} else {
				fmt.Fprintf(options.Writer, "WARNING: %v\n", matchError)
			}
		}

		if r.eventChannel != nil {
			// Note that we use the input name here to preserve the transport data.
			r.writeEvent(&Event{ID: image.ID(), Name: name, Time: time.Now(), Type: EventTypeImagePull})
		}
	}

	return pulledImages, pullError
}

// nameFromAnnotations returns a reference string to be used as an image name,
// or an empty string.  The annotations map may be nil.
func nameFromAnnotations(annotations map[string]string) string {
	if annotations == nil {
		return ""
	}
	// buildkit/containerd are using a custom annotation see
	// containers/podman/issues/12560.
	if annotations["io.containerd.image.name"] != "" {
		return annotations["io.containerd.image.name"]
	}
	return annotations[ociSpec.AnnotationRefName]
}

// copyFromDefault is the default copier for a number of transports.  Other
// transports require some specific dancing, sometimes Yoga.
func (r *Runtime) copyFromDefault(ctx context.Context, ref types.ImageReference, options *CopyOptions) ([]*Image, []string, error) {
	c, err := r.newCopier(options)
	if err != nil {
		return nil, nil, err
	}
	defer c.Close()

	// Figure out a name for the storage destination.
	var storageName, imageName string
	switch ref.Transport().Name() {
	case registryTransport.Transport.Name():
		// Normalize to docker.io if needed (see containers/podman/issues/10998).
		named, err := reference.ParseNormalizedNamed(strings.TrimLeft(ref.StringWithinTransport(), ":/"))
		if err != nil {
			return nil, nil, err
		}
		imageName = named.String()
		storageName = imageName

	case dockerDaemonTransport.Transport.Name():
		// Normalize to docker.io if needed (see containers/podman/issues/10998).
		named, err := reference.ParseNormalizedNamed(ref.StringWithinTransport())
		if err != nil {
			return nil, nil, err
		}
		imageName = named.String()
		storageName = imageName

	case ociTransport.Transport.Name():
		_, refName, ok := strings.Cut(ref.StringWithinTransport(), ":")
		if !ok || refName == "" {
			// Same trick as for the dir transport: we cannot use
			// the path to a directory as the name.
			storageName, err = getImageID(ctx, ref, nil)
			if err != nil {
				return nil, nil, err
			}
			imageName = "sha256:" + storageName[1:]
		} else { // If the OCI-reference includes an image reference, use it
			storageName = refName
			imageName = storageName
		}

	case ociArchiveTransport.Transport.Name():
		manifestDescriptor, err := ociArchiveTransport.LoadManifestDescriptorWithContext(r.SystemContext(), ref)
		if err != nil {
			return nil, nil, err
		}
		storageName = nameFromAnnotations(manifestDescriptor.Annotations)
		switch len(storageName) {
		case 0:
			// If there's no reference name in the annotations, compute an ID.
			storageName, err = getImageID(ctx, ref, nil)
			if err != nil {
				return nil, nil, err
			}
			imageName = "sha256:" + storageName[1:]
		default:
			named, err := NormalizeName(storageName)
			if err != nil {
				return nil, nil, err
			}
			imageName = named.String()
			storageName = imageName
		}

	case storageTransport.Transport.Name():
		storageName = ref.StringWithinTransport()
		named := ref.DockerReference()
		if named == nil {
			return nil, nil, fmt.Errorf("could not get an image name for storage reference %q", ref)
		}
		imageName = named.String()

	default:
		// Path-based transports (e.g., dir) may include invalid
		// characters, so we should pessimistically generate an ID
		// instead of looking at the StringWithinTransport().
		storageName, err = getImageID(ctx, ref, nil)
		if err != nil {
			return nil, nil, err
		}
		imageName = "sha256:" + storageName[1:]
	}

	// Create a storage reference.
	destRef, err := storageTransport.Transport.ParseStoreReference(r.store, storageName)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %q: %w", storageName, err)
	}
	image, err := c.copyToStorage(ctx, ref, destRef)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to perform copy: %w", err)
	}
	resolvedImage := r.storageToImage(image, nil)
	return []*Image{resolvedImage}, []string{imageName}, err
}

// storageReferencesFromArchiveReader returns a slice of image references inside the
// archive reader.  A docker archive may include more than one image and this
// method allows for extracting them into containers storage references which
// can later be used from copying.
func (r *Runtime) storageReferencesReferencesFromArchiveReader(ctx context.Context, readerRef types.ImageReference, reader *dockerArchiveTransport.Reader) ([]types.ImageReference, []string, error) {
	destNames, err := reader.ManifestTagsForReference(readerRef)
	if err != nil {
		return nil, nil, err
	}

	var imageNames []string
	if len(destNames) == 0 {
		destName, err := getImageID(ctx, readerRef, &r.systemContext)
		if err != nil {
			return nil, nil, err
		}
		destNames = append(destNames, destName)
		// Make sure the image can be loaded after the pull by
		// replacing the @ with sha256:.
		imageNames = append(imageNames, "sha256:"+destName[1:])
	} else {
		for i := range destNames {
			ref, err := NormalizeName(destNames[i])
			if err != nil {
				return nil, nil, err
			}
			destNames[i] = ref.String()
		}
		imageNames = destNames
	}

	references := []types.ImageReference{}
	for _, destName := range destNames {
		destRef, err := storageTransport.Transport.ParseStoreReference(r.store, destName)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing dest reference name %#v: %w", destName, err)
		}
		references = append(references, destRef)
	}

	return references, imageNames, nil
}

// copyFromDockerArchive copies one image from the specified reference.
func (r *Runtime) copyFromDockerArchive(ctx context.Context, ref types.ImageReference, options *CopyOptions) ([]*Image, []string, error) {
	// There may be more than one image inside the docker archive, so we
	// need a quick glimpse inside.
	reader, readerRef, err := dockerArchiveTransport.NewReaderForReference(&r.systemContext, ref)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			logrus.Errorf("Closing reader of docker archive: %v", err)
		}
	}()

	return r.copyFromDockerArchiveReaderReference(ctx, reader, readerRef, options)
}

// copyFromDockerArchiveReaderReference copies the specified readerRef from reader.
func (r *Runtime) copyFromDockerArchiveReaderReference(ctx context.Context, reader *dockerArchiveTransport.Reader, readerRef types.ImageReference, options *CopyOptions) ([]*Image, []string, error) {
	c, err := r.newCopier(options)
	if err != nil {
		return nil, nil, err
	}
	defer c.Close()

	// Get a slice of storage references we can copy.
	references, destNames, err := r.storageReferencesReferencesFromArchiveReader(ctx, readerRef, reader)
	if err != nil {
		return nil, nil, err
	}

	images := []*Image{}
	// Now copy all of the images.  Use readerRef for performance.
	for _, destRef := range references {
		image, err := c.copyToStorage(ctx, readerRef, destRef)
		if err != nil {
			return nil, nil, err
		}
		resolvedImage := r.storageToImage(image, nil)
		images = append(images, resolvedImage)
	}

	return images, destNames, nil
}

// copyFromRegistry pulls the specified, possibly unqualified, name from a
// registry.  On successful pull it returns slice of the pulled images.
//
// If options.All is set, all tags from the specified registry will be pulled.
func (r *Runtime) copyFromRegistry(ctx context.Context, ref types.ImageReference, inputName string, pullPolicy config.PullPolicy, options *PullOptions) ([]*Image, error) {
	// Sanity check.
	if err := pullPolicy.Validate(); err != nil {
		return nil, err
	}

	if !options.AllTags {
		pulled, err := r.copySingleImageFromRegistry(ctx, inputName, pullPolicy, options)
		if err != nil {
			return nil, err
		}
		return []*Image{pulled}, nil
	}

	// Copy all tags
	named := reference.TrimNamed(ref.DockerReference())
	tags, err := registryTransport.GetRepositoryTags(ctx, &r.systemContext, ref)
	if err != nil {
		return nil, err
	}

	pulledImages := []*Image{}
	for _, tag := range tags {
		select { // Let's be gentle with Podman remote.
		case <-ctx.Done():
			return nil, errors.New("pulling cancelled")
		default:
			// We can continue.
		}
		tagged, err := reference.WithTag(named, tag)
		if err != nil {
			return nil, fmt.Errorf("creating tagged reference (name %s, tag %s): %w", named.String(), tag, err)
		}
		pulled, err := r.copySingleImageFromRegistry(ctx, tagged.String(), pullPolicy, options)
		if err != nil {
			return nil, err
		}
		pulledImages = append(pulledImages, pulled)
	}

	return pulledImages, nil
}

// copySingleImageFromRegistry pulls the specified, possibly unqualified, name
// from a registry.  On successful pull it returns the Image from the local storage.
func (r *Runtime) copySingleImageFromRegistry(ctx context.Context, imageName string, pullPolicy config.PullPolicy, options *PullOptions) (*Image, error) { //nolint:gocyclo
	// Sanity check.
	if err := pullPolicy.Validate(); err != nil {
		return nil, err
	}

	var (
		localImage        *Image
		resolvedImageName string
		err               error
	)

	// Always check if there's a local image.  If so, we should use its
	// resolved name for pulling.  Assume we're doing a `pull foo`.
	// If there's already a local image "localhost/foo", then we should
	// attempt pulling that instead of doing the full short-name dance.
	//
	// NOTE that we only do platform checks if the specified values differ
	// from the local platform. Unfortunately, there are many images used
	// in the wild which don't set the correct value(s) in the config
	// causing various issues such as containers/podman/issues/10682.
	lookupImageOptions := &LookupImageOptions{Variant: options.Variant}
	if options.Architecture != runtime.GOARCH {
		lookupImageOptions.Architecture = options.Architecture
	}
	if options.OS != runtime.GOOS {
		lookupImageOptions.OS = options.OS
	}

	localImage, resolvedImageName, err = r.LookupImage(imageName, lookupImageOptions)
	if err != nil && !errors.Is(err, storage.ErrImageUnknown) {
		logrus.Errorf("Looking up %s in local storage: %v", imageName, err)
	}

	// If the local image is corrupted, we need to repull it.
	if localImage != nil {
		if err := localImage.isCorrupted(ctx, imageName); err != nil {
			logrus.Error(err)
			localImage = nil
		}
	}

	customPlatform := len(options.Architecture)+len(options.OS)+len(options.Variant) > 0
	if customPlatform && pullPolicy != config.PullPolicyAlways && pullPolicy != config.PullPolicyNever {
		// Unless the pull policy is always/never, we must
		// pessimistically assume that the local image has an invalid
		// architecture (see containers/podman/issues/10682).  Hence,
		// whenever the user requests a custom platform, set the pull
		// policy to "newer" to make sure we're pulling down the
		// correct image.
		//
		// NOTE that this is will even override --pull={false,never}.
		pullPolicy = config.PullPolicyNewer
		logrus.Debugf("Enforcing pull policy to %q to pull custom platform (arch: %q, os: %q, variant: %q) - local image may mistakenly specify wrong platform", pullPolicy, options.Architecture, options.OS, options.Variant)
	}

	if pullPolicy == config.PullPolicyNever {
		if localImage != nil {
			logrus.Debugf("Pull policy %q and %s resolved to local image %s", pullPolicy, imageName, resolvedImageName)
			return localImage, nil
		}
		logrus.Debugf("Pull policy %q but no local image has been found for %s", pullPolicy, imageName)
		return nil, fmt.Errorf("%s: %w", imageName, storage.ErrImageUnknown)
	}

	if pullPolicy == config.PullPolicyMissing && localImage != nil {
		return localImage, nil
	}

	// If we looked up the image by ID, we cannot really pull from anywhere.
	if localImage != nil && strings.HasPrefix(localImage.ID(), imageName) {
		switch pullPolicy {
		case config.PullPolicyAlways:
			return nil, fmt.Errorf("pull policy is always but image has been referred to by ID (%s)", imageName)
		default:
			return localImage, nil
		}
	}

	// If we found a local image, we should use its locally resolved name
	// (see containers/buildah/issues/2904).  An exception is if a custom
	// platform is specified (e.g., `--arch=arm64`).  In that case, we need
	// to pessimistically pull the image since some images declare wrong
	// platforms making platform checks absolutely unreliable (see
	// containers/podman/issues/10682).
	//
	// In other words: multi-arch support can only be as good as the images
	// in the wild, so we shouldn't break things for our users by trying to
	// insist that they make sense.
	if localImage != nil && !customPlatform {
		if imageName != resolvedImageName {
			logrus.Debugf("Image %s resolved to local image %s which will be used for pulling", imageName, resolvedImageName)
		}
		imageName = resolvedImageName
	}

	sys := r.systemContextCopy()
	resolved, err := shortnames.Resolve(sys, imageName)
	if err != nil {
		if localImage != nil && pullPolicy == config.PullPolicyNewer {
			return localImage, nil
		}
		return nil, err
	}

	// NOTE: Below we print the description from the short-name resolution.
	// In theory we could print it here.  In practice, however, this is
	// causing a hard time for Buildah uses who are doing a `buildah from
	// image` and expect just the container name to be printed if the image
	// is present locally.
	// The pragmatic solution is to only print the description when we found
	// a _newer_ image that we're about to pull.
	wroteDesc := false
	writeDesc := func() error {
		if wroteDesc {
			return nil
		}
		wroteDesc = true
		if desc := resolved.Description(); len(desc) > 0 {
			logrus.Debug(desc)
			if options.Writer != nil {
				if _, err := options.Writer.Write([]byte(desc + "\n")); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if socketPath, ok := os.LookupEnv("NOTIFY_SOCKET"); ok {
		options.extendTimeoutSocket = socketPath
	}
	c, err := r.newCopier(&options.CopyOptions)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	var pullErrors []error
	for _, candidate := range resolved.PullCandidates {
		candidateString := candidate.Value.String()
		logrus.Debugf("Attempting to pull candidate %s for %s", candidateString, imageName)
		srcRef, err := registryTransport.NewReference(candidate.Value)
		if err != nil {
			return nil, err
		}

		if pullPolicy == config.PullPolicyNewer && localImage != nil {
			isNewer, err := localImage.hasDifferentDigestWithSystemContext(ctx, srcRef, c.systemContext)
			if err != nil {
				pullErrors = append(pullErrors, err)
				continue
			}

			if !isNewer {
				logrus.Debugf("Skipping pull candidate %s as the image is not newer (pull policy %s)", candidateString, pullPolicy)
				continue
			}
		}

		destRef, err := storageTransport.Transport.ParseStoreReference(r.store, candidate.Value.String())
		if err != nil {
			return nil, err
		}

		if err := writeDesc(); err != nil {
			return nil, err
		}
		if options.Writer != nil {
			if _, err := io.WriteString(options.Writer, fmt.Sprintf("Trying to pull %s...\n", candidateString)); err != nil {
				return nil, err
			}
		}
		image, err := c.copyToStorage(ctx, srcRef, destRef)
		if err != nil {
			logrus.Debugf("Error pulling candidate %s: %v", candidateString, err)
			pullErrors = append(pullErrors, err)
			continue
		}
		if err := candidate.Record(); err != nil {
			// Only log the recording errors.  Podman has seen
			// reports where users set most of the system to
			// read-only which can cause issues.
			logrus.Errorf("Error recording short-name alias %q: %v", candidateString, err)
		}
		logrus.Debugf("Pulled candidate %s successfully", candidateString)
		resolvedImage := r.storageToImage(image, nil)
		return resolvedImage, err
	}

	if localImage != nil && pullPolicy == config.PullPolicyNewer {
		return localImage, nil
	}

	if len(pullErrors) == 0 {
		return nil, fmt.Errorf("internal error: no image pulled (pull policy %s)", pullPolicy)
	}

	return nil, resolved.FormatPullErrors(pullErrors)
}
