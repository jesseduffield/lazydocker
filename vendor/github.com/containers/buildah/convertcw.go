package buildah

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/containers/buildah/define"
	"github.com/containers/buildah/internal/mkcw"
	encconfig "github.com/containers/ocicrypt/config"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/archive"
)

// CWConvertImageOptions provides both required and optional bits of
// configuration for CWConvertImage().
type CWConvertImageOptions struct {
	// Required parameters.
	InputImage string

	// If supplied, we'll tag the resulting image with the specified name.
	Tag         string
	OutputImage types.ImageReference

	// If supplied, we'll register the workload with this server.
	// Practically necessary if DiskEncryptionPassphrase is not set, in
	// which case we'll generate one and throw it away after.
	AttestationURL string

	// Used to measure the environment.  If left unset (0), defaults will be applied.
	CPUs   int
	Memory int

	// Can be manually set.  If left unset ("", false, nil), reasonable values will be used.
	TeeType                  define.TeeType
	IgnoreAttestationErrors  bool
	WorkloadID               string
	DiskEncryptionPassphrase string
	Slop                     string
	FirmwareLibrary          string
	BaseImage                string
	Logger                   *logrus.Logger
	ExtraImageContent        map[string]string

	// Passed through to BuilderOptions. Most settings won't make
	// sense to be made available here because we don't launch a process.
	ContainerSuffix     string
	PullPolicy          PullPolicy
	BlobDirectory       string
	SignaturePolicyPath string
	ReportWriter        io.Writer
	IDMappingOptions    *IDMappingOptions
	Format              string
	MaxPullRetries      int
	PullRetryDelay      time.Duration
	OciDecryptConfig    *encconfig.DecryptConfig
	MountLabel          string
}

// CWConvertImage takes the rootfs and configuration from one image, generates a
// LUKS-encrypted disk image that more or less includes them both, and puts the
// result into a new container image.
// Returns the new image's ID and digest on success, along with a canonical
// reference for it if a repository name was specified.
func CWConvertImage(ctx context.Context, systemContext *types.SystemContext, store storage.Store, options CWConvertImageOptions) (string, reference.Canonical, digest.Digest, error) {
	// Apply our defaults if some options aren't set.
	logger := options.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	// Now create the target working container, pulling the base image if
	// there is one and it isn't present.
	builderOptions := BuilderOptions{
		FromImage:     options.BaseImage,
		SystemContext: systemContext,
		Logger:        logger,

		ContainerSuffix:     options.ContainerSuffix,
		PullPolicy:          options.PullPolicy,
		BlobDirectory:       options.BlobDirectory,
		SignaturePolicyPath: options.SignaturePolicyPath,
		ReportWriter:        options.ReportWriter,
		IDMappingOptions:    options.IDMappingOptions,
		Format:              options.Format,
		MaxPullRetries:      options.MaxPullRetries,
		PullRetryDelay:      options.PullRetryDelay,
		OciDecryptConfig:    options.OciDecryptConfig,
		MountLabel:          options.MountLabel,
	}
	target, err := NewBuilder(ctx, store, builderOptions)
	if err != nil {
		return "", nil, "", fmt.Errorf("creating container from target image: %w", err)
	}
	defer func() {
		if err := target.Delete(); err != nil {
			logrus.Warnf("deleting target container: %v", err)
		}
	}()
	targetDir, err := target.Mount("")
	if err != nil {
		return "", nil, "", fmt.Errorf("mounting target container: %w", err)
	}
	defer func() {
		if err := target.Unmount(); err != nil {
			logrus.Warnf("unmounting target container: %v", err)
		}
	}()

	// Mount the source image, pulling it first if necessary.
	builderOptions = BuilderOptions{
		FromImage:     options.InputImage,
		SystemContext: systemContext,
		Logger:        logger,

		ContainerSuffix:     options.ContainerSuffix,
		PullPolicy:          options.PullPolicy,
		BlobDirectory:       options.BlobDirectory,
		SignaturePolicyPath: options.SignaturePolicyPath,
		ReportWriter:        options.ReportWriter,
		IDMappingOptions:    options.IDMappingOptions,
		Format:              options.Format,
		MaxPullRetries:      options.MaxPullRetries,
		PullRetryDelay:      options.PullRetryDelay,
		OciDecryptConfig:    options.OciDecryptConfig,
		MountLabel:          options.MountLabel,
	}
	source, err := NewBuilder(ctx, store, builderOptions)
	if err != nil {
		return "", nil, "", fmt.Errorf("creating container from source image: %w", err)
	}
	defer func() {
		if err := source.Delete(); err != nil {
			logrus.Warnf("deleting source container: %v", err)
		}
	}()
	sourceInfo := GetBuildInfo(source)
	if err != nil {
		return "", nil, "", fmt.Errorf("retrieving info about source image: %w", err)
	}
	sourceImageID := sourceInfo.FromImageID
	sourceSize, err := store.ImageSize(sourceImageID)
	if err != nil {
		return "", nil, "", fmt.Errorf("computing size of source image: %w", err)
	}
	sourceDir, err := source.Mount("")
	if err != nil {
		return "", nil, "", fmt.Errorf("mounting source container: %w", err)
	}
	defer func() {
		if err := source.Unmount(); err != nil {
			logrus.Warnf("unmounting source container: %v", err)
		}
	}()

	// Generate the image contents.
	archiveOptions := mkcw.ArchiveOptions{
		AttestationURL:           options.AttestationURL,
		CPUs:                     options.CPUs,
		Memory:                   options.Memory,
		TempDir:                  targetDir,
		TeeType:                  options.TeeType,
		IgnoreAttestationErrors:  options.IgnoreAttestationErrors,
		ImageSize:                sourceSize,
		WorkloadID:               options.WorkloadID,
		DiskEncryptionPassphrase: options.DiskEncryptionPassphrase,
		Slop:                     options.Slop,
		FirmwareLibrary:          options.FirmwareLibrary,
		Logger:                   logger,
		GraphOptions:             store.GraphOptions(),
		ExtraImageContent:        options.ExtraImageContent,
	}
	rc, workloadConfig, err := mkcw.Archive(sourceDir, &source.OCIv1, archiveOptions)
	if err != nil {
		return "", nil, "", fmt.Errorf("generating encrypted image content: %w", err)
	}
	if err = archive.Untar(rc, targetDir, &archive.TarOptions{}); err != nil {
		if err = rc.Close(); err != nil {
			logger.Warnf("cleaning up: %v", err)
		}
		return "", nil, "", fmt.Errorf("saving encrypted image content: %w", err)
	}
	if err = rc.Close(); err != nil {
		return "", nil, "", fmt.Errorf("cleaning up: %w", err)
	}

	// Commit the image.  Clear out most of the configuration (if there is any — we default
	// to scratch as a base) so that an engine that doesn't or can't set up a TEE will just
	// run the static entrypoint.  The rest of the configuration which the runtime consults
	// is in the .krun_config.json file in the encrypted filesystem.
	logger.Log(logrus.DebugLevel, "committing disk image")
	target.ClearAnnotations()
	target.ClearEnv()
	target.ClearLabels()
	target.ClearOnBuild()
	target.ClearPorts()
	target.ClearVolumes()
	target.SetCmd(nil)
	target.SetCreatedBy(fmt.Sprintf(": convert %q for use with %q", sourceImageID, workloadConfig.Type))
	target.SetDomainname("")
	target.SetEntrypoint([]string{"/entrypoint"})
	target.SetHealthcheck(nil)
	target.SetHostname("")
	target.SetMaintainer("")
	target.SetShell(nil)
	target.SetUser("")
	target.SetWorkDir("")
	commitOptions := CommitOptions{
		SystemContext: systemContext,
	}
	if options.Tag != "" {
		commitOptions.AdditionalTags = append(commitOptions.AdditionalTags, options.Tag)
	}
	return target.Commit(ctx, options.OutputImage, commitOptions)
}
