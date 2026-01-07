package tarfile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/imagedestination/impl"
	"go.podman.io/image/v5/internal/imagedestination/stubs"
	"go.podman.io/image/v5/internal/iolimits"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/streamdigest"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/types"
)

// Destination is a partial implementation of private.ImageDestination for writing to a Writer.
type Destination struct {
	impl.Compat
	impl.PropertyMethodsInitialize
	stubs.IgnoresOriginalOCIConfig
	stubs.NoPutBlobPartialInitialize
	stubs.NoSignaturesInitialize

	archive           *Writer
	commitWithOptions func(ctx context.Context, options private.CommitOptions) error
	repoTags          []reference.NamedTagged
	// Other state.
	config []byte
	sysCtx *types.SystemContext
}

// NewDestination returns a tarfile.Destination adding images to the specified Writer.
// commitWithOptions implements ImageDestination.CommitWithOptions.
func NewDestination(sys *types.SystemContext, archive *Writer, transportName string, ref reference.NamedTagged,
	commitWithOptions func(ctx context.Context, options private.CommitOptions) error) *Destination {
	repoTags := []reference.NamedTagged{}
	if ref != nil {
		repoTags = append(repoTags, ref)
	}
	dest := &Destination{
		PropertyMethodsInitialize: impl.PropertyMethods(impl.Properties{
			SupportedManifestMIMETypes: []string{
				manifest.DockerV2Schema2MediaType, // We rely on the types.Image.UpdatedImage schema conversion capabilities.
			},
			DesiredLayerCompression:        types.Decompress,
			AcceptsForeignLayerURLs:        false,
			MustMatchRuntimeOS:             false,
			IgnoresEmbeddedDockerReference: false, // N/A, we only accept schema2 images where EmbeddedDockerReferenceConflicts() is always false.
			// The code _is_ actually thread-safe, but apart from computing sizes/digests of layers where
			// this is unknown in advance, the actual copy is serialized by d.archive, so there probably isn’t
			// much benefit from concurrency, mostly just extra CPU, memory and I/O contention.
			HasThreadSafePutBlob: false,
		}),
		NoPutBlobPartialInitialize: stubs.NoPutBlobPartialRaw(transportName),
		NoSignaturesInitialize:     stubs.NoSignatures("Storing signatures for docker tar files is not supported"),

		archive:           archive,
		commitWithOptions: commitWithOptions,
		repoTags:          repoTags,
		sysCtx:            sys,
	}
	dest.Compat = impl.AddCompat(dest)
	return dest
}

// AddRepoTags adds the specified tags to the destination's repoTags.
func (d *Destination) AddRepoTags(tags []reference.NamedTagged) {
	d.repoTags = append(d.repoTags, tags...)
}

// PutBlobWithOptions writes contents of stream and returns data representing the result.
// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
// inputInfo.Size is the expected length of stream, if known.
// inputInfo.MediaType describes the blob format, if known.
// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
// to any other readers for download using the supplied digest.
// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlobWithOptions MUST 1) fail, and 2) delete any data stored so far.
func (d *Destination) PutBlobWithOptions(ctx context.Context, stream io.Reader, inputInfo types.BlobInfo, options private.PutBlobOptions) (private.UploadedBlob, error) {
	// Ouch, we need to stream the blob into a temporary file just to determine the size.
	// When the layer is decompressed, we also have to generate the digest on uncompressed data.
	if inputInfo.Size == -1 || inputInfo.Digest == "" {
		logrus.Debugf("docker tarfile: input with unknown size, streaming to disk first ...")
		streamCopy, cleanup, err := streamdigest.ComputeBlobInfo(d.sysCtx, stream, &inputInfo)
		if err != nil {
			return private.UploadedBlob{}, err
		}
		defer cleanup()
		stream = streamCopy
		logrus.Debugf("... streaming done")
	}

	if err := d.archive.lock(); err != nil {
		return private.UploadedBlob{}, err
	}
	defer d.archive.unlock()

	// Maybe the blob has been already sent
	ok, reusedInfo, err := d.archive.tryReusingBlobLocked(inputInfo)
	if err != nil {
		return private.UploadedBlob{}, err
	}
	if ok {
		return private.UploadedBlob{Digest: reusedInfo.Digest, Size: reusedInfo.Size}, nil
	}

	if options.IsConfig {
		buf, err := iolimits.ReadAtMost(stream, iolimits.MaxConfigBodySize)
		if err != nil {
			return private.UploadedBlob{}, fmt.Errorf("reading Config file stream: %w", err)
		}
		d.config = buf
		configPath, err := d.archive.configPath(inputInfo.Digest)
		if err != nil {
			return private.UploadedBlob{}, err
		}
		if err := d.archive.sendFileLocked(configPath, inputInfo.Size, bytes.NewReader(buf)); err != nil {
			return private.UploadedBlob{}, fmt.Errorf("writing Config file: %w", err)
		}
	} else {
		layerPath, err := d.archive.physicalLayerPath(inputInfo.Digest)
		if err != nil {
			return private.UploadedBlob{}, err
		}
		if err := d.archive.sendFileLocked(layerPath, inputInfo.Size, stream); err != nil {
			return private.UploadedBlob{}, err
		}
	}
	d.archive.recordBlobLocked(types.BlobInfo{Digest: inputInfo.Digest, Size: inputInfo.Size})
	return private.UploadedBlob{Digest: inputInfo.Digest, Size: inputInfo.Size}, nil
}

// TryReusingBlobWithOptions checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
// info.Digest must not be empty.
// If the blob has been successfully reused, returns (true, info, nil).
// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
func (d *Destination) TryReusingBlobWithOptions(ctx context.Context, info types.BlobInfo, options private.TryReusingBlobOptions) (bool, private.ReusedBlob, error) {
	if !impl.OriginalCandidateMatchesTryReusingBlobOptions(options) {
		return false, private.ReusedBlob{}, nil
	}
	if err := d.archive.lock(); err != nil {
		return false, private.ReusedBlob{}, err
	}
	defer d.archive.unlock()

	return d.archive.tryReusingBlobLocked(info)
}

// PutManifest writes manifest to the destination.
// The instanceDigest value is expected to always be nil, because this transport does not support manifest lists, so
// there can be no secondary manifests.
// FIXME? This should also receive a MIME type if known, to differentiate between schema versions.
// If the destination is in principle available, refuses this manifest type (e.g. it does not recognize the schema),
// but may accept a different manifest type, the returned error must be an ManifestTypeRejectedError.
func (d *Destination) PutManifest(ctx context.Context, m []byte, instanceDigest *digest.Digest) error {
	if instanceDigest != nil {
		return errors.New(`Manifest lists are not supported for docker tar files`)
	}
	// We do not bother with types.ManifestTypeRejectedError; our .SupportedManifestMIMETypes() above is already providing only one alternative,
	// so the caller trying a different manifest kind would be pointless.
	var man manifest.Schema2
	if err := json.Unmarshal(m, &man); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}
	if man.SchemaVersion != 2 || man.MediaType != manifest.DockerV2Schema2MediaType {
		return errors.New("Unsupported manifest type, need a Docker schema 2 manifest")
	}

	if err := d.archive.lock(); err != nil {
		return err
	}
	defer d.archive.unlock()

	if err := d.archive.writeLegacyMetadataLocked(man.LayersDescriptors, d.config, d.repoTags); err != nil {
		return err
	}

	return d.archive.ensureManifestItemLocked(man.LayersDescriptors, man.ConfigDescriptor.Digest, d.repoTags)
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (d *Destination) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	// This indirection exists because impl.Compat expects all ImageDestinationInternalOnly methods
	// to be implemented in one place.
	return d.commitWithOptions(ctx, options)
}
