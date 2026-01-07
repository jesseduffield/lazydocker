package openshift

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/internal/imagesource/impl"
	"go.podman.io/image/v5/internal/imagesource/stubs"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/types"
)

type openshiftImageSource struct {
	impl.Compat
	impl.DoesNotAffectLayerInfosForCopy
	// This is slightly suboptimal. We could forward GetBlobAt(), but we need to call ensureImageIsResolved in SupportsGetBlobAt(),
	// and that method doesn’t provide a context for timing out. That could actually be fixed (SupportsGetBlobAt is private and we
	// can change it), but this is a deprecated transport anyway, so for now we just punt.
	stubs.NoGetBlobAtInitialize

	client *openshiftClient
	// Values specific to this image
	sys *types.SystemContext
	// State
	docker               types.ImageSource // The docker/distribution API endpoint, or nil if not resolved yet
	imageStreamImageName string            // Resolved image identifier, or "" if not known yet
}

// newImageSource creates a new ImageSource for the specified reference.
// The caller must call .Close() on the returned ImageSource.
func newImageSource(sys *types.SystemContext, ref openshiftReference) (private.ImageSource, error) {
	client, err := newOpenshiftClient(ref)
	if err != nil {
		return nil, err
	}

	s := &openshiftImageSource{
		NoGetBlobAtInitialize: stubs.NoGetBlobAt(ref),

		client: client,
		sys:    sys,
	}
	s.Compat = impl.AddCompat(s)
	return s, nil
}

// Reference returns the reference used to set up this source, _as specified by the user_
// (not as the image itself, or its underlying storage, claims).  This can be used e.g. to determine which public keys are trusted for this image.
func (s *openshiftImageSource) Reference() types.ImageReference {
	return s.client.ref
}

// Close removes resources associated with an initialized ImageSource, if any.
func (s *openshiftImageSource) Close() error {
	var err error
	if s.docker != nil {
		err = s.docker.Close()
		s.docker = nil
	}

	s.client.close()

	return err
}

// GetManifest returns the image's manifest along with its MIME type (which may be empty when it can't be determined but the manifest is available).
// It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve (when the primary manifest is a manifest list);
// this never happens if the primary manifest is not a manifest list (e.g. if the source never returns manifest lists).
func (s *openshiftImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	if err := s.ensureImageIsResolved(ctx); err != nil {
		return nil, "", err
	}
	return s.docker.GetManifest(ctx, instanceDigest)
}

// HasThreadSafeGetBlob indicates whether GetBlob can be executed concurrently.
func (s *openshiftImageSource) HasThreadSafeGetBlob() bool {
	return false
}

// GetBlob returns a stream for the specified blob, and the blob’s size (or -1 if unknown).
// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
func (s *openshiftImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	if err := s.ensureImageIsResolved(ctx); err != nil {
		return nil, 0, err
	}
	return s.docker.GetBlob(ctx, info, cache)
}

// GetSignaturesWithFormat returns the image's signatures.  It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
func (s *openshiftImageSource) GetSignaturesWithFormat(ctx context.Context, instanceDigest *digest.Digest) ([]signature.Signature, error) {
	var imageStreamImageName string
	if instanceDigest == nil {
		if err := s.ensureImageIsResolved(ctx); err != nil {
			return nil, err
		}
		imageStreamImageName = s.imageStreamImageName
	} else {
		if err := instanceDigest.Validate(); err != nil { // Make sure instanceDigest.String() does not contain any unexpected characters
			return nil, err
		}
		imageStreamImageName = instanceDigest.String()
	}
	image, err := s.client.getImage(ctx, imageStreamImageName)
	if err != nil {
		return nil, err
	}
	var sigs []signature.Signature
	for _, sig := range image.Signatures {
		if sig.Type == imageSignatureTypeAtomic {
			sigs = append(sigs, signature.SimpleSigningFromBlob(sig.Content))
		}
	}
	return sigs, nil
}

// ensureImageIsResolved sets up s.docker and s.imageStreamImageName
func (s *openshiftImageSource) ensureImageIsResolved(ctx context.Context) error {
	if s.docker != nil {
		return nil
	}

	// FIXME: validate components per validation.IsValidPathSegmentName?
	path := fmt.Sprintf("/oapi/v1/namespaces/%s/imagestreams/%s", s.client.ref.namespace, s.client.ref.stream)
	body, err := s.client.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	// Note: This does absolutely no kind/version checking or conversions.
	var is imageStream
	if err := json.Unmarshal(body, &is); err != nil {
		return err
	}
	var te *tagEvent
	for _, tag := range is.Status.Tags {
		if tag.Tag != s.client.ref.dockerReference.Tag() {
			continue
		}
		if len(tag.Items) > 0 {
			te = &tag.Items[0]
			break
		}
	}
	if te == nil {
		return errors.New("No matching tag found")
	}
	logrus.Debugf("tag event %#v", te)
	dockerRefString, err := s.client.convertDockerImageReference(te.DockerImageReference)
	if err != nil {
		return err
	}
	logrus.Debugf("Resolved reference %#v", dockerRefString)
	dockerRef, err := docker.ParseReference("//" + dockerRefString)
	if err != nil {
		return err
	}
	d, err := dockerRef.NewImageSource(ctx, s.sys)
	if err != nil {
		return err
	}
	s.docker = d
	s.imageStreamImageName = te.Image
	return nil
}
