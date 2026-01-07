package layout

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/docker/go-connections/tlsconfig"
	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/internal/imagesource/impl"
	"go.podman.io/image/v5/internal/imagesource/stubs"
	"go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/pkg/tlsclientconfig"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
)

// ImageNotFoundError is used when the OCI structure, in principle, exists and seems valid enough,
// but nothing matches the “image” part of the provided reference.
type ImageNotFoundError struct {
	ref ociReference
	// We may make members public, or add methods, in the future.
}

func (e ImageNotFoundError) Error() string {
	return fmt.Sprintf("no descriptor found for reference %q", e.ref.image)
}

type ociImageSource struct {
	impl.Compat
	impl.PropertyMethodsInitialize
	impl.NoSignatures
	impl.DoesNotAffectLayerInfosForCopy
	stubs.NoGetBlobAtInitialize

	ref           ociReference
	index         *imgspecv1.Index
	descriptor    imgspecv1.Descriptor
	client        *http.Client
	sharedBlobDir string
}

// newImageSource returns an ImageSource for reading from an existing directory.
func newImageSource(sys *types.SystemContext, ref ociReference) (private.ImageSource, error) {
	tr := tlsclientconfig.NewTransport()
	tr.TLSClientConfig = &tls.Config{
		// As of 2025-08, tlsconfig.ClientDefault() differs from Go 1.23 defaults only in CipherSuites;
		// so, limit us to only using that value. If go-connections/tlsconfig changes its policy, we
		// will want to consider that and make a decision whether to follow suit.
		// There is some chance that eventually the Go default will be to require TLS 1.3, and that point
		// we might want to drop the dependency on go-connections entirely.
		CipherSuites: tlsconfig.ClientDefault().CipherSuites,
	}

	if sys != nil && sys.OCICertPath != "" {
		if err := tlsclientconfig.SetupCertificates(sys.OCICertPath, tr.TLSClientConfig); err != nil {
			return nil, err
		}
		tr.TLSClientConfig.InsecureSkipVerify = sys.OCIInsecureSkipTLSVerify
	}

	client := &http.Client{}
	client.Transport = tr
	descriptor, _, err := ref.getManifestDescriptor()
	if err != nil {
		return nil, err
	}
	index, err := ref.getIndex()
	if err != nil {
		return nil, err
	}
	s := &ociImageSource{
		PropertyMethodsInitialize: impl.PropertyMethods(impl.Properties{
			HasThreadSafeGetBlob: false,
		}),
		NoGetBlobAtInitialize: stubs.NoGetBlobAt(ref),

		ref:        ref,
		index:      index,
		descriptor: descriptor,
		client:     client,
	}
	if sys != nil {
		// TODO(jonboulle): check dir existence?
		s.sharedBlobDir = sys.OCISharedBlobDirPath
	}
	s.Compat = impl.AddCompat(s)
	return s, nil
}

// Reference returns the reference used to set up this source.
func (s *ociImageSource) Reference() types.ImageReference {
	return s.ref
}

// Close removes resources associated with an initialized ImageSource, if any.
func (s *ociImageSource) Close() error {
	s.client.CloseIdleConnections()
	return nil
}

// GetManifest returns the image's manifest along with its MIME type (which may be empty when it can't be determined but the manifest is available).
// It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve (when the primary manifest is a manifest list);
// this never happens if the primary manifest is not a manifest list (e.g. if the source never returns manifest lists).
func (s *ociImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	var dig digest.Digest
	var mimeType string
	var err error

	if instanceDigest == nil {
		dig = s.descriptor.Digest
		mimeType = s.descriptor.MediaType
	} else {
		dig = *instanceDigest
		for _, md := range s.index.Manifests {
			if md.Digest == dig {
				mimeType = md.MediaType
				break
			}
		}
	}

	manifestPath, err := s.ref.blobPath(dig, s.sharedBlobDir)
	if err != nil {
		return nil, "", err
	}

	m, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", err
	}
	if mimeType == "" {
		mimeType = manifest.GuessMIMEType(m)
	}

	return m, mimeType, nil
}

// GetBlob returns a stream for the specified blob, and the blob’s size (or -1 if unknown).
// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
func (s *ociImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	if len(info.URLs) != 0 {
		r, s, err := s.getExternalBlob(ctx, info.URLs)
		if err != nil {
			return nil, 0, err
		} else if r != nil {
			return r, s, nil
		}
	}

	path, err := s.ref.blobPath(info.Digest, s.sharedBlobDir)
	if err != nil {
		return nil, 0, err
	}

	r, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	fi, err := r.Stat()
	if err != nil {
		return nil, 0, err
	}
	return r, fi.Size(), nil
}

// getExternalBlob returns the reader of the first available blob URL from urls, which must not be empty.
// This function can return nil reader when no url is supported by this function. In this case, the caller
// should fallback to fetch the non-external blob (i.e. pull from the registry).
func (s *ociImageSource) getExternalBlob(ctx context.Context, urls []string) (io.ReadCloser, int64, error) {
	if len(urls) == 0 {
		return nil, 0, errors.New("internal error: getExternalBlob called with no URLs")
	}

	errWrap := errors.New("failed fetching external blob from all urls")
	hasSupportedURL := false
	for _, u := range urls {
		if u, err := url.Parse(u); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue // unsupported url. skip this url.
		}
		hasSupportedURL = true
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			errWrap = fmt.Errorf("fetching %q failed %s: %w", u, err.Error(), errWrap)
			continue
		}

		resp, err := s.client.Do(req)
		if err != nil {
			errWrap = fmt.Errorf("fetching %q failed %s: %w", u, err.Error(), errWrap)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			errWrap = fmt.Errorf("fetching %q failed, response code not 200: %w", u, errWrap)
			continue
		}

		return resp.Body, getBlobSize(resp), nil
	}
	if !hasSupportedURL {
		return nil, 0, nil // fallback to non-external blob
	}

	return nil, 0, errWrap
}

func getBlobSize(resp *http.Response) int64 {
	size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		size = -1
	}
	return size
}

// GetLocalBlobPath returns the local path to the blob file with the given digest.
// The returned path is checked for existence so when a non existing digest is
// given an error will be returned.
//
// Important: The returned path must be treated as read only, writing the file will
// corrupt the oci layout as the digest no longer matches.
func GetLocalBlobPath(ctx context.Context, src types.ImageSource, digest digest.Digest) (string, error) {
	s, ok := src.(*ociImageSource)
	if !ok {
		return "", errors.New("caller error: GetLocalBlobPath called with a non-oci: source")
	}

	path, err := s.ref.blobPath(digest, s.sharedBlobDir)
	if err != nil {
		return "", err
	}
	if err := fileutils.Exists(path); err != nil {
		return "", err
	}

	return path, nil
}
