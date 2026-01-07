package directory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/imagedestination/impl"
	"go.podman.io/image/v5/internal/imagedestination/stubs"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/putblobdigest"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
)

const version = "Directory Transport Version: 1.1\n"

// ErrNotContainerImageDir indicates that the directory doesn't match the expected contents of a directory created
// using the 'dir' transport
var ErrNotContainerImageDir = errors.New("not a containers image directory, don't want to overwrite important data")

type dirImageDestination struct {
	impl.Compat
	impl.PropertyMethodsInitialize
	stubs.IgnoresOriginalOCIConfig
	stubs.NoPutBlobPartialInitialize
	stubs.AlwaysSupportsSignatures

	ref dirReference
}

// newImageDestination returns an ImageDestination for writing to a directory.
func newImageDestination(sys *types.SystemContext, ref dirReference) (private.ImageDestination, error) {
	desiredLayerCompression := types.PreserveOriginal
	if sys != nil {
		if sys.DirForceCompress {
			desiredLayerCompression = types.Compress

			if sys.DirForceDecompress {
				return nil, fmt.Errorf("Cannot compress and decompress at the same time")
			}
		}
		if sys.DirForceDecompress {
			desiredLayerCompression = types.Decompress
		}
	}

	// If directory exists check if it is empty
	// if not empty, check whether the contents match that of a container image directory and overwrite the contents
	// if the contents don't match throw an error
	dirExists, err := pathExists(ref.resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("checking for path %q: %w", ref.resolvedPath, err)
	}
	if dirExists {
		isEmpty, err := isDirEmpty(ref.resolvedPath)
		if err != nil {
			return nil, err
		}

		if !isEmpty {
			versionExists, err := pathExists(ref.versionPath())
			if err != nil {
				return nil, fmt.Errorf("checking if path exists %q: %w", ref.versionPath(), err)
			}
			if versionExists {
				contents, err := os.ReadFile(ref.versionPath())
				if err != nil {
					return nil, err
				}
				// check if contents of version file is what we expect it to be
				if string(contents) != version {
					return nil, ErrNotContainerImageDir
				}
			} else {
				return nil, ErrNotContainerImageDir
			}
			// delete directory contents so that only one image is in the directory at a time
			if err = removeDirContents(ref.resolvedPath); err != nil {
				return nil, fmt.Errorf("erasing contents in %q: %w", ref.resolvedPath, err)
			}
			logrus.Debugf("overwriting existing container image directory %q", ref.resolvedPath)
		}
	} else {
		// create directory if it doesn't exist
		if err := os.MkdirAll(ref.resolvedPath, 0755); err != nil {
			return nil, fmt.Errorf("unable to create directory %q: %w", ref.resolvedPath, err)
		}
	}
	// create version file
	err = os.WriteFile(ref.versionPath(), []byte(version), 0644)
	if err != nil {
		return nil, fmt.Errorf("creating version file %q: %w", ref.versionPath(), err)
	}

	d := &dirImageDestination{
		PropertyMethodsInitialize: impl.PropertyMethods(impl.Properties{
			SupportedManifestMIMETypes:     nil,
			DesiredLayerCompression:        desiredLayerCompression,
			AcceptsForeignLayerURLs:        false,
			MustMatchRuntimeOS:             false,
			IgnoresEmbeddedDockerReference: false, // N/A, DockerReference() returns nil.
			HasThreadSafePutBlob:           true,
		}),
		NoPutBlobPartialInitialize: stubs.NoPutBlobPartial(ref),

		ref: ref,
	}
	d.Compat = impl.AddCompat(d)
	return d, nil
}

// Reference returns the reference used to set up this destination.  Note that this should directly correspond to user's intent,
// e.g. it should use the public hostname instead of the result of resolving CNAMEs or following redirects.
func (d *dirImageDestination) Reference() types.ImageReference {
	return d.ref
}

// Close removes resources associated with an initialized ImageDestination, if any.
func (d *dirImageDestination) Close() error {
	return nil
}

// PutBlobWithOptions writes contents of stream and returns data representing the result.
// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
// inputInfo.Size is the expected length of stream, if known.
// inputInfo.MediaType describes the blob format, if known.
// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
// to any other readers for download using the supplied digest.
// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlobWithOptions MUST 1) fail, and 2) delete any data stored so far.
func (d *dirImageDestination) PutBlobWithOptions(ctx context.Context, stream io.Reader, inputInfo types.BlobInfo, options private.PutBlobOptions) (private.UploadedBlob, error) {
	blobFile, err := os.CreateTemp(d.ref.path, "dir-put-blob")
	if err != nil {
		return private.UploadedBlob{}, err
	}
	succeeded := false
	explicitClosed := false
	defer func() {
		if !explicitClosed {
			blobFile.Close()
		}
		if !succeeded {
			os.Remove(blobFile.Name())
		}
	}()

	digester, stream := putblobdigest.DigestIfCanonicalUnknown(stream, inputInfo)
	// TODO: This can take quite some time, and should ideally be cancellable using ctx.Done().
	size, err := io.Copy(blobFile, stream)
	if err != nil {
		return private.UploadedBlob{}, err
	}
	blobDigest := digester.Digest()
	if inputInfo.Size != -1 && size != inputInfo.Size {
		return private.UploadedBlob{}, fmt.Errorf("Size mismatch when copying %s, expected %d, got %d", blobDigest, inputInfo.Size, size)
	}
	if err := blobFile.Sync(); err != nil {
		return private.UploadedBlob{}, err
	}

	// On POSIX systems, blobFile was created with mode 0600, so we need to make it readable.
	// On Windows, the “permissions of newly created files” argument to syscall.Open is
	// ignored and the file is already readable; besides, blobFile.Chmod, i.e. syscall.Fchmod,
	// always fails on Windows.
	if runtime.GOOS != "windows" {
		if err := blobFile.Chmod(0644); err != nil {
			return private.UploadedBlob{}, err
		}
	}

	blobPath, err := d.ref.layerPath(blobDigest)
	if err != nil {
		return private.UploadedBlob{}, err
	}
	// need to explicitly close the file, since a rename won't otherwise not work on Windows
	blobFile.Close()
	explicitClosed = true
	if err := os.Rename(blobFile.Name(), blobPath); err != nil {
		return private.UploadedBlob{}, err
	}
	succeeded = true
	return private.UploadedBlob{Digest: blobDigest, Size: size}, nil
}

// TryReusingBlobWithOptions checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
// info.Digest must not be empty.
// If the blob has been successfully reused, returns (true, info, nil).
// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
func (d *dirImageDestination) TryReusingBlobWithOptions(ctx context.Context, info types.BlobInfo, options private.TryReusingBlobOptions) (bool, private.ReusedBlob, error) {
	if !impl.OriginalCandidateMatchesTryReusingBlobOptions(options) {
		return false, private.ReusedBlob{}, nil
	}
	if info.Digest == "" {
		return false, private.ReusedBlob{}, fmt.Errorf("Can not check for a blob with unknown digest")
	}
	blobPath, err := d.ref.layerPath(info.Digest)
	if err != nil {
		return false, private.ReusedBlob{}, err
	}
	finfo, err := os.Stat(blobPath)
	if err != nil && os.IsNotExist(err) {
		return false, private.ReusedBlob{}, nil
	}
	if err != nil {
		return false, private.ReusedBlob{}, err
	}
	return true, private.ReusedBlob{Digest: info.Digest, Size: finfo.Size()}, nil
}

// PutManifest writes manifest to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write the manifest for (when
// the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// It is expected but not enforced that the instanceDigest, when specified, matches the digest of `manifest` as generated
// by `manifest.Digest()`.
// FIXME? This should also receive a MIME type if known, to differentiate between schema versions.
// If the destination is in principle available, refuses this manifest type (e.g. it does not recognize the schema),
// but may accept a different manifest type, the returned error must be an ManifestTypeRejectedError.
func (d *dirImageDestination) PutManifest(ctx context.Context, manifest []byte, instanceDigest *digest.Digest) error {
	path, err := d.ref.manifestPath(instanceDigest)
	if err != nil {
		return err
	}
	return os.WriteFile(path, manifest, 0644)
}

// PutSignaturesWithFormat writes a set of signatures to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// MUST be called after PutManifest (signatures may reference manifest contents).
func (d *dirImageDestination) PutSignaturesWithFormat(ctx context.Context, signatures []signature.Signature, instanceDigest *digest.Digest) error {
	for i, sig := range signatures {
		blob, err := signature.Blob(sig)
		if err != nil {
			return err
		}
		path, err := d.ref.signaturePath(i, instanceDigest)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, blob, 0644); err != nil {
			return err
		}
	}
	return nil
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (d *dirImageDestination) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	return nil
}

// returns true if path exists
func pathExists(path string) (bool, error) {
	err := fileutils.Exists(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// returns true if directory is empty
func isDirEmpty(path string) (bool, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(files) == 0, nil
}

// deletes the contents of a directory
func removeDirContents(path string) error {
	files, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, file := range files {
		if err := os.RemoveAll(filepath.Join(path, file.Name())); err != nil {
			return err
		}
	}
	return nil
}
