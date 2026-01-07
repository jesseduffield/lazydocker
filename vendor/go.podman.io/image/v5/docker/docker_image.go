package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/types"
)

// Image is a Docker-specific implementation of types.ImageCloser with a few extra methods
// which are specific to Docker.
type Image struct {
	types.ImageCloser
	src *dockerImageSource
}

// newImage returns a new Image interface type after setting up
// a client to the registry hosting the given image.
// The caller must call .Close() on the returned Image.
func newImage(ctx context.Context, sys *types.SystemContext, ref dockerReference) (types.ImageCloser, error) {
	s, err := newImageSource(ctx, sys, ref)
	if err != nil {
		return nil, err
	}
	img, err := image.FromSource(ctx, sys, s)
	if err != nil {
		return nil, err
	}
	return &Image{ImageCloser: img, src: s}, nil
}

// SourceRefFullName returns a fully expanded name for the repository this image is in.
func (i *Image) SourceRefFullName() string {
	return i.src.logicalRef.ref.Name()
}

// GetRepositoryTags list all tags available in the repository. The tag
// provided inside the ImageReference will be ignored. (This is a
// backward-compatible shim method which calls the module-level
// GetRepositoryTags)
func (i *Image) GetRepositoryTags(ctx context.Context) ([]string, error) {
	return GetRepositoryTags(ctx, i.src.c.sys, i.src.logicalRef)
}

// GetRepositoryTags list all tags available in the repository. The tag
// provided inside the ImageReference will be ignored.
func GetRepositoryTags(ctx context.Context, sys *types.SystemContext, ref types.ImageReference) ([]string, error) {
	dr, ok := ref.(dockerReference)
	if !ok {
		return nil, errors.New("ref must be a dockerReference")
	}

	registryConfig, err := loadRegistryConfiguration(sys)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf(tagsPath, reference.Path(dr.ref))
	client, err := newDockerClientFromRef(sys, dr, registryConfig, false, "pull")
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	tags := make([]string, 0)

	for {
		res, err := client.makeRequest(ctx, http.MethodGet, path, nil, nil, v2Auth, nil)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetching tags list: %w", registryHTTPResponseToError(res))
		}

		var tagsHolder struct {
			Tags []string
		}
		if err = json.NewDecoder(res.Body).Decode(&tagsHolder); err != nil {
			return nil, err
		}
		for _, tag := range tagsHolder.Tags {
			if _, err := reference.WithTag(dr.ref, tag); err != nil { // Ensure the tag does not contain unexpected values
				// Per https://github.com/containers/skopeo/issues/2409 , Sonatype Nexus 3.58, contrary
				// to the spec, may include JSON null values in the list; and Go silently parses them as "".
				if tag == "" {
					logrus.Debugf("Ignoring invalid empty tag")
					continue
				}
				// Per https://github.com/containers/skopeo/issues/2346 , unknown versions of JFrog Artifactory,
				// contrary to the tag format specified in
				// https://github.com/opencontainers/distribution-spec/blob/8a871c8234977df058f1a14e299fe0a673853da2/spec.md?plain=1#L160 ,
				// include digests in the list.
				if _, err := digest.Parse(tag); err == nil {
					logrus.Debugf("Ignoring invalid tag %q matching a digest format", tag)
					continue
				}
				return nil, fmt.Errorf("registry returned invalid tag %q: %w", tag, err)
			}
			tags = append(tags, tag)
		}

		link := res.Header.Get("Link")
		if link == "" {
			break
		}

		linkURLPart, _, _ := strings.Cut(link, ";")
		linkURL, err := url.Parse(strings.Trim(linkURLPart, "<>"))
		if err != nil {
			return tags, err
		}

		// can be relative or absolute, but we only want the path (and I
		// guess we're in trouble if it forwards to a new place...)
		path = linkURL.Path
		if linkURL.RawQuery != "" {
			path += "?"
			path += linkURL.RawQuery
		}
	}
	return tags, nil
}

// GetDigest returns the image's digest
// Use this to optimize and avoid use of an ImageSource based on the returned digest;
// if you are going to use an ImageSource anyway, it’s more efficient to create it first
// and compute the digest from the value returned by GetManifest.
// NOTE: Implemented to avoid Docker Hub API limits, and mirror configuration may be
// ignored (but may be implemented in the future)
func GetDigest(ctx context.Context, sys *types.SystemContext, ref types.ImageReference) (digest.Digest, error) {
	dr, ok := ref.(dockerReference)
	if !ok {
		return "", errors.New("ref must be a dockerReference")
	}
	if dr.isUnknownDigest {
		return "", fmt.Errorf("docker: reference %q is for unknown digest case; cannot get digest", dr.StringWithinTransport())
	}

	tagOrDigest, err := dr.tagOrDigest()
	if err != nil {
		return "", err
	}

	registryConfig, err := loadRegistryConfiguration(sys)
	if err != nil {
		return "", err
	}
	client, err := newDockerClientFromRef(sys, dr, registryConfig, false, "pull")
	if err != nil {
		return "", fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	path := fmt.Sprintf(manifestPath, reference.Path(dr.ref), tagOrDigest)
	headers := map[string][]string{
		"Accept": manifest.DefaultRequestedManifestMIMETypes,
	}

	res, err := client.makeRequest(ctx, http.MethodHead, path, headers, nil, v2Auth, nil)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reading digest %s in %s: %w", tagOrDigest, dr.ref.Name(), registryHTTPResponseToError(res))
	}

	dig, err := digest.Parse(res.Header.Get("Docker-Content-Digest"))
	if err != nil {
		return "", err
	}

	return dig, nil
}
