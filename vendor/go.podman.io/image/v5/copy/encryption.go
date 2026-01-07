package copy

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/containers/ocicrypt"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/types"
)

// isOciEncrypted returns a bool indicating if a mediatype is encrypted
// This function will be moved to be part of OCI spec when adopted.
func isOciEncrypted(mediatype string) bool {
	return strings.HasSuffix(mediatype, "+encrypted")
}

// isEncrypted checks if an image is encrypted
func isEncrypted(i types.Image) bool {
	layers := i.LayerInfos()
	return slices.ContainsFunc(layers, func(l types.BlobInfo) bool {
		return isOciEncrypted(l.MediaType)
	})
}

// bpDecryptionStepData contains data that the copy pipeline needs about the decryption step.
type bpDecryptionStepData struct {
	decrypting bool // We are actually decrypting the stream
}

// blobPipelineDecryptionStep updates *stream to decrypt if, it necessary.
// srcInfo is only used for error messages.
// Returns data for other steps; the caller should eventually use updateCryptoOperation.
func (ic *imageCopier) blobPipelineDecryptionStep(stream *sourceStream, srcInfo types.BlobInfo) (*bpDecryptionStepData, error) {
	if !isOciEncrypted(stream.info.MediaType) || ic.c.options.OciDecryptConfig == nil {
		return &bpDecryptionStepData{
			decrypting: false,
		}, nil
	}

	if ic.cannotModifyManifestReason != "" {
		return nil, fmt.Errorf("layer %s should be decrypted, but we can’t modify the manifest: %s", srcInfo.Digest, ic.cannotModifyManifestReason)
	}

	desc := imgspecv1.Descriptor{
		Annotations: stream.info.Annotations,
	}
	// DecryptLayer supposedly returns a digest of the decrypted stream.
	// In practice, that value is never set in the current implementation.
	// And we shouldn’t use it anyway, because it is not trusted: encryption can be made to a public key,
	// i.e. it doesn’t authenticate the origin of the metadata in any way.
	reader, _, err := ocicrypt.DecryptLayer(ic.c.options.OciDecryptConfig, stream.reader, desc, false)
	if err != nil {
		return nil, fmt.Errorf("decrypting layer %s: %w", srcInfo.Digest, err)
	}

	stream.reader = reader
	stream.info.Digest = ""
	stream.info.Size = -1
	maps.DeleteFunc(stream.info.Annotations, func(k string, _ string) bool {
		return strings.HasPrefix(k, "org.opencontainers.image.enc")
	})
	return &bpDecryptionStepData{
		decrypting: true,
	}, nil
}

// updateCryptoOperation sets *operation, if necessary.
func (d *bpDecryptionStepData) updateCryptoOperation(operation *types.LayerCrypto) {
	if d.decrypting {
		*operation = types.Decrypt
	}
}

// bpEncryptionStepData contains data that the copy pipeline needs about the encryption step.
type bpEncryptionStepData struct {
	encrypting bool // We are actually encrypting the stream
	finalizer  ocicrypt.EncryptLayerFinalizer
}

// blobPipelineEncryptionStep updates *stream to encrypt if, it required by toEncrypt.
// srcInfo is primarily used for error messages.
// Returns data for other steps; the caller should eventually call updateCryptoOperationAndAnnotations.
func (ic *imageCopier) blobPipelineEncryptionStep(stream *sourceStream, toEncrypt bool, srcInfo types.BlobInfo,
	decryptionStep *bpDecryptionStepData) (*bpEncryptionStepData, error) {
	if !toEncrypt || isOciEncrypted(srcInfo.MediaType) || ic.c.options.OciEncryptConfig == nil {
		return &bpEncryptionStepData{
			encrypting: false,
		}, nil
	}

	if ic.cannotModifyManifestReason != "" {
		return nil, fmt.Errorf("layer %s should be encrypted, but we can’t modify the manifest: %s", srcInfo.Digest, ic.cannotModifyManifestReason)
	}

	var annotations map[string]string
	if !decryptionStep.decrypting {
		annotations = srcInfo.Annotations
	}
	desc := imgspecv1.Descriptor{
		MediaType:   srcInfo.MediaType,
		Digest:      srcInfo.Digest,
		Size:        srcInfo.Size,
		Annotations: annotations,
	}
	reader, finalizer, err := ocicrypt.EncryptLayer(ic.c.options.OciEncryptConfig, stream.reader, desc)
	if err != nil {
		return nil, fmt.Errorf("encrypting blob %s: %w", srcInfo.Digest, err)
	}

	stream.reader = reader
	stream.info.Digest = ""
	stream.info.Size = -1
	return &bpEncryptionStepData{
		encrypting: true,
		finalizer:  finalizer,
	}, nil
}

// updateCryptoOperationAndAnnotations sets *operation and updates *annotations, if necessary.
func (d *bpEncryptionStepData) updateCryptoOperationAndAnnotations(operation *types.LayerCrypto, annotations *map[string]string) error {
	if !d.encrypting {
		return nil
	}

	encryptAnnotations, err := d.finalizer()
	if err != nil {
		return fmt.Errorf("Unable to finalize encryption: %w", err)
	}
	*operation = types.Encrypt
	if *annotations == nil {
		*annotations = map[string]string{}
	}
	maps.Copy(*annotations, encryptAnnotations)
	return nil
}
