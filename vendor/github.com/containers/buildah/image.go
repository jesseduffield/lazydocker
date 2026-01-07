package buildah

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containers/buildah/copier"
	"github.com/containers/buildah/define"
	"github.com/containers/buildah/docker"
	"github.com/containers/buildah/internal/config"
	"github.com/containers/buildah/internal/mkcw"
	"github.com/containers/buildah/internal/tmpdir"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chrootarchive"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
)

const (
	// OCIv1ImageManifest is the MIME type of an OCIv1 image manifest,
	// suitable for specifying as a value of the PreferredManifestType
	// member of a CommitOptions structure.  It is also the default.
	OCIv1ImageManifest = define.OCIv1ImageManifest
	// Dockerv2ImageManifest is the MIME type of a Docker v2s2 image
	// manifest, suitable for specifying as a value of the
	// PreferredManifestType member of a CommitOptions structure.
	Dockerv2ImageManifest = define.Dockerv2ImageManifest
	// containerExcludesDir is the subdirectory of the container data
	// directory where we drop exclusions
	containerExcludesDir = "commit-excludes"
	// containerPulledUpDir is the subdirectory of the container
	// data directory where we drop exclusions when we're not squashing
	containerPulledUpDir = "commit-pulled-up"
	// containerExcludesSubstring is the suffix of files under
	// $cdir/containerExcludesDir and $cdir/containerPulledUpDir which
	// should be ignored, as they only exist because we use CreateTemp() to
	// create uniquely-named files, but we don't want to try to use their
	// contents until after they've been written to
	containerExcludesSubstring = ".tmp"
)

// ExtractRootfsOptions is consumed by ExtractRootfs() which allows users to
// control whether various information like the like setuid and setgid bits and
// xattrs are preserved when extracting file system objects.
type ExtractRootfsOptions struct {
	StripSetuidBit bool       // strip the setuid bit off of items being extracted.
	StripSetgidBit bool       // strip the setgid bit off of items being extracted.
	StripXattrs    bool       // don't record extended attributes of items being extracted.
	ForceTimestamp *time.Time // force timestamps in output content
}

type containerImageRef struct {
	fromImageName         string
	fromImageID           string
	store                 storage.Store
	compression           archive.Compression
	name                  reference.Named
	names                 []string
	containerID           string
	mountLabel            string
	layerID               string
	oconfig               []byte
	dconfig               []byte
	created               *time.Time
	createdBy             string
	layerModTime          *time.Time
	layerLatestModTime    *time.Time
	historyComment        string
	annotations           map[string]string
	preferredManifestType string
	squash                bool
	confidentialWorkload  ConfidentialWorkloadOptions
	omitHistory           bool
	emptyLayer            bool
	omitLayerHistoryEntry bool
	idMappingOptions      *define.IDMappingOptions
	parent                string
	blobDirectory         string
	preEmptyLayers        []v1.History
	preLayers             []commitLinkedLayerInfo
	postEmptyLayers       []v1.History
	postLayers            []commitLinkedLayerInfo
	overrideChanges       []string
	overrideConfig        *manifest.Schema2Config
	extraImageContent     map[string]string
	compatSetParent       types.OptionalBool
	layerExclusions       []copier.ConditionalRemovePath
	layerMountTargets     []copier.ConditionalRemovePath
	layerPullUps          []copier.EnsureParentPath
	unsetAnnotations      []string
	setAnnotations        []string
	createdAnnotation     types.OptionalBool
}

type blobLayerInfo struct {
	ID   string
	Size int64
}

type commitLinkedLayerInfo struct {
	layerID            string // more like layer "ID"
	linkedLayer        LinkedLayer
	uncompressedDigest digest.Digest
	size               int64
}

type containerImageSource struct {
	path          string
	ref           *containerImageRef
	store         storage.Store
	containerID   string
	mountLabel    string
	layerID       string
	names         []string
	compression   archive.Compression
	config        []byte
	configDigest  digest.Digest
	manifest      []byte
	manifestType  string
	blobDirectory string
	blobLayers    map[digest.Digest]blobLayerInfo
}

func (i *containerImageRef) NewImage(ctx context.Context, sc *types.SystemContext) (types.ImageCloser, error) {
	src, err := i.NewImageSource(ctx, sc)
	if err != nil {
		return nil, err
	}
	return image.FromSource(ctx, sc, src)
}

func expectedOCIDiffIDs(image v1.Image) int {
	expected := 0
	for _, history := range image.History {
		if !history.EmptyLayer {
			expected = expected + 1
		}
	}
	return expected
}

func expectedDockerDiffIDs(image docker.V2Image) int {
	expected := 0
	for _, history := range image.History {
		if !history.EmptyLayer {
			expected = expected + 1
		}
	}
	return expected
}

// Extract the container's whole filesystem as a filesystem image, wrapped
// in LUKS-compatible encryption.
func (i *containerImageRef) extractConfidentialWorkloadFS(options ConfidentialWorkloadOptions) (io.ReadCloser, error) {
	var image v1.Image
	if err := json.Unmarshal(i.oconfig, &image); err != nil {
		return nil, fmt.Errorf("recreating OCI configuration for %q: %w", i.containerID, err)
	}
	if options.TempDir == "" {
		cdir, err := i.store.ContainerDirectory(i.containerID)
		if err != nil {
			return nil, fmt.Errorf("getting the per-container data directory for %q: %w", i.containerID, err)
		}
		tempdir, err := os.MkdirTemp(cdir, "buildah-rootfs")
		if err != nil {
			return nil, fmt.Errorf("creating a temporary data directory to hold a rootfs image for %q: %w", i.containerID, err)
		}
		defer func() {
			if err := os.RemoveAll(tempdir); err != nil {
				logrus.Warnf("removing temporary directory %q: %v", tempdir, err)
			}
		}()
		options.TempDir = tempdir
	}
	mountPoint, err := i.store.Mount(i.containerID, i.mountLabel)
	if err != nil {
		return nil, fmt.Errorf("mounting container %q: %w", i.containerID, err)
	}
	archiveOptions := mkcw.ArchiveOptions{
		AttestationURL:           options.AttestationURL,
		CPUs:                     options.CPUs,
		Memory:                   options.Memory,
		TempDir:                  options.TempDir,
		TeeType:                  options.TeeType,
		IgnoreAttestationErrors:  options.IgnoreAttestationErrors,
		WorkloadID:               options.WorkloadID,
		DiskEncryptionPassphrase: options.DiskEncryptionPassphrase,
		Slop:                     options.Slop,
		FirmwareLibrary:          options.FirmwareLibrary,
		GraphOptions:             i.store.GraphOptions(),
		ExtraImageContent:        i.extraImageContent,
	}
	rc, _, err := mkcw.Archive(mountPoint, &image, archiveOptions)
	if err != nil {
		if _, err2 := i.store.Unmount(i.containerID, false); err2 != nil {
			logrus.Debugf("unmounting container %q: %v", i.containerID, err2)
		}
		return nil, fmt.Errorf("converting rootfs %q: %w", i.containerID, err)
	}
	return ioutils.NewReadCloserWrapper(rc, func() error {
		if err = rc.Close(); err != nil {
			err = fmt.Errorf("closing tar archive of container %q: %w", i.containerID, err)
		}
		if _, err2 := i.store.Unmount(i.containerID, false); err == nil {
			if err2 != nil {
				err2 = fmt.Errorf("unmounting container %q: %w", i.containerID, err2)
			}
			err = err2
		} else {
			logrus.Debugf("unmounting container %q: %v", i.containerID, err2)
		}
		return err
	}), nil
}

// Extract the container's whole filesystem as if it were a single layer.
// The ExtractRootfsOptions control whether or not to preserve setuid and
// setgid bits and extended attributes on contents.
func (i *containerImageRef) extractRootfs(opts ExtractRootfsOptions) (io.ReadCloser, chan error, error) {
	var uidMap, gidMap []idtools.IDMap
	mountPoint, err := i.store.Mount(i.containerID, i.mountLabel)
	if err != nil {
		return nil, nil, fmt.Errorf("mounting container %q: %w", i.containerID, err)
	}
	pipeReader, pipeWriter := io.Pipe()
	errChan := make(chan error, 1)
	go func() {
		defer pipeWriter.Close()
		defer close(errChan)
		if len(i.extraImageContent) > 0 {
			// Abuse the tar format and _prepend_ the synthesized
			// data items to the archive we'll get from
			// copier.Get(), in a way that looks right to a reader
			// as long as we DON'T Close() the tar Writer.
			filename, _, _, err := i.makeExtraImageContentDiff(false, opts.ForceTimestamp)
			if err != nil {
				errChan <- fmt.Errorf("creating part of archive with extra content: %w", err)
				return
			}
			file, err := os.Open(filename)
			if err != nil {
				errChan <- err
				return
			}
			defer file.Close()
			if _, err = io.Copy(pipeWriter, file); err != nil {
				errChan <- fmt.Errorf("writing contents of %q: %w", filename, err)
				return
			}
		}
		if i.idMappingOptions != nil {
			uidMap, gidMap = convertRuntimeIDMaps(i.idMappingOptions.UIDMap, i.idMappingOptions.GIDMap)
		}
		copierOptions := copier.GetOptions{
			UIDMap:         uidMap,
			GIDMap:         gidMap,
			StripSetuidBit: opts.StripSetuidBit,
			StripSetgidBit: opts.StripSetgidBit,
			StripXattrs:    opts.StripXattrs,
			Timestamp:      opts.ForceTimestamp,
		}
		err := copier.Get(mountPoint, mountPoint, copierOptions, []string{"."}, pipeWriter)
		errChan <- err
	}()
	return ioutils.NewReadCloserWrapper(pipeReader, func() error {
		if err = pipeReader.Close(); err != nil {
			err = fmt.Errorf("closing tar archive of container %q: %w", i.containerID, err)
		}
		if _, err2 := i.store.Unmount(i.containerID, false); err == nil {
			if err2 != nil {
				err2 = fmt.Errorf("unmounting container %q: %w", i.containerID, err2)
			}
			err = err2
		}
		return err
	}), errChan, nil
}

type manifestBuilder interface {
	// addLayer adds notes to the manifest and config about the layer.  The layer blobs are
	// identified by their possibly-compressed blob digests and sizes in the manifest, and by
	// their uncompressed digests (diffIDs) in the config.
	addLayer(layerBlobSum digest.Digest, layerBlobSize int64, diffID digest.Digest)
	computeLayerMIMEType(what string, layerCompression archive.Compression) error
	buildHistory(extraImageContentDiff string, extraImageContentDiffDigest digest.Digest) error
	manifestAndConfig() ([]byte, []byte, error)
}

type dockerSchema2ManifestBuilder struct {
	i              *containerImageRef
	layerMediaType string
	dimage         docker.V2Image
	dmanifest      docker.V2S2Manifest
}

// Build fresh copies of the container configuration structures so that we can edit them
// without making unintended changes to the original Builder (Docker schema 2).
func (i *containerImageRef) newDockerSchema2ManifestBuilder() (manifestBuilder, error) {
	created := time.Now().UTC()
	if i.created != nil {
		created = *i.created
	}

	// Build an empty image, and then decode over it.
	dimage := docker.V2Image{}
	if err := json.Unmarshal(i.dconfig, &dimage); err != nil {
		return nil, err
	}
	// Suppress the hostname and domainname if we're running with the
	// equivalent of either --timestamp or --source-date-epoch.
	if i.created != nil {
		dimage.Config.Hostname = "sandbox"
		dimage.Config.Domainname = ""
	}
	// Set the parent, but only if we want to be compatible with "classic" docker build.
	if i.compatSetParent == types.OptionalBoolTrue {
		dimage.Parent = docker.ID(i.parent)
	}
	// Set the container ID and containerConfig in the docker format.
	dimage.Container = i.containerID
	if i.created != nil {
		dimage.Container = ""
	}
	if dimage.Config != nil {
		dimage.ContainerConfig = *dimage.Config
	}
	// Always replace this value, since we're newer than our base image.
	dimage.Created = created
	// Clear the list of diffIDs, since we always repopulate it.
	dimage.RootFS = &docker.V2S2RootFS{}
	dimage.RootFS.Type = docker.TypeLayers
	dimage.RootFS.DiffIDs = []digest.Digest{}
	// Only clear the history if we're squashing, otherwise leave it be so
	// that we can append entries to it.  Clear the parent, too, to reflect
	// that we no longer include its layers and history.
	if i.confidentialWorkload.Convert || i.squash || i.omitHistory {
		dimage.Parent = ""
		dimage.History = []docker.V2S2History{}
	}

	// If we were supplied with a configuration, copy fields from it to
	// matching fields in both formats.
	if err := config.OverrideDocker(dimage.Config, i.overrideChanges, i.overrideConfig); err != nil {
		return nil, fmt.Errorf("applying changes: %w", err)
	}

	// If we're producing a confidential workload, override the command and
	// assorted other settings that aren't expected to work correctly.
	if i.confidentialWorkload.Convert {
		dimage.Config.Entrypoint = []string{"/entrypoint"}
		dimage.Config.Cmd = nil
		dimage.Config.User = ""
		dimage.Config.WorkingDir = ""
		dimage.Config.Healthcheck = nil
		dimage.Config.Shell = nil
		dimage.Config.Volumes = nil
		dimage.Config.ExposedPorts = nil
	}

	// Return partial manifest.  The Layers lists will be populated later.
	return &dockerSchema2ManifestBuilder{
		i:              i,
		layerMediaType: docker.V2S2MediaTypeUncompressedLayer,
		dimage:         dimage,
		dmanifest: docker.V2S2Manifest{
			V2Versioned: docker.V2Versioned{
				SchemaVersion: 2,
				MediaType:     manifest.DockerV2Schema2MediaType,
			},
			Config: docker.V2S2Descriptor{
				MediaType: manifest.DockerV2Schema2ConfigMediaType,
			},
			Layers: []docker.V2S2Descriptor{},
		},
	}, nil
}

func (mb *dockerSchema2ManifestBuilder) addLayer(layerBlobSum digest.Digest, layerBlobSize int64, diffID digest.Digest) {
	dlayerDescriptor := docker.V2S2Descriptor{
		MediaType: mb.layerMediaType,
		Digest:    layerBlobSum,
		Size:      layerBlobSize,
	}
	mb.dmanifest.Layers = append(mb.dmanifest.Layers, dlayerDescriptor)
	// Note this layer in the list of diffIDs, again using the uncompressed digest.
	mb.dimage.RootFS.DiffIDs = append(mb.dimage.RootFS.DiffIDs, diffID)
}

// Compute the media types which we need to attach to a layer, given the type of
// compression that we'll be applying.
func (mb *dockerSchema2ManifestBuilder) computeLayerMIMEType(what string, layerCompression archive.Compression) error {
	dmediaType := docker.V2S2MediaTypeUncompressedLayer
	if layerCompression != archive.Uncompressed {
		switch layerCompression {
		case archive.Gzip:
			dmediaType = manifest.DockerV2Schema2LayerMediaType
			logrus.Debugf("compressing %s with gzip", what)
		case archive.Bzip2:
			// Until the image specs define a media type for bzip2-compressed layers, even if we know
			// how to decompress them, we can't try to compress layers with bzip2.
			return errors.New("media type for bzip2-compressed layers is not defined")
		case archive.Xz:
			// Until the image specs define a media type for xz-compressed layers, even if we know
			// how to decompress them, we can't try to compress layers with xz.
			return errors.New("media type for xz-compressed layers is not defined")
		case archive.Zstd:
			// Until the image specs define a media type for zstd-compressed layers, even if we know
			// how to decompress them, we can't try to compress layers with zstd.
			return errors.New("media type for zstd-compressed layers is not defined")
		default:
			logrus.Debugf("compressing %s with unknown compressor(?)", what)
		}
	}
	mb.layerMediaType = dmediaType
	return nil
}

func (mb *dockerSchema2ManifestBuilder) buildHistory(extraImageContentDiff string, extraImageContentDiffDigest digest.Digest) error {
	// Build history notes in the image configuration.
	appendHistory := func(history []v1.History, empty bool) {
		for i := range history {
			var created time.Time
			if history[i].Created != nil {
				created = *history[i].Created
			}
			dnews := docker.V2S2History{
				Created:    created,
				CreatedBy:  history[i].CreatedBy,
				Author:     history[i].Author,
				Comment:    history[i].Comment,
				EmptyLayer: empty,
			}
			mb.dimage.History = append(mb.dimage.History, dnews)
		}
	}

	// Keep track of how many entries the base image's history had
	// before we started adding to it.
	baseImageHistoryLen := len(mb.dimage.History)

	// Add history entries for prepended empty layers.
	appendHistory(mb.i.preEmptyLayers, true)
	// Add history entries for prepended API-supplied layers.
	for _, h := range mb.i.preLayers {
		appendHistory([]v1.History{h.linkedLayer.History}, h.linkedLayer.History.EmptyLayer)
	}
	// Add a history entry for this layer, empty or not.
	created := time.Now().UTC()
	if mb.i.created != nil {
		created = (*mb.i.created).UTC()
	}
	if !mb.i.omitLayerHistoryEntry {
		dnews := docker.V2S2History{
			Created:    created,
			CreatedBy:  mb.i.createdBy,
			Author:     mb.dimage.Author,
			EmptyLayer: mb.i.emptyLayer,
			Comment:    mb.i.historyComment,
		}
		mb.dimage.History = append(mb.dimage.History, dnews)
	}
	// Add a history entry for the extra image content if we added a layer for it.
	// This diff was added to the list of layers before API-supplied layers that
	// needed to be appended, and we need to keep the order of history entries for
	// not-empty layers consistent with that.
	if extraImageContentDiff != "" {
		createdBy := fmt.Sprintf(`/bin/sh -c #(nop) ADD dir:%s in /",`, extraImageContentDiffDigest.Encoded())
		dnews := docker.V2S2History{
			Created:   created,
			CreatedBy: createdBy,
		}
		mb.dimage.History = append(mb.dimage.History, dnews)
	}
	// Add history entries for appended empty layers.
	appendHistory(mb.i.postEmptyLayers, true)
	// Add history entries for appended API-supplied layers.
	for _, h := range mb.i.postLayers {
		appendHistory([]v1.History{h.linkedLayer.History}, h.linkedLayer.History.EmptyLayer)
	}

	// Assemble a comment indicating which base image was used, if it
	// wasn't just an image ID, and add it to the first history entry we
	// added, if we indeed added one.
	if len(mb.dimage.History) > baseImageHistoryLen {
		var fromComment string
		if strings.Contains(mb.i.parent, mb.i.fromImageID) && mb.i.fromImageName != "" && !strings.HasPrefix(mb.i.fromImageID, mb.i.fromImageName) {
			if mb.dimage.History[baseImageHistoryLen].Comment != "" {
				fromComment = " "
			}
			fromComment += "FROM " + mb.i.fromImageName
		}
		mb.dimage.History[baseImageHistoryLen].Comment += fromComment
	}

	// Confidence check that we didn't just create a mismatch between non-empty layers in the
	// history and the number of diffIDs.  Only applicable if the base image (if there was
	// one) provided us at least one entry to use as a starting point.
	if baseImageHistoryLen != 0 {
		expectedDiffIDs := expectedDockerDiffIDs(mb.dimage)
		if len(mb.dimage.RootFS.DiffIDs) != expectedDiffIDs {
			return fmt.Errorf("internal error: history lists %d non-empty layers, but we have %d layers on disk", expectedDiffIDs, len(mb.dimage.RootFS.DiffIDs))
		}
	}
	return nil
}

func (mb *dockerSchema2ManifestBuilder) manifestAndConfig() ([]byte, []byte, error) {
	// Encode the image configuration blob.
	dconfig, err := json.Marshal(&mb.dimage)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding %#v as json: %w", mb.dimage, err)
	}
	logrus.Debugf("Docker v2s2 config = %s", dconfig)

	// Add the configuration blob to the manifest.
	mb.dmanifest.Config.Digest = digest.Canonical.FromBytes(dconfig)
	mb.dmanifest.Config.Size = int64(len(dconfig))
	mb.dmanifest.Config.MediaType = manifest.DockerV2Schema2ConfigMediaType

	// Encode the manifest.
	dmanifestbytes, err := json.Marshal(&mb.dmanifest)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding %#v as json: %w", mb.dmanifest, err)
	}
	logrus.Debugf("Docker v2s2 manifest = %s", dmanifestbytes)

	return dmanifestbytes, dconfig, nil
}

type ociManifestBuilder struct {
	i              *containerImageRef
	layerMediaType string
	oimage         v1.Image
	omanifest      v1.Manifest
}

// Build fresh copies of the container configuration structures so that we can edit them
// without making unintended changes to the original Builder (OCI manifest).
func (i *containerImageRef) newOCIManifestBuilder() (manifestBuilder, error) {
	created := time.Now().UTC()
	if i.created != nil {
		created = *i.created
	}

	// Build an empty image, and then decode over it.
	oimage := v1.Image{}
	if err := json.Unmarshal(i.oconfig, &oimage); err != nil {
		return nil, err
	}
	// Always replace this value, since we're newer than our base image.
	oimage.Created = &created
	// Clear the list of diffIDs, since we always repopulate it.
	oimage.RootFS.Type = docker.TypeLayers
	oimage.RootFS.DiffIDs = []digest.Digest{}
	// Only clear the history if we're squashing, otherwise leave it be so that we can append
	// entries to it.
	if i.confidentialWorkload.Convert || i.squash || i.omitHistory {
		oimage.History = []v1.History{}
	}

	// If we were supplied with a configuration, copy fields from it to
	// matching fields in both formats.
	if err := config.OverrideOCI(&oimage.Config, i.overrideChanges, i.overrideConfig); err != nil {
		return nil, fmt.Errorf("applying changes: %w", err)
	}

	// If we're producing a confidential workload, override the command and
	// assorted other settings that aren't expected to work correctly.
	if i.confidentialWorkload.Convert {
		oimage.Config.Entrypoint = []string{"/entrypoint"}
		oimage.Config.Cmd = nil
		oimage.Config.User = ""
		oimage.Config.WorkingDir = ""
		oimage.Config.Volumes = nil
		oimage.Config.ExposedPorts = nil
	}

	// Return partial manifest.  The Layers lists will be populated later.
	annotations := make(map[string]string)
	maps.Copy(annotations, i.annotations)
	switch i.createdAnnotation {
	case types.OptionalBoolFalse:
		delete(annotations, v1.AnnotationCreated)
	default:
		fallthrough
	case types.OptionalBoolTrue, types.OptionalBoolUndefined:
		annotations[v1.AnnotationCreated] = created.UTC().Format(time.RFC3339Nano)
	}
	for _, k := range i.unsetAnnotations {
		delete(annotations, k)
	}
	for _, kv := range i.setAnnotations {
		k, v, _ := strings.Cut(kv, "=")
		annotations[k] = v
	}
	return &ociManifestBuilder{
		i: i,
		// The default layer media type assumes no compression.
		layerMediaType: v1.MediaTypeImageLayer,
		oimage:         oimage,
		omanifest: v1.Manifest{
			Versioned: specs.Versioned{
				SchemaVersion: 2,
			},
			MediaType: v1.MediaTypeImageManifest,
			Config: v1.Descriptor{
				MediaType: v1.MediaTypeImageConfig,
			},
			Layers:      []v1.Descriptor{},
			Annotations: annotations,
		},
	}, nil
}

func (mb *ociManifestBuilder) addLayer(layerBlobSum digest.Digest, layerBlobSize int64, diffID digest.Digest) {
	olayerDescriptor := v1.Descriptor{
		MediaType: mb.layerMediaType,
		Digest:    layerBlobSum,
		Size:      layerBlobSize,
	}
	mb.omanifest.Layers = append(mb.omanifest.Layers, olayerDescriptor)
	// Note this layer in the list of diffIDs, again using the uncompressed digest.
	mb.oimage.RootFS.DiffIDs = append(mb.oimage.RootFS.DiffIDs, diffID)
}

// Compute the media types which we need to attach to a layer, given the type of
// compression that we'll be applying.
func (mb *ociManifestBuilder) computeLayerMIMEType(what string, layerCompression archive.Compression) error {
	omediaType := v1.MediaTypeImageLayer
	if layerCompression != archive.Uncompressed {
		switch layerCompression {
		case archive.Gzip:
			omediaType = v1.MediaTypeImageLayerGzip
			logrus.Debugf("compressing %s with gzip", what)
		case archive.Bzip2:
			// Until the image specs define a media type for bzip2-compressed layers, even if we know
			// how to decompress them, we can't try to compress layers with bzip2.
			return errors.New("media type for bzip2-compressed layers is not defined")
		case archive.Xz:
			// Until the image specs define a media type for xz-compressed layers, even if we know
			// how to decompress them, we can't try to compress layers with xz.
			return errors.New("media type for xz-compressed layers is not defined")
		case archive.Zstd:
			omediaType = v1.MediaTypeImageLayerZstd
			logrus.Debugf("compressing %s with zstd", what)
		default:
			logrus.Debugf("compressing %s with unknown compressor(?)", what)
		}
	}
	mb.layerMediaType = omediaType
	return nil
}

func (mb *ociManifestBuilder) buildHistory(extraImageContentDiff string, extraImageContentDiffDigest digest.Digest) error {
	// Build history notes in the image configuration.
	appendHistory := func(history []v1.History, empty bool) {
		for i := range history {
			var created *time.Time
			if history[i].Created != nil {
				copiedTimestamp := *history[i].Created
				created = &copiedTimestamp
			}
			onews := v1.History{
				Created:    created,
				CreatedBy:  history[i].CreatedBy,
				Author:     history[i].Author,
				Comment:    history[i].Comment,
				EmptyLayer: empty,
			}
			mb.oimage.History = append(mb.oimage.History, onews)
		}
	}

	// Keep track of how many entries the base image's history had
	// before we started adding to it.
	baseImageHistoryLen := len(mb.oimage.History)

	// Add history entries for prepended empty layers.
	appendHistory(mb.i.preEmptyLayers, true)
	// Add history entries for prepended API-supplied layers.
	for _, h := range mb.i.preLayers {
		appendHistory([]v1.History{h.linkedLayer.History}, h.linkedLayer.History.EmptyLayer)
	}
	// Add a history entry for this layer, empty or not.
	created := time.Now().UTC()
	if mb.i.created != nil {
		created = (*mb.i.created).UTC()
	}
	if !mb.i.omitLayerHistoryEntry {
		onews := v1.History{
			Created:    &created,
			CreatedBy:  mb.i.createdBy,
			Author:     mb.oimage.Author,
			EmptyLayer: mb.i.emptyLayer,
			Comment:    mb.i.historyComment,
		}
		mb.oimage.History = append(mb.oimage.History, onews)
	}
	// Add a history entry for the extra image content if we added a layer for it.
	// This diff was added to the list of layers before API-supplied layers that
	// needed to be appended, and we need to keep the order of history entries for
	// not-empty layers consistent with that.
	if extraImageContentDiff != "" {
		createdBy := fmt.Sprintf(`/bin/sh -c #(nop) ADD dir:%s in /",`, extraImageContentDiffDigest.Encoded())
		onews := v1.History{
			Created:   &created,
			CreatedBy: createdBy,
		}
		mb.oimage.History = append(mb.oimage.History, onews)
	}
	// Add history entries for appended empty layers.
	appendHistory(mb.i.postEmptyLayers, true)
	// Add history entries for appended API-supplied layers.
	for _, h := range mb.i.postLayers {
		appendHistory([]v1.History{h.linkedLayer.History}, h.linkedLayer.History.EmptyLayer)
	}

	// Assemble a comment indicating which base image was used, if it
	// wasn't just an image ID, and add it to the first history entry we
	// added, if we indeed added one.
	if len(mb.oimage.History) > baseImageHistoryLen {
		var fromComment string
		if strings.Contains(mb.i.parent, mb.i.fromImageID) && mb.i.fromImageName != "" && !strings.HasPrefix(mb.i.fromImageID, mb.i.fromImageName) {
			if mb.oimage.History[baseImageHistoryLen].Comment != "" {
				fromComment = " "
			}
			fromComment += "FROM " + mb.i.fromImageName
		}
		mb.oimage.History[baseImageHistoryLen].Comment += fromComment
	}

	// Confidence check that we didn't just create a mismatch between non-empty layers in the
	// history and the number of diffIDs.  Only applicable if the base image (if there was
	// one) provided us at least one entry to use as a starting point.
	if baseImageHistoryLen != 0 {
		expectedDiffIDs := expectedOCIDiffIDs(mb.oimage)
		if len(mb.oimage.RootFS.DiffIDs) != expectedDiffIDs {
			return fmt.Errorf("internal error: history lists %d non-empty layers, but we have %d layers on disk", expectedDiffIDs, len(mb.oimage.RootFS.DiffIDs))
		}
	}
	return nil
}

func (mb *ociManifestBuilder) manifestAndConfig() ([]byte, []byte, error) {
	// Encode the image configuration blob.
	oconfig, err := json.Marshal(&mb.oimage)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding %#v as json: %w", mb.oimage, err)
	}
	logrus.Debugf("OCIv1 config = %s", oconfig)

	// Add the configuration blob to the manifest.
	mb.omanifest.Config.Digest = digest.Canonical.FromBytes(oconfig)
	mb.omanifest.Config.Size = int64(len(oconfig))
	mb.omanifest.Config.MediaType = v1.MediaTypeImageConfig

	// Encode the manifest.
	omanifestbytes, err := json.Marshal(&mb.omanifest)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding %#v as json: %w", mb.omanifest, err)
	}
	logrus.Debugf("OCIv1 manifest = %s", omanifestbytes)

	return omanifestbytes, oconfig, nil
}

// filterExclusionsByImage returns a slice of the members of "exclusions" which are present in the image with the specified ID
func (i containerImageRef) filterExclusionsByImage(ctx context.Context, exclusions []copier.EnsureParentPath, imageID string) ([]copier.EnsureParentPath, error) {
	if len(exclusions) == 0 || imageID == "" {
		return nil, nil
	}
	var paths []copier.EnsureParentPath
	mountPoint, err := i.store.MountImage(imageID, nil, i.mountLabel)
	cleanup := func() {
		if _, err := i.store.UnmountImage(imageID, false); err != nil {
			logrus.Debugf("unmounting image %q: %v", imageID, err)
		}
	}
	if err != nil && errors.Is(err, storage.ErrLayerUnknown) {
		// if an imagestore is being used, this could be expected
		if b, err2 := NewBuilder(ctx, i.store, BuilderOptions{
			FromImage:       imageID,
			PullPolicy:      define.PullNever,
			ContainerSuffix: "tmp",
		}); err2 == nil {
			mountPoint, err = b.Mount(i.mountLabel)
			cleanup = func() {
				cid := b.ContainerID
				if err := b.Delete(); err != nil {
					logrus.Debugf("unmounting image %q as container %q: %v", imageID, cid, err)
				}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("mounting image %q to examine its contents: %w", imageID, err)
	}
	defer cleanup()
	globs := make([]string, 0, len(exclusions))
	for _, exclusion := range exclusions {
		globs = append(globs, exclusion.Path)
	}
	options := copier.StatOptions{}
	stats, err := copier.Stat(mountPoint, mountPoint, options, globs)
	if err != nil {
		return nil, fmt.Errorf("checking for potential exclusion items in image %q: %w", imageID, err)
	}
	for _, stat := range stats {
		for _, exclusion := range exclusions {
			if stat.Glob != exclusion.Path {
				continue
			}
			for result, stat := range stat.Results {
				if result != exclusion.Path {
					continue
				}
				if exclusion.ModTime != nil && !exclusion.ModTime.Equal(stat.ModTime) {
					continue
				}
				if exclusion.Mode != nil && *exclusion.Mode != stat.Mode {
					continue
				}
				if exclusion.Owner != nil && (int64(exclusion.Owner.UID) != stat.UID && int64(exclusion.Owner.GID) != stat.GID) {
					continue
				}
				paths = append(paths, exclusion)
			}
		}
	}
	return paths, nil
}

func (i *containerImageRef) NewImageSource(ctx context.Context, _ *types.SystemContext) (src types.ImageSource, err error) {
	// These maps will let us check if a layer ID is part of one group or another.
	parentLayerIDs := make(map[string]bool)
	apiLayerIDs := make(map[string]bool)
	// Start building the list of layers with any prepended layers.
	layers := []string{}
	for _, preLayer := range i.preLayers {
		layers = append(layers, preLayer.layerID)
		apiLayerIDs[preLayer.layerID] = true
	}
	// Now look at the read-write layer, and prepare to work our way back
	// through all of its parent layers.
	layerID := i.layerID
	layer, err := i.store.Layer(layerID)
	if err != nil {
		return nil, fmt.Errorf("unable to read layer %q: %w", layerID, err)
	}
	// Walk the list of parent layers, prepending each as we go.  If we're squashing
	// or making a confidential workload, we're only producing one layer, so stop at
	// the layer ID of the top layer, which we won't really be using anyway.
	for layer != nil {
		if layerID == i.layerID {
			// append the layer for this container to the list,
			// whether it's first or after some prepended layers
			layers = append(layers, layerID)
		} else {
			// prepend this parent layer to the list
			layers = append(append([]string{}, layerID), layers...)
			parentLayerIDs[layerID] = true
		}
		layerID = layer.Parent
		if layerID == "" || i.confidentialWorkload.Convert || i.squash {
			err = nil
			break
		}
		layer, err = i.store.Layer(layerID)
		if err != nil {
			return nil, fmt.Errorf("unable to read layer %q: %w", layerID, err)
		}
	}
	layer = nil

	// If we're slipping in a synthesized layer to hold some files, we need
	// to add a placeholder for it to the list just after the read-write
	// layer.  Confidential workloads and squashed images will just inline
	// the files, so we don't need to create a layer in those cases.
	const synthesizedLayerID = "(synthesized layer)"
	if len(i.extraImageContent) > 0 && !i.confidentialWorkload.Convert && !i.squash {
		layers = append(layers, synthesizedLayerID)
	}
	// Now add any API-supplied layers we have to append.
	for _, postLayer := range i.postLayers {
		layers = append(layers, postLayer.layerID)
		apiLayerIDs[postLayer.layerID] = true
	}
	logrus.Debugf("layer list: %q", layers)

	// It's simpler from here on to keep track of these as a group.
	apiLayers := append(slices.Clone(i.preLayers), slices.Clone(i.postLayers)...)

	// Make a temporary directory to hold blobs.
	path, err := os.MkdirTemp(tmpdir.GetTempDir(), define.Package)
	if err != nil {
		return nil, fmt.Errorf("creating temporary directory to hold layer blobs: %w", err)
	}
	logrus.Debugf("using %q to hold temporary data", path)
	defer func() {
		if src == nil {
			err2 := os.RemoveAll(path)
			if err2 != nil {
				logrus.Errorf("error removing layer blob directory: %v", err)
			}
		}
	}()

	// Build fresh copies of the configurations and manifest so that we don't mess with any
	// values in the Builder object itself.
	var mb manifestBuilder
	switch i.preferredManifestType {
	case v1.MediaTypeImageManifest:
		mb, err = i.newOCIManifestBuilder()
		if err != nil {
			return nil, err
		}
	case manifest.DockerV2Schema2MediaType:
		mb, err = i.newDockerSchema2ManifestBuilder()
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("no supported manifest types (attempted to use %q, only know %q and %q)",
			i.preferredManifestType, v1.MediaTypeImageManifest, manifest.DockerV2Schema2MediaType)
	}

	// Extract each layer and compute its digests, both compressed (if requested) and uncompressed.
	var extraImageContentDiff string
	var extraImageContentDiffDigest digest.Digest
	blobLayers := make(map[digest.Digest]blobLayerInfo)
	for _, layerID := range layers {
		what := fmt.Sprintf("layer %q", layerID)
		if i.confidentialWorkload.Convert || i.squash {
			what = fmt.Sprintf("container %q", i.containerID)
		}
		if layerID == synthesizedLayerID {
			what = synthesizedLayerID
		}
		if apiLayerIDs[layerID] {
			what = layerID
		}
		// Look up this layer.
		var layerUncompressedDigest digest.Digest
		var layerUncompressedSize int64
		linkedLayerHasLayerID := func(l commitLinkedLayerInfo) bool { return l.layerID == layerID }
		if apiLayerIDs[layerID] {
			// API-provided prepended or appended layer
			apiLayerIndex := slices.IndexFunc(apiLayers, linkedLayerHasLayerID)
			layerUncompressedDigest = apiLayers[apiLayerIndex].uncompressedDigest
			layerUncompressedSize = apiLayers[apiLayerIndex].size
		} else if layerID == synthesizedLayerID {
			// layer diff consisting of extra files to synthesize into a layer
			diffFilename, digest, size, err := i.makeExtraImageContentDiff(true, nil)
			if err != nil {
				return nil, fmt.Errorf("unable to generate layer for additional content: %w", err)
			}
			extraImageContentDiff = diffFilename
			extraImageContentDiffDigest = digest
			layerUncompressedDigest = digest
			layerUncompressedSize = size
		} else {
			// "normal" layer
			layer, err := i.store.Layer(layerID)
			if err != nil {
				return nil, fmt.Errorf("unable to locate layer %q: %w", layerID, err)
			}
			layerID = layer.ID
			layerUncompressedDigest = layer.UncompressedDigest
			layerUncompressedSize = layer.UncompressedSize
		}
		// We already know the digest of the contents of parent layers,
		// so if this is a parent layer, and we know its digest, reuse
		// its blobsum, diff ID, and size.
		if !i.confidentialWorkload.Convert && !i.squash && parentLayerIDs[layerID] && layerUncompressedDigest != "" {
			layerBlobSum := layerUncompressedDigest
			layerBlobSize := layerUncompressedSize
			diffID := layerUncompressedDigest
			// Note this layer in the manifest, using the appropriate blobsum.
			mb.addLayer(layerBlobSum, layerBlobSize, diffID)
			blobLayers[diffID] = blobLayerInfo{
				ID:   layerID,
				Size: layerBlobSize,
			}
			continue
		}
		// Figure out if we need to change the media type, in case we've changed the compression.
		if err := mb.computeLayerMIMEType(what, i.compression); err != nil {
			return nil, err
		}
		// Start reading either the layer or the whole container rootfs.
		noCompression := archive.Uncompressed
		diffOptions := &storage.DiffOptions{
			Compression: &noCompression,
		}
		var rc io.ReadCloser
		var errChan chan error
		var layerExclusions []copier.ConditionalRemovePath
		if i.confidentialWorkload.Convert {
			// Convert the root filesystem into an encrypted disk image.
			rc, err = i.extractConfidentialWorkloadFS(i.confidentialWorkload)
			if err != nil {
				return nil, err
			}
		} else if i.squash {
			// Extract the root filesystem as a single layer.
			rc, errChan, err = i.extractRootfs(ExtractRootfsOptions{})
			if err != nil {
				return nil, err
			}
		} else {
			if apiLayerIDs[layerID] {
				// We're reading an API-supplied blob.
				apiLayerIndex := slices.IndexFunc(apiLayers, linkedLayerHasLayerID)
				f, err := os.Open(apiLayers[apiLayerIndex].linkedLayer.BlobPath)
				if err != nil {
					return nil, fmt.Errorf("opening layer blob for %s: %w", layerID, err)
				}
				rc = f
			} else if layerID == synthesizedLayerID {
				// Slip in additional content as an additional layer.
				if rc, err = os.Open(extraImageContentDiff); err != nil {
					return nil, err
				}
			} else {
				// If we're up to the final layer, but we don't want to
				// include a diff for it, we're done.
				if i.emptyLayer && layerID == i.layerID {
					continue
				}
				if layerID == i.layerID {
					// We need to filter out any mount targets that we created.
					layerExclusions = append(slices.Clone(i.layerExclusions), i.layerMountTargets...)
					// And we _might_ need to filter out directories that modified
					// by creating and removing mount targets, _if_ they were the
					// same in the base image for this stage.
					layerPullUps, err := i.filterExclusionsByImage(ctx, i.layerPullUps, i.fromImageID)
					if err != nil {
						return nil, fmt.Errorf("checking which exclusions are in base image %q: %w", i.fromImageID, err)
					}
					layerExclusions = append(layerExclusions, layerPullUps...)
				}
				// Extract this layer, one of possibly many.
				rc, err = i.store.Diff("", layerID, diffOptions)
				if err != nil {
					return nil, fmt.Errorf("extracting %s: %w", what, err)
				}
			}
		}
		srcHasher := digest.Canonical.Digester()
		// Set up to write the possibly-recompressed blob.
		layerFile, err := os.OpenFile(filepath.Join(path, "layer"), os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			rc.Close()
			return nil, fmt.Errorf("opening file for %s: %w", what, err)
		}

		counter := ioutils.NewWriteCounter(layerFile)
		var destHasher digest.Digester
		var multiWriter io.Writer
		// Avoid rehashing when we compress or mess with the layer contents somehow.
		// At this point, there are multiple ways that can happen.
		diffBeingAltered := i.compression != archive.Uncompressed
		diffBeingAltered = diffBeingAltered || i.layerModTime != nil || i.layerLatestModTime != nil
		diffBeingAltered = diffBeingAltered || len(layerExclusions) != 0
		if diffBeingAltered {
			destHasher = digest.Canonical.Digester()
			multiWriter = io.MultiWriter(counter, destHasher.Hash())
		} else {
			destHasher = srcHasher
			multiWriter = counter
		}
		// Compress the layer, if we're recompressing it.
		writeCloser, err := archive.CompressStream(multiWriter, i.compression)
		if err != nil {
			layerFile.Close()
			rc.Close()
			return nil, fmt.Errorf("compressing %s: %w", what, err)
		}
		writer := io.MultiWriter(writeCloser, srcHasher.Hash())

		// Use specified timestamps in the layer, if we're doing that for history
		// entries.
		nestedWriteCloser := ioutils.NewWriteCloserWrapper(writer, writeCloser.Close)
		writeCloser = makeFilteredLayerWriteCloser(nestedWriteCloser, i.layerModTime, i.layerLatestModTime, layerExclusions)
		writer = writeCloser
		// Okay, copy from the raw diff through the filter, compressor, and counter and
		// digesters.
		size, err := io.Copy(writer, rc)
		if err != nil {
			writeCloser.Close()
			layerFile.Close()
			rc.Close()
			return nil, fmt.Errorf("storing %s to file: on copy: %w", what, err)
		}
		if err := writeCloser.Close(); err != nil {
			layerFile.Close()
			rc.Close()
			return nil, fmt.Errorf("storing %s to file: on pipe close: %w", what, err)
		}
		if err := layerFile.Close(); err != nil {
			rc.Close()
			return nil, fmt.Errorf("storing %s to file: on file close: %w", what, err)
		}
		rc.Close()

		if errChan != nil {
			err = <-errChan
			if err != nil {
				return nil, fmt.Errorf("extracting container rootfs: %w", err)
			}
		}

		if err != nil {
			return nil, fmt.Errorf("storing %s to file: %w", what, err)
		}
		if diffBeingAltered {
			size = counter.Count
		} else {
			if size != counter.Count {
				return nil, fmt.Errorf("storing %s to file: inconsistent layer size (copied %d, wrote %d)", what, size, counter.Count)
			}
		}
		logrus.Debugf("%s size is %d bytes, uncompressed digest %s, possibly-compressed digest %s", what, size, srcHasher.Digest().String(), destHasher.Digest().String())
		// Rename the layer so that we can more easily find it by digest later.
		finalBlobName := filepath.Join(path, destHasher.Digest().String())
		if err = os.Rename(filepath.Join(path, "layer"), finalBlobName); err != nil {
			return nil, fmt.Errorf("storing %s to file while renaming %q to %q: %w", what, filepath.Join(path, "layer"), finalBlobName, err)
		}
		mb.addLayer(destHasher.Digest(), size, srcHasher.Digest())
	}

	// Only attempt to append history if history was not disabled explicitly.
	if !i.omitHistory {
		if err := mb.buildHistory(extraImageContentDiff, extraImageContentDiffDigest); err != nil {
			return nil, err
		}
	}

	imageManifest, config, err := mb.manifestAndConfig()
	if err != nil {
		return nil, err
	}
	src = &containerImageSource{
		path:          path,
		ref:           i,
		store:         i.store,
		containerID:   i.containerID,
		mountLabel:    i.mountLabel,
		layerID:       i.layerID,
		names:         i.names,
		compression:   i.compression,
		config:        config,
		configDigest:  digest.Canonical.FromBytes(config),
		manifest:      imageManifest,
		manifestType:  i.preferredManifestType,
		blobDirectory: i.blobDirectory,
		blobLayers:    blobLayers,
	}
	return src, nil
}

func (i *containerImageRef) NewImageDestination(_ context.Context, _ *types.SystemContext) (types.ImageDestination, error) {
	return nil, errors.New("can't write to a container")
}

func (i *containerImageRef) DockerReference() reference.Named {
	return i.name
}

func (i *containerImageRef) StringWithinTransport() string {
	if len(i.names) > 0 {
		return i.names[0]
	}
	return ""
}

func (i *containerImageRef) DeleteImage(context.Context, *types.SystemContext) error {
	// we were never here
	return nil
}

func (i *containerImageRef) PolicyConfigurationIdentity() string {
	return ""
}

func (i *containerImageRef) PolicyConfigurationNamespaces() []string {
	return nil
}

func (i *containerImageRef) Transport() types.ImageTransport {
	return is.Transport
}

func (i *containerImageSource) Close() error {
	err := os.RemoveAll(i.path)
	if err != nil {
		return fmt.Errorf("removing layer blob directory: %w", err)
	}
	return nil
}

func (i *containerImageSource) Reference() types.ImageReference {
	return i.ref
}

func (i *containerImageSource) GetSignatures(_ context.Context, _ *digest.Digest) ([][]byte, error) {
	return nil, nil
}

func (i *containerImageSource) GetManifest(_ context.Context, _ *digest.Digest) ([]byte, string, error) {
	return i.manifest, i.manifestType, nil
}

func (i *containerImageSource) LayerInfosForCopy(_ context.Context, _ *digest.Digest) ([]types.BlobInfo, error) {
	return nil, nil
}

func (i *containerImageSource) HasThreadSafeGetBlob() bool {
	return false
}

func (i *containerImageSource) GetBlob(_ context.Context, blob types.BlobInfo, _ types.BlobInfoCache) (reader io.ReadCloser, size int64, err error) {
	if blob.Digest == i.configDigest {
		logrus.Debugf("start reading config")
		reader := bytes.NewReader(i.config)
		closer := func() error {
			logrus.Debugf("finished reading config")
			return nil
		}
		return ioutils.NewReadCloserWrapper(reader, closer), reader.Size(), nil
	}
	var layerReadCloser io.ReadCloser
	size = -1
	if blobLayerInfo, ok := i.blobLayers[blob.Digest]; ok {
		noCompression := archive.Uncompressed
		diffOptions := &storage.DiffOptions{
			Compression: &noCompression,
		}
		layerReadCloser, err = i.store.Diff("", blobLayerInfo.ID, diffOptions)
		size = blobLayerInfo.Size
	} else {
		for _, blobDir := range []string{i.blobDirectory, i.path} {
			var layerFile *os.File
			layerFile, err = os.OpenFile(filepath.Join(blobDir, blob.Digest.String()), os.O_RDONLY, 0o600)
			if err == nil {
				st, err := layerFile.Stat()
				if err != nil {
					logrus.Warnf("error reading size of layer file %q: %v", blob.Digest.String(), err)
				} else {
					size = st.Size()
					layerReadCloser = layerFile
					break
				}
				layerFile.Close()
			}
			if !errors.Is(err, os.ErrNotExist) {
				logrus.Debugf("error checking for layer %q in %q: %v", blob.Digest.String(), blobDir, err)
			}
		}
	}
	if err != nil || layerReadCloser == nil || size == -1 {
		logrus.Debugf("error reading layer %q: %v", blob.Digest.String(), err)
		return nil, -1, fmt.Errorf("opening layer blob: %w", err)
	}
	logrus.Debugf("reading layer %q", blob.Digest.String())
	closer := func() error {
		logrus.Debugf("finished reading layer %q", blob.Digest.String())
		if err := layerReadCloser.Close(); err != nil {
			return fmt.Errorf("closing layer %q after reading: %w", blob.Digest.String(), err)
		}
		return nil
	}
	return ioutils.NewReadCloserWrapper(layerReadCloser, closer), size, nil
}

// makeExtraImageContentDiff creates an archive file containing the contents of
// files named in i.extraImageContent.  The footer that marks the end of the
// archive may be omitted.
func (i *containerImageRef) makeExtraImageContentDiff(includeFooter bool, timestamp *time.Time) (_ string, _ digest.Digest, _ int64, retErr error) {
	cdir, err := i.store.ContainerDirectory(i.containerID)
	if err != nil {
		return "", "", -1, err
	}
	diff, err := os.CreateTemp(cdir, "extradiff")
	if err != nil {
		return "", "", -1, err
	}
	defer diff.Close()
	defer func() {
		if retErr != nil {
			os.Remove(diff.Name())
		}
	}()
	digester := digest.Canonical.Digester()
	counter := ioutils.NewWriteCounter(digester.Hash())
	tw := tar.NewWriter(io.MultiWriter(diff, counter))
	if timestamp == nil {
		now := time.Now()
		timestamp = &now
		if i.created != nil {
			timestamp = i.created
		}
	}
	for path, contents := range i.extraImageContent {
		if err := func() error {
			content, err := os.Open(contents)
			if err != nil {
				return err
			}
			defer content.Close()
			st, err := content.Stat()
			if err != nil {
				return err
			}
			if err := tw.WriteHeader(&tar.Header{
				Name:     path,
				Typeflag: tar.TypeReg,
				Mode:     0o644,
				ModTime:  *timestamp,
				Size:     st.Size(),
			}); err != nil {
				return fmt.Errorf("writing header for %q: %w", path, err)
			}
			if _, err := io.Copy(tw, content); err != nil {
				return fmt.Errorf("writing content for %q: %w", path, err)
			}
			if err := tw.Flush(); err != nil {
				return fmt.Errorf("flushing content for %q: %w", path, err)
			}
			return nil
		}(); err != nil {
			return "", "", -1, fmt.Errorf("writing %q to prepend-to-archive blob: %w", contents, err)
		}
	}
	if includeFooter {
		if err = tw.Close(); err != nil {
			return "", "", -1, fmt.Errorf("closingprepend-to-archive blob after final write: %w", err)
		}
	} else {
		if err = tw.Flush(); err != nil {
			return "", "", -1, fmt.Errorf("flushing prepend-to-archive blob after final write: %w", err)
		}
	}
	return diff.Name(), digester.Digest(), counter.Count, nil
}

// makeFilteredLayerWriteCloser returns either the passed-in WriteCloser, or if
// layerModeTime or layerLatestModTime are set, a WriteCloser which modifies
// the tarball that's written to it so that timestamps in headers are set to
// layerModTime exactly (if a value is provided for it), and then clamped to be
// no later than layerLatestModTime (if a value is provided for it).
// This implies that if both values are provided, the archive's timestamps will
// be set to the earlier of the two values.
func makeFilteredLayerWriteCloser(wc io.WriteCloser, layerModTime, layerLatestModTime *time.Time, exclusions []copier.ConditionalRemovePath) io.WriteCloser {
	if layerModTime == nil && layerLatestModTime == nil && len(exclusions) == 0 {
		return wc
	}
	exclusionsMap := make(map[string]copier.ConditionalRemovePath)
	for _, exclusionSpec := range exclusions {
		pathSpec := strings.Trim(path.Clean(exclusionSpec.Path), "/")
		if pathSpec == "" {
			continue
		}
		exclusionsMap[pathSpec] = exclusionSpec
	}
	wc = newTarFilterer(wc, func(hdr *tar.Header) (skip, replaceContents bool, replacementContents io.Reader) {
		// Changing a zeroed field to a non-zero field can affect the
		// format that the library uses for writing the header, so only
		// change fields that are already set to avoid changing the
		// format (and as a result, changing the length) of the header
		// that we write.
		modTime := hdr.ModTime
		nameSpec := strings.Trim(path.Clean(hdr.Name), "/")
		if conditions, ok := exclusionsMap[nameSpec]; ok {
			if (conditions.ModTime == nil || conditions.ModTime.Equal(modTime)) &&
				(conditions.Owner == nil || (conditions.Owner.UID == hdr.Uid && conditions.Owner.GID == hdr.Gid)) &&
				(conditions.Mode == nil || (*conditions.Mode&os.ModePerm == os.FileMode(hdr.Mode)&os.ModePerm)) {
				return true, false, nil
			}
		}
		if layerModTime != nil {
			modTime = *layerModTime
		}
		if layerLatestModTime != nil && layerLatestModTime.Before(modTime) {
			modTime = *layerLatestModTime
		}
		if !hdr.ModTime.IsZero() {
			hdr.ModTime = modTime
		}
		if !hdr.AccessTime.IsZero() {
			hdr.AccessTime = modTime
		}
		if !hdr.ChangeTime.IsZero() {
			hdr.ChangeTime = modTime
		}
		return false, false, nil
	})
	return wc
}

// makeLinkedLayerInfos calculates the size and digest information for a layer
// we intend to add to the image that we're committing.
func (b *Builder) makeLinkedLayerInfos(layers []LinkedLayer, layerType string, layerModTime, layerLatestModTime *time.Time) ([]commitLinkedLayerInfo, error) {
	if layers == nil {
		return nil, nil
	}
	infos := make([]commitLinkedLayerInfo, 0, len(layers))
	for i, layer := range layers {
		// complain if EmptyLayer and "is the BlobPath empty" don't agree
		if layer.History.EmptyLayer != (layer.BlobPath == "") {
			return nil, fmt.Errorf("internal error: layer-is-empty = %v, but content path is %q", layer.History.EmptyLayer, layer.BlobPath)
		}
		// if there's no layer contents, we're done with this one
		if layer.History.EmptyLayer {
			continue
		}
		// check if it's a directory or a non-directory
		st, err := os.Stat(layer.BlobPath)
		if err != nil {
			return nil, fmt.Errorf("checking if layer content %s is a directory: %w", layer.BlobPath, err)
		}
		info := commitLinkedLayerInfo{
			layerID:     fmt.Sprintf("(%s %d)", layerType, i+1),
			linkedLayer: layer,
		}
		if err = func() error {
			cdir, err := b.store.ContainerDirectory(b.ContainerID)
			if err != nil {
				return fmt.Errorf("determining directory for working container: %w", err)
			}
			f, err := os.CreateTemp(cdir, "")
			if err != nil {
				return fmt.Errorf("creating temporary file to hold blob for %q: %w", info.linkedLayer.BlobPath, err)
			}
			defer f.Close()
			var rc io.ReadCloser
			var what string
			if st.IsDir() {
				// if it's a directory, archive it and digest the archive while we're storing a copy somewhere
				what = "directory"
				rc, err = chrootarchive.Tar(info.linkedLayer.BlobPath, nil, info.linkedLayer.BlobPath)
				if err != nil {
					return fmt.Errorf("generating a layer blob from %q: %w", info.linkedLayer.BlobPath, err)
				}
			} else {
				what = "file"
				// if it's not a directory, just digest it while we're storing a copy somewhere
				rc, err = os.Open(info.linkedLayer.BlobPath)
				if err != nil {
					return err
				}
			}

			digester := digest.Canonical.Digester()
			sizeCountedFile := ioutils.NewWriteCounter(io.MultiWriter(digester.Hash(), f))
			wc := makeFilteredLayerWriteCloser(ioutils.NopWriteCloser(sizeCountedFile), layerModTime, layerLatestModTime, nil)
			_, copyErr := io.Copy(wc, rc)
			wcErr := wc.Close()
			if err := rc.Close(); err != nil {
				return fmt.Errorf("storing a copy of %s %q: closing reader: %w", what, info.linkedLayer.BlobPath, err)
			}
			if copyErr != nil {
				return fmt.Errorf("storing a copy of %s %q: copying data: %w", what, info.linkedLayer.BlobPath, copyErr)
			}
			if wcErr != nil {
				return fmt.Errorf("storing a copy of %s %q: closing writer: %w", what, info.linkedLayer.BlobPath, wcErr)
			}
			info.uncompressedDigest = digester.Digest()
			info.size = sizeCountedFile.Count
			info.linkedLayer.BlobPath = f.Name()
			return nil
		}(); err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// makeContainerImageRef creates a containers/image/v5/types.ImageReference
// which is mainly used for representing the working container as a source
// image that can be copied, which is how we commit the container to create the
// image.
func (b *Builder) makeContainerImageRef(options CommitOptions) (*containerImageRef, error) {
	if (len(options.PrependedLinkedLayers) > 0 || len(options.AppendedLinkedLayers) > 0) &&
		(options.ConfidentialWorkloadOptions.Convert || options.Squash) {
		return nil, errors.New("can't add prebuilt layers and produce an image with only one layer, at the same time")
	}
	var name reference.Named
	container, err := b.store.Container(b.ContainerID)
	if err != nil {
		return nil, fmt.Errorf("locating container %q: %w", b.ContainerID, err)
	}
	if len(container.Names) > 0 {
		if parsed, err2 := reference.ParseNamed(container.Names[0]); err2 == nil {
			name = parsed
		}
	}

	cdir, err := b.store.ContainerDirectory(b.ContainerID)
	if err != nil {
		return nil, fmt.Errorf("getting the per-container data directory for %q: %w", b.ContainerID, err)
	}

	gatherExclusions := func(excludesFiles []string) ([]copier.ConditionalRemovePath, error) {
		var excludes []copier.ConditionalRemovePath
		for _, excludesFile := range excludesFiles {
			if strings.Contains(excludesFile, containerExcludesSubstring) {
				continue
			}
			excludesData, err := os.ReadFile(excludesFile)
			if err != nil {
				return nil, fmt.Errorf("reading commit exclusions for %q: %w", b.ContainerID, err)
			}
			var theseExcludes []copier.ConditionalRemovePath
			if err := json.Unmarshal(excludesData, &theseExcludes); err != nil {
				return nil, fmt.Errorf("parsing commit exclusions for %q: %w", b.ContainerID, err)
			}
			excludes = append(excludes, theseExcludes...)
		}
		return excludes, nil
	}
	mountTargetFiles, err := filepath.Glob(filepath.Join(cdir, containerExcludesDir, "*"))
	if err != nil {
		return nil, fmt.Errorf("checking for commit exclusions for %q: %w", b.ContainerID, err)
	}
	pulledUpFiles, err := filepath.Glob(filepath.Join(cdir, containerPulledUpDir, "*"))
	if err != nil {
		return nil, fmt.Errorf("checking for commit pulled-up items for %q: %w", b.ContainerID, err)
	}
	layerMountTargets, err := gatherExclusions(mountTargetFiles)
	if err != nil {
		return nil, err
	}
	if len(layerMountTargets) > 0 {
		logrus.Debugf("these items were created for use as mount targets: %#v", layerMountTargets)
	}
	layerPullUps, err := gatherExclusions(pulledUpFiles)
	if err != nil {
		return nil, err
	}
	if len(layerPullUps) > 0 {
		logrus.Debugf("these items appear to have been pulled up: %#v", layerPullUps)
	}
	var layerExclusions []copier.ConditionalRemovePath
	if options.CompatLayerOmissions == types.OptionalBoolTrue {
		layerExclusions = slices.Clone(compatLayerExclusions)
	}
	if len(layerExclusions) > 0 {
		logrus.Debugf("excluding these items from committed layer: %#v", layerExclusions)
	}

	manifestType := options.PreferredManifestType
	if manifestType == "" {
		manifestType = define.OCIv1ImageManifest
	}

	for _, u := range options.UnsetEnvs {
		b.UnsetEnv(u)
	}
	oconfig, err := json.Marshal(&b.OCIv1)
	if err != nil {
		return nil, fmt.Errorf("encoding OCI-format image configuration %#v: %w", b.OCIv1, err)
	}
	dconfig, err := json.Marshal(&b.Docker)
	if err != nil {
		return nil, fmt.Errorf("encoding docker-format image configuration %#v: %w", b.Docker, err)
	}
	var created, layerModTime, layerLatestModTime *time.Time
	if options.HistoryTimestamp != nil {
		historyTimestampUTC := options.HistoryTimestamp.UTC()
		created = &historyTimestampUTC
		layerModTime = &historyTimestampUTC
	}
	if options.SourceDateEpoch != nil {
		sourceDateEpochUTC := options.SourceDateEpoch.UTC()
		created = &sourceDateEpochUTC
		if options.RewriteTimestamp {
			layerLatestModTime = &sourceDateEpochUTC
		}
	}
	createdBy := b.CreatedBy()
	if createdBy == "" {
		createdBy = strings.Join(b.Shell(), " ")
		if createdBy == "" {
			createdBy = "/bin/sh"
		}
	}

	parent := ""
	forceOmitHistory := false
	if b.FromImageID != "" {
		parentDigest := digest.NewDigestFromEncoded(digest.Canonical, b.FromImageID)
		if parentDigest.Validate() == nil {
			parent = parentDigest.String()
		}
		if !options.OmitHistory && len(b.OCIv1.History) == 0 && len(b.OCIv1.RootFS.DiffIDs) != 0 {
			// Parent had layers, but no history.  We shouldn't confuse
			// our own confidence checks by adding history for layers
			// that we're adding, creating an image with multiple layers,
			// only some of which have history entries, which would be
			// broken in confusing ways.
			b.Logger.Debugf("parent image %q had no history but had %d layers, assuming OmitHistory", b.FromImageID, len(b.OCIv1.RootFS.DiffIDs))
			forceOmitHistory = true
		}
	}

	preLayerInfos, err := b.makeLinkedLayerInfos(append(slices.Clone(b.PrependedLinkedLayers), slices.Clone(options.PrependedLinkedLayers)...), "prepended layer", layerModTime, layerLatestModTime)
	if err != nil {
		return nil, err
	}
	postLayerInfos, err := b.makeLinkedLayerInfos(append(slices.Clone(options.AppendedLinkedLayers), slices.Clone(b.AppendedLinkedLayers)...), "appended layer", layerModTime, layerLatestModTime)
	if err != nil {
		return nil, err
	}

	ref := &containerImageRef{
		fromImageName:         b.FromImage,
		fromImageID:           b.FromImageID,
		store:                 b.store,
		compression:           options.Compression,
		name:                  name,
		names:                 container.Names,
		containerID:           container.ID,
		mountLabel:            b.MountLabel,
		layerID:               container.LayerID,
		oconfig:               oconfig,
		dconfig:               dconfig,
		created:               created,
		createdBy:             createdBy,
		layerModTime:          layerModTime,
		layerLatestModTime:    layerLatestModTime,
		historyComment:        b.HistoryComment(),
		annotations:           b.Annotations(),
		setAnnotations:        slices.Clone(options.Annotations),
		unsetAnnotations:      slices.Clone(options.UnsetAnnotations),
		preferredManifestType: manifestType,
		squash:                options.Squash,
		confidentialWorkload:  options.ConfidentialWorkloadOptions,
		omitHistory:           options.OmitHistory || forceOmitHistory,
		emptyLayer:            (options.EmptyLayer || options.OmitLayerHistoryEntry) && !options.Squash && !options.ConfidentialWorkloadOptions.Convert,
		omitLayerHistoryEntry: options.OmitLayerHistoryEntry && !options.Squash && !options.ConfidentialWorkloadOptions.Convert,
		idMappingOptions:      &b.IDMappingOptions,
		parent:                parent,
		blobDirectory:         options.BlobDirectory,
		preEmptyLayers:        slices.Clone(b.PrependedEmptyLayers),
		preLayers:             preLayerInfos,
		postEmptyLayers:       slices.Clone(b.AppendedEmptyLayers),
		postLayers:            postLayerInfos,
		overrideChanges:       options.OverrideChanges,
		overrideConfig:        options.OverrideConfig,
		extraImageContent:     maps.Clone(options.ExtraImageContent),
		compatSetParent:       options.CompatSetParent,
		layerExclusions:       layerExclusions,
		layerMountTargets:     layerMountTargets,
		layerPullUps:          layerPullUps,
		createdAnnotation:     options.CreatedAnnotation,
	}
	if ref.created != nil {
		for i := range ref.preEmptyLayers {
			ref.preEmptyLayers[i].Created = ref.created
		}
		for i := range ref.preLayers {
			ref.preLayers[i].linkedLayer.History.Created = ref.created
		}
		for i := range ref.postEmptyLayers {
			ref.postEmptyLayers[i].Created = ref.created
		}
		for i := range ref.postLayers {
			ref.postLayers[i].linkedLayer.History.Created = ref.created
		}
	}
	return ref, nil
}

// Extract the container's whole filesystem as if it were a single layer from current builder instance
func (b *Builder) ExtractRootfs(options CommitOptions, opts ExtractRootfsOptions) (io.ReadCloser, chan error, error) {
	src, err := b.makeContainerImageRef(options)
	if err != nil {
		return nil, nil, fmt.Errorf("creating image reference for container %q to extract its contents: %w", b.ContainerID, err)
	}
	return src.extractRootfs(opts)
}
