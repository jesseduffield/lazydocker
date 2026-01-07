package sif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/opencontainers/go-digest"
	imgspecs "github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/sylabs/sif/v2/pkg/sif"
	"go.podman.io/image/v5/internal/imagesource/impl"
	"go.podman.io/image/v5/internal/imagesource/stubs"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/tmpdir"
	"go.podman.io/image/v5/types"
)

type sifImageSource struct {
	impl.Compat
	impl.PropertyMethodsInitialize
	impl.NoSignatures
	impl.DoesNotAffectLayerInfosForCopy
	stubs.NoGetBlobAtInitialize

	ref          sifReference
	workDir      string
	layerDigest  digest.Digest
	layerSize    int64
	layerFile    string
	config       []byte
	configDigest digest.Digest
	manifest     []byte
}

// getBlobInfo returns the digest,  and size of the provided file.
func getBlobInfo(path string) (digest.Digest, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", -1, fmt.Errorf("opening %q for reading: %w", path, err)
	}
	defer f.Close()

	// TODO: Instead of writing the tar file to disk, and reading
	// it here again, stream the tar file to a pipe and
	// compute the digest while writing it to disk.
	logrus.Debugf("Computing a digest of the SIF conversion output...")
	digester := digest.Canonical.Digester()
	// TODO: This can take quite some time, and should ideally be cancellable using ctx.Done().
	size, err := io.Copy(digester.Hash(), f)
	if err != nil {
		return "", -1, fmt.Errorf("reading %q: %w", path, err)
	}
	digest := digester.Digest()
	logrus.Debugf("... finished computing the digest of the SIF conversion output")

	return digest, size, nil
}

// newImageSource returns an ImageSource for reading from an existing directory.
// newImageSource extracts SIF objects and saves them in a temp directory.
func newImageSource(ctx context.Context, sys *types.SystemContext, ref sifReference) (private.ImageSource, error) {
	sifImg, err := sif.LoadContainerFromPath(ref.file, sif.OptLoadWithFlag(os.O_RDONLY))
	if err != nil {
		return nil, fmt.Errorf("loading SIF file: %w", err)
	}
	defer func() {
		_ = sifImg.UnloadContainer()
	}()

	workDir, err := tmpdir.MkDirBigFileTemp(sys, "sif")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			os.RemoveAll(workDir)
		}
	}()

	layerPath, commandLine, err := convertSIFToElements(ctx, sifImg, workDir)
	if err != nil {
		return nil, fmt.Errorf("converting rootfs from SquashFS to Tarball: %w", err)
	}

	layerDigest, layerSize, err := getBlobInfo(layerPath)
	if err != nil {
		return nil, fmt.Errorf("gathering blob information: %w", err)
	}

	created := sifImg.ModifiedAt()
	config := imgspecv1.Image{
		Created: &created,
		Platform: imgspecv1.Platform{
			Architecture: sifImg.PrimaryArch(),
			OS:           "linux",
		},
		Config: imgspecv1.ImageConfig{
			Cmd: commandLine,
		},
		RootFS: imgspecv1.RootFS{
			Type:    "layers",
			DiffIDs: []digest.Digest{layerDigest},
		},
		History: []imgspecv1.History{
			{
				Created:   &created,
				CreatedBy: fmt.Sprintf("/bin/sh -c #(nop) ADD file:%s in %c", layerDigest.Encoded(), os.PathSeparator),
				Comment:   "imported from SIF, uuid: " + sifImg.ID(),
			},
			{
				Created:    &created,
				CreatedBy:  "/bin/sh -c #(nop) CMD [\"bash\"]",
				EmptyLayer: true,
			},
		},
	}
	configBytes, err := json.Marshal(&config)
	if err != nil {
		return nil, fmt.Errorf("generating configuration blob for %q: %w", ref.resolvedFile, err)
	}
	configDigest := digest.Canonical.FromBytes(configBytes)

	manifest := imgspecv1.Manifest{
		Versioned: imgspecs.Versioned{SchemaVersion: 2},
		MediaType: imgspecv1.MediaTypeImageManifest,
		Config: imgspecv1.Descriptor{
			Digest:    configDigest,
			Size:      int64(len(configBytes)),
			MediaType: imgspecv1.MediaTypeImageConfig,
		},
		Layers: []imgspecv1.Descriptor{{
			Digest:    layerDigest,
			Size:      layerSize,
			MediaType: imgspecv1.MediaTypeImageLayer,
		}},
	}
	manifestBytes, err := json.Marshal(&manifest)
	if err != nil {
		return nil, fmt.Errorf("generating manifest for %q: %w", ref.resolvedFile, err)
	}

	succeeded = true
	s := &sifImageSource{
		PropertyMethodsInitialize: impl.PropertyMethods(impl.Properties{
			HasThreadSafeGetBlob: true,
		}),
		NoGetBlobAtInitialize: stubs.NoGetBlobAt(ref),

		ref:          ref,
		workDir:      workDir,
		layerDigest:  layerDigest,
		layerSize:    layerSize,
		layerFile:    layerPath,
		config:       configBytes,
		configDigest: configDigest,
		manifest:     manifestBytes,
	}
	s.Compat = impl.AddCompat(s)
	return s, nil
}

// Reference returns the reference used to set up this source.
func (s *sifImageSource) Reference() types.ImageReference {
	return s.ref
}

// Close removes resources associated with an initialized ImageSource, if any.
func (s *sifImageSource) Close() error {
	return os.RemoveAll(s.workDir)
}

// GetBlob returns a stream for the specified blob, and the blob’s size (or -1 if unknown).
// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
func (s *sifImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	switch info.Digest {
	case s.configDigest:
		return io.NopCloser(bytes.NewReader(s.config)), int64(len(s.config)), nil
	case s.layerDigest:
		reader, err := os.Open(s.layerFile)
		if err != nil {
			return nil, -1, fmt.Errorf("opening %q: %w", s.layerFile, err)
		}
		return reader, s.layerSize, nil
	default:
		return nil, -1, fmt.Errorf("no blob with digest %q found", info.Digest.String())
	}
}

// GetManifest returns the image's manifest along with its MIME type (which may be empty when it can't be determined but the manifest is available).
// It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve (when the primary manifest is a manifest list);
// this never happens if the primary manifest is not a manifest list (e.g. if the source never returns manifest lists).
func (s *sifImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	if instanceDigest != nil {
		return nil, "", errors.New("manifest lists are not supported by the sif transport")
	}
	return s.manifest, imgspecv1.MediaTypeImageManifest, nil
}
