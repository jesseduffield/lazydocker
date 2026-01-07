package mkcw

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/containers/buildah/internal/tmpdir"
	"github.com/containers/buildah/pkg/overlay"
	"github.com/containers/luksy"
	"github.com/docker/go-units"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/system"
)

const minimumImageSize = 10 * 1024 * 1024

// ArchiveOptions includes optional settings for generating an archive.
type ArchiveOptions struct {
	// If supplied, we'll register the workload with this server.
	// Practically necessary if DiskEncryptionPassphrase is not set, in
	// which case we'll generate one and throw it away after.
	AttestationURL string

	// Used to measure the environment.  If left unset (0, ""), defaults will be applied.
	CPUs   int
	Memory int

	// Can be manually set.  If left unset ("", false, nil), reasonable values will be used.
	TempDir                  string
	TeeType                  TeeType
	IgnoreAttestationErrors  bool
	ImageSize                int64
	WorkloadID               string
	Slop                     string
	DiskEncryptionPassphrase string
	FirmwareLibrary          string
	Logger                   *logrus.Logger
	GraphOptions             []string // passed in from a storage Store, probably
	ExtraImageContent        map[string]string
}

type chainRetrievalError struct {
	stderr string
	err    error
}

func (c chainRetrievalError) Error() string {
	if trimmed := strings.TrimSpace(c.stderr); trimmed != "" {
		return fmt.Sprintf("retrieving SEV certificate chain: sevctl: %v: %v", strings.TrimSpace(c.stderr), c.err)
	}
	return fmt.Sprintf("retrieving SEV certificate chain: sevctl: %v", c.err)
}

// Archive generates a WorkloadConfig for a specified directory and produces a
// tar archive of a container image's rootfs with the expected contents.
func Archive(rootfsPath string, ociConfig *v1.Image, options ArchiveOptions) (io.ReadCloser, WorkloadConfig, error) {
	const (
		teeDefaultCPUs       = 2
		teeDefaultMemory     = 512
		teeDefaultFilesystem = "ext4"
		teeDefaultTeeType    = SNP
	)

	if rootfsPath == "" {
		return nil, WorkloadConfig{}, fmt.Errorf("required path not specified")
	}
	logger := options.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	teeType := options.TeeType
	if teeType == "" {
		teeType = teeDefaultTeeType
	}
	cpus := options.CPUs
	if cpus == 0 {
		cpus = teeDefaultCPUs
	}
	memory := options.Memory
	if memory == 0 {
		memory = teeDefaultMemory
	}
	filesystem := teeDefaultFilesystem
	workloadID := options.WorkloadID
	if workloadID == "" {
		digestInput := rootfsPath + filesystem + time.Now().String()
		workloadID = digest.Canonical.FromString(digestInput).Encoded()
	}
	workloadConfig := WorkloadConfig{
		Type:           teeType,
		WorkloadID:     workloadID,
		CPUs:           cpus,
		Memory:         memory,
		AttestationURL: options.AttestationURL,
	}
	if options.TempDir == "" {
		options.TempDir = tmpdir.GetTempDir()
	}

	// Do things which are specific to the type of TEE we're building for.
	var chainBytes []byte
	var chainBytesFile string
	var chainInfo fs.FileInfo
	switch teeType {
	default:
		return nil, WorkloadConfig{}, fmt.Errorf("don't know how to generate TeeData for TEE type %q", teeType)
	case SEV, SEV_NO_ES:
		// If we need a certificate chain, get it.
		chain, err := os.CreateTemp(options.TempDir, "chain")
		if err != nil {
			return nil, WorkloadConfig{}, err
		}
		chain.Close()
		defer func() {
			if err := os.Remove(chain.Name()); err != nil {
				logger.Warnf("error removing temporary file %q: %v", chain.Name(), err)
			}
		}()
		logrus.Debugf("sevctl export -f %s", chain.Name())
		cmd := exec.Command("sevctl", "export", "-f", chain.Name())
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			if !options.IgnoreAttestationErrors {
				return nil, WorkloadConfig{}, chainRetrievalError{stderr.String(), err}
			}
			logger.Warn(chainRetrievalError{stderr.String(), err}.Error())
		}
		if chainBytes, err = os.ReadFile(chain.Name()); err != nil {
			chainBytes = []byte{}
		}
		var teeData SevWorkloadData
		if len(chainBytes) > 0 {
			chainBytesFile = "sev.chain"
			chainInfo, err = os.Stat(chain.Name())
			if err != nil {
				return nil, WorkloadConfig{}, err
			}
			teeData.VendorChain = "/" + chainBytesFile
		}
		encodedTeeData, err := json.Marshal(teeData)
		if err != nil {
			return nil, WorkloadConfig{}, fmt.Errorf("encoding tee data: %w", err)
		}
		workloadConfig.TeeData = string(encodedTeeData)
	case SNP:
		teeData := SnpWorkloadData{
			Generation: "milan",
		}
		encodedTeeData, err := json.Marshal(teeData)
		if err != nil {
			return nil, WorkloadConfig{}, fmt.Errorf("encoding tee data: %w", err)
		}
		workloadConfig.TeeData = string(encodedTeeData)
	}

	// We're going to want to add some content to the rootfs, so set up an
	// overlay that uses it as a lower layer so that we can write to it.
	st, err := system.Stat(rootfsPath)
	if err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("reading information about the container root filesystem: %w", err)
	}
	// Create a temporary directory to hold all of this.  Use tmpdir.GetTempDir()
	// instead of the passed-in location, which a crafty caller might have put in an
	// overlay filesystem in storage because there tends to be more room there than
	// in, say, /var/tmp, and the plaintext disk image, which we put in the passed-in
	// location, can get quite large.
	rootfsParentDir, err := os.MkdirTemp(tmpdir.GetTempDir(), "buildah-rootfs")
	if err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("setting up parent for container root filesystem: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(rootfsParentDir); err != nil {
			logger.Warnf("cleaning up parent for container root filesystem: %v", err)
		}
	}()
	// Create a mountpoint for the new overlay, which we'll use as the rootfs.
	rootfsDir := filepath.Join(rootfsParentDir, "rootfs")
	if err := idtools.MkdirAndChown(rootfsDir, fs.FileMode(st.Mode()), idtools.IDPair{UID: int(st.UID()), GID: int(st.GID())}); err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("creating mount target for container root filesystem: %w", err)
	}
	defer func() {
		if err := os.Remove(rootfsDir); err != nil {
			logger.Warnf("removing mount target for container root filesystem: %v", err)
		}
	}()
	// Create a directory to hold all of the overlay package's working state.
	tempDir := filepath.Join(rootfsParentDir, "tmp")
	if err = os.Mkdir(tempDir, 0o700); err != nil {
		return nil, WorkloadConfig{}, err
	}
	// Create some working state in there.
	overlayTempDir, err := overlay.TempDir(tempDir, int(st.UID()), int(st.GID()))
	if err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("setting up mount of container root filesystem: %w", err)
	}
	defer func() {
		if err := overlay.RemoveTemp(overlayTempDir); err != nil {
			logger.Warnf("cleaning up mount of container root filesystem: %v", err)
		}
	}()
	// Create a mount point using that working state.
	rootfsMount, err := overlay.Mount(overlayTempDir, rootfsPath, rootfsDir, 0, 0, options.GraphOptions)
	if err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("setting up support for overlay of container root filesystem: %w", err)
	}
	defer func() {
		if err := overlay.Unmount(overlayTempDir); err != nil {
			logger.Warnf("unmounting support for overlay of container root filesystem: %v", err)
		}
	}()
	// Follow through on the overlay or bind mount, whatever the overlay package decided
	// to leave to us to do.
	rootfsMountOptions := strings.Join(rootfsMount.Options, ",")
	logrus.Debugf("mounting %q to %q as %q with options %v", rootfsMount.Source, rootfsMount.Destination, rootfsMount.Type, rootfsMountOptions)
	if err := mount.Mount(rootfsMount.Source, rootfsMount.Destination, rootfsMount.Type, rootfsMountOptions); err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("mounting overlay of container root filesystem: %w", err)
	}
	defer func() {
		logrus.Debugf("unmounting %q", rootfsMount.Destination)
		if err := mount.Unmount(rootfsMount.Destination); err != nil {
			logger.Warnf("unmounting overlay of container root filesystem: %v", err)
		}
	}()
	// Pretend that we didn't have to do any of the preceding.
	rootfsPath = rootfsDir

	// Write extra content to the rootfs, creating intermediate directories if necessary.
	for location, content := range options.ExtraImageContent {
		err := func() error {
			if err := idtools.MkdirAllAndChownNew(filepath.Dir(filepath.Join(rootfsPath, location)), 0o755, idtools.IDPair{UID: int(st.UID()), GID: int(st.GID())}); err != nil {
				return fmt.Errorf("ensuring %q is present in container root filesystem: %w", filepath.Dir(location), err)
			}
			output, err := os.OpenFile(filepath.Join(rootfsPath, location), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("preparing to write %q to container root filesystem: %w", location, err)
			}
			defer output.Close()
			input, err := os.Open(content)
			if err != nil {
				return err
			}
			defer input.Close()
			if _, err := io.Copy(output, input); err != nil {
				return fmt.Errorf("copying contents of %q to %q in container root filesystem: %w", content, location, err)
			}
			if err := output.Chown(int(st.UID()), int(st.GID())); err != nil {
				return fmt.Errorf("setting owner of %q in the container root filesystem: %w", location, err)
			}
			if err := output.Chmod(0o644); err != nil {
				return fmt.Errorf("setting permissions on %q in the container root filesystem: %w", location, err)
			}
			return nil
		}()
		if err != nil {
			return nil, WorkloadConfig{}, err
		}
	}

	// Write part of the config blob where the krun init process will be
	// looking for it.  The oci2cw tool used `buildah inspect` output, but
	// init is just looking for fields that have the right names in any
	// object, and the image's config will have that, so let's try encoding
	// it directly.
	krunConfigPath := filepath.Join(rootfsPath, ".krun_config.json")
	krunConfigBytes, err := json.Marshal(ociConfig)
	if err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("creating .krun_config from image configuration: %w", err)
	}
	if err := ioutils.AtomicWriteFile(krunConfigPath, krunConfigBytes, 0o600); err != nil {
		return nil, WorkloadConfig{}, fmt.Errorf("saving krun config: %w", err)
	}

	// Encode the workload config, in case it fails for any reason.
	cleanedUpWorkloadConfig := workloadConfig
	switch cleanedUpWorkloadConfig.Type {
	default:
		return nil, WorkloadConfig{}, fmt.Errorf("don't know how to canonicalize TEE type %q", cleanedUpWorkloadConfig.Type)
	case SEV, SEV_NO_ES:
		cleanedUpWorkloadConfig.Type = SEV
	case SNP:
		cleanedUpWorkloadConfig.Type = SNP
	}
	workloadConfigBytes, err := json.Marshal(cleanedUpWorkloadConfig)
	if err != nil {
		return nil, WorkloadConfig{}, err
	}

	// Make sure we have the passphrase to use for encrypting the disk image.
	diskEncryptionPassphrase := options.DiskEncryptionPassphrase
	if diskEncryptionPassphrase == "" {
		diskEncryptionPassphrase, err = GenerateDiskEncryptionPassphrase()
		if err != nil {
			return nil, WorkloadConfig{}, err
		}
	}

	// If we weren't told how big the image should be, get a rough estimate
	// of the input data size, then add a hedge to it.
	imageSize := slop(options.ImageSize, options.Slop)
	if imageSize == 0 {
		var sourceSize int64
		if err := filepath.WalkDir(rootfsPath, func(_ string, d fs.DirEntry, err error) error {
			if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
				return err
			}
			info, err := d.Info()
			if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
				return err
			}
			sourceSize += info.Size()
			return nil
		}); err != nil {
			return nil, WorkloadConfig{}, err
		}
		imageSize = slop(sourceSize, options.Slop)
	}
	if imageSize%4096 != 0 {
		imageSize += (4096 - (imageSize % 4096))
	}
	if imageSize < minimumImageSize {
		imageSize = minimumImageSize
	}

	// Create a file to use as the unencrypted version of the disk image.
	plain, err := os.CreateTemp(options.TempDir, "plain.img")
	if err != nil {
		return nil, WorkloadConfig{}, err
	}
	removePlain := true
	defer func() {
		if removePlain {
			if err := os.Remove(plain.Name()); err != nil {
				logger.Warnf("removing temporary file %q: %v", plain.Name(), err)
			}
		}
	}()

	// Lengthen the plaintext disk image file.
	if err := plain.Truncate(imageSize); err != nil {
		plain.Close()
		return nil, WorkloadConfig{}, err
	}
	plainInfo, err := plain.Stat()
	plain.Close()
	if err != nil {
		return nil, WorkloadConfig{}, err
	}

	// Format the disk image with the filesystem contents.
	if _, stderr, err := MakeFS(rootfsPath, plain.Name(), filesystem); err != nil {
		if strings.TrimSpace(stderr) != "" {
			return nil, WorkloadConfig{}, fmt.Errorf("%s: %w", strings.TrimSpace(stderr), err)
		}
		return nil, WorkloadConfig{}, err
	}

	// If we're registering the workload, we can do that now.
	if workloadConfig.AttestationURL != "" {
		if err := SendRegistrationRequest(workloadConfig, diskEncryptionPassphrase, options.FirmwareLibrary, options.IgnoreAttestationErrors, logger); err != nil {
			return nil, WorkloadConfig{}, err
		}
	}

	// Try to encrypt on the fly.
	pipeReader, pipeWriter := io.Pipe()
	removePlain = false
	go func() {
		var err error
		defer func() {
			if err := os.Remove(plain.Name()); err != nil {
				logger.Warnf("removing temporary file %q: %v", plain.Name(), err)
			}
			if err != nil {
				pipeWriter.CloseWithError(err)
			} else {
				pipeWriter.Close()
			}
		}()
		plain, err := os.Open(plain.Name())
		if err != nil {
			logrus.Errorf("opening unencrypted disk image %q: %v", plain.Name(), err)
			return
		}
		defer plain.Close()
		tw := tar.NewWriter(pipeWriter)
		defer tw.Flush()

		// Write /entrypoint
		var decompressedEntrypoint bytes.Buffer
		decompressor, err := gzip.NewReader(bytes.NewReader(entrypointCompressedBytes))
		if err != nil {
			logrus.Errorf("decompressing copy of entrypoint: %v", err)
			return
		}
		defer decompressor.Close()
		if _, err = io.Copy(&decompressedEntrypoint, decompressor); err != nil {
			logrus.Errorf("decompressing copy of entrypoint: %v", err)
			return
		}
		entrypointHeader, err := tar.FileInfoHeader(plainInfo, "")
		if err != nil {
			logrus.Errorf("building header for entrypoint: %v", err)
			return
		}
		entrypointHeader.Name = "entrypoint"
		entrypointHeader.Mode = 0o755
		entrypointHeader.Uname, entrypointHeader.Gname = "", ""
		entrypointHeader.Uid, entrypointHeader.Gid = 0, 0
		entrypointHeader.Size = int64(decompressedEntrypoint.Len())
		if err = tw.WriteHeader(entrypointHeader); err != nil {
			logrus.Errorf("writing header for %q: %v", entrypointHeader.Name, err)
			return
		}
		if _, err = io.Copy(tw, &decompressedEntrypoint); err != nil {
			logrus.Errorf("writing %q: %v", entrypointHeader.Name, err)
			return
		}

		// Write /sev.chain
		if chainInfo != nil {
			chainHeader, err := tar.FileInfoHeader(chainInfo, "")
			if err != nil {
				logrus.Errorf("building header for %q: %v", chainInfo.Name(), err)
				return
			}
			chainHeader.Name = chainBytesFile
			chainHeader.Mode = 0o600
			chainHeader.Uname, chainHeader.Gname = "", ""
			chainHeader.Uid, chainHeader.Gid = 0, 0
			chainHeader.Size = int64(len(chainBytes))
			if err = tw.WriteHeader(chainHeader); err != nil {
				logrus.Errorf("writing header for %q: %v", chainHeader.Name, err)
				return
			}
			if _, err = tw.Write(chainBytes); err != nil {
				logrus.Errorf("writing %q: %v", chainHeader.Name, err)
				return
			}
		}

		// Write /krun-sev.json.
		workloadConfigHeader, err := tar.FileInfoHeader(plainInfo, "")
		if err != nil {
			logrus.Errorf("building header for %q: %v", plainInfo.Name(), err)
			return
		}
		workloadConfigHeader.Name = "krun-sev.json"
		workloadConfigHeader.Mode = 0o600
		workloadConfigHeader.Uname, workloadConfigHeader.Gname = "", ""
		workloadConfigHeader.Uid, workloadConfigHeader.Gid = 0, 0
		workloadConfigHeader.Size = int64(len(workloadConfigBytes))
		if err = tw.WriteHeader(workloadConfigHeader); err != nil {
			logrus.Errorf("writing header for %q: %v", workloadConfigHeader.Name, err)
			return
		}
		if _, err = tw.Write(workloadConfigBytes); err != nil {
			logrus.Errorf("writing %q: %v", workloadConfigHeader.Name, err)
			return
		}

		// Write /tmp.
		tmpHeader, err := tar.FileInfoHeader(plainInfo, "")
		if err != nil {
			logrus.Errorf("building header for %q: %v", plainInfo.Name(), err)
			return
		}
		tmpHeader.Name = "tmp/"
		tmpHeader.Typeflag = tar.TypeDir
		tmpHeader.Mode = 0o1777
		tmpHeader.Uname, tmpHeader.Gname = "", ""
		tmpHeader.Uid, tmpHeader.Gid = 0, 0
		tmpHeader.Size = 0
		if err = tw.WriteHeader(tmpHeader); err != nil {
			logrus.Errorf("writing header for %q: %v", tmpHeader.Name, err)
			return
		}

		// Now figure out the footer that we'll append to the encrypted disk.
		var footer bytes.Buffer
		lengthBuffer := make([]byte, 8)
		footer.Write(workloadConfigBytes)
		footer.WriteString("KRUN")
		binary.LittleEndian.PutUint64(lengthBuffer, uint64(len(workloadConfigBytes)))
		footer.Write(lengthBuffer)

		// Start encrypting and write /disk.img.
		header, encrypt, blockSize, err := luksy.EncryptV1([]string{diskEncryptionPassphrase}, "")
		paddingBoundary := int64(4096)
		paddingNeeded := (paddingBoundary - ((int64(len(header)) + imageSize + int64(footer.Len())) % paddingBoundary)) % paddingBoundary
		diskHeader := workloadConfigHeader
		diskHeader.Name = "disk.img"
		diskHeader.Mode = 0o600
		diskHeader.Size = int64(len(header)) + imageSize + paddingNeeded + int64(footer.Len())
		if err = tw.WriteHeader(diskHeader); err != nil {
			logrus.Errorf("writing archive header for disk.img: %v", err)
			return
		}
		if _, err = io.Copy(tw, bytes.NewReader(header)); err != nil {
			logrus.Errorf("writing encryption header for disk.img: %v", err)
			return
		}
		encryptWrapper := luksy.EncryptWriter(encrypt, tw, blockSize)
		if _, err = io.Copy(encryptWrapper, plain); err != nil {
			logrus.Errorf("encrypting disk.img: %v", err)
			return
		}
		encryptWrapper.Close()
		if _, err = tw.Write(make([]byte, paddingNeeded)); err != nil {
			logrus.Errorf("writing padding for disk.img: %v", err)
			return
		}
		if _, err = io.Copy(tw, &footer); err != nil {
			logrus.Errorf("writing footer for disk.img: %v", err)
			return
		}
		tw.Close()
	}()

	return pipeReader, workloadConfig, nil
}

func slop(size int64, slop string) int64 {
	if slop == "" {
		return size * 5 / 4
	}
	for factor := range strings.SplitSeq(slop, "+") {
		factor = strings.TrimSpace(factor)
		if factor == "" {
			continue
		}
		if strings.HasSuffix(factor, "%") {
			percentage := strings.TrimSuffix(factor, "%")
			percent, err := strconv.ParseInt(percentage, 10, 8)
			if err != nil {
				logrus.Warnf("parsing percentage %q: %v", factor, err)
			} else {
				size *= (percent + 100)
				size /= 100
			}
		} else {
			more, err := units.RAMInBytes(factor)
			if err != nil {
				logrus.Warnf("parsing %q as a size: %v", factor, err)
			} else {
				size += more
			}
		}
	}
	return size
}
