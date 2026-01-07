package define

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chrootarchive"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/types"
)

const (
	// Package is the name of this package, used in help output and to
	// identify working containers.
	Package = "buildah"
	// Version for the Package. Also used by .packit.sh for Packit builds.
	Version = "1.42.2"

	// DefaultRuntime if containers.conf fails.
	DefaultRuntime = "runc"

	// OCIv1ImageManifest is the MIME type of an OCIv1 image manifest,
	// suitable for specifying as a value of the PreferredManifestType
	// member of a CommitOptions structure.  It is also the default.
	OCIv1ImageManifest = v1.MediaTypeImageManifest
	// Dockerv2ImageManifest is the MIME type of a Docker v2s2 image
	// manifest, suitable for specifying as a value of the
	// PreferredManifestType member of a CommitOptions structure.
	Dockerv2ImageManifest = manifest.DockerV2Schema2MediaType

	// OCI used to define the "oci" image format
	OCI = "oci"
	// DOCKER used to define the "docker" image format
	DOCKER = "docker"

	// SEV is a known trusted execution environment type: AMD-SEV (secure encrypted virtualization using encrypted state, requires epyc 1000 "naples")
	SEV TeeType = "sev"
	// SNP is a known trusted execution environment type: AMD-SNP (SEV secure nested pages) (requires epyc 3000 "milan")
	SNP TeeType = "snp"
)

// DefaultRlimitValue is the value set by default for nofile and nproc
const RLimitDefaultValue = uint64(1048576)

// TeeType is a supported trusted execution environment type.
type TeeType string

var (
	// Deprecated: DefaultCapabilities values should be retrieved from
	// github.com/containers/common/pkg/config
	DefaultCapabilities = []string{
		"CAP_AUDIT_WRITE",
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FOWNER",
		"CAP_FSETID",
		"CAP_KILL",
		"CAP_MKNOD",
		"CAP_NET_BIND_SERVICE",
		"CAP_SETFCAP",
		"CAP_SETGID",
		"CAP_SETPCAP",
		"CAP_SETUID",
		"CAP_SYS_CHROOT",
	}
	// Deprecated: DefaultNetworkSysctl values should be retrieved from
	// github.com/containers/common/pkg/config
	DefaultNetworkSysctl = map[string]string{
		"net.ipv4.ping_group_range": "0 0",
	}

	Gzip         = archive.Gzip
	Bzip2        = archive.Bzip2
	Xz           = archive.Xz
	Zstd         = archive.Zstd
	Uncompressed = archive.Uncompressed
)

// IDMappingOptions controls how we set up UID/GID mapping when we set up a
// user namespace.
type IDMappingOptions struct {
	HostUIDMapping bool
	HostGIDMapping bool
	UIDMap         []specs.LinuxIDMapping
	GIDMap         []specs.LinuxIDMapping
	AutoUserNs     bool
	AutoUserNsOpts types.AutoUserNsOptions
}

// Secret is a secret source that can be used in a RUN
type Secret struct {
	ID         string
	Source     string
	SourceType string
}

// BuildOutputOptions contains the the outcome of parsing the value of a build --output flag
type BuildOutputOption struct {
	Path     string // Only valid if !IsStdout
	IsDir    bool
	IsStdout bool
}

// ConfidentialWorkloadOptions encapsulates options which control whether or not
// we output an image whose rootfs contains a LUKS-compatibly-encrypted disk image
// instead of the usual rootfs contents.
type ConfidentialWorkloadOptions struct {
	Convert                  bool
	AttestationURL           string
	CPUs                     int
	Memory                   int
	TempDir                  string // used for the temporary plaintext copy of the disk image
	TeeType                  TeeType
	IgnoreAttestationErrors  bool
	WorkloadID               string
	DiskEncryptionPassphrase string
	Slop                     string
	FirmwareLibrary          string
}

// SBOMMergeStrategy tells us how to merge multiple SBOM documents into one.
type SBOMMergeStrategy string

const (
	// SBOMMergeStrategyCat literally concatenates the documents.
	SBOMMergeStrategyCat SBOMMergeStrategy = "cat"
	// SBOMMergeStrategyCycloneDXByComponentNameAndVersion adds components
	// from the second document to the first, so long as they have a
	// name+version combination which is not already present in the
	// components array.
	SBOMMergeStrategyCycloneDXByComponentNameAndVersion SBOMMergeStrategy = "merge-cyclonedx-by-component-name-and-version"
	// SBOMMergeStrategySPDXByPackageNameAndVersionInfo adds packages from
	// the second document to the first, so long as they have a
	// name+versionInfo combination which is not already present in the
	// first document's packages array, and adds hasExtractedLicensingInfos
	// items from the second document to the first, so long as they include
	// a licenseId value which is not already present in the first
	// document's hasExtractedLicensingInfos array.
	SBOMMergeStrategySPDXByPackageNameAndVersionInfo SBOMMergeStrategy = "merge-spdx-by-package-name-and-versioninfo"
)

// SBOMScanOptions encapsulates options which control whether or not we run a
// scanner on the rootfs that we're about to commit, and how.
type SBOMScanOptions struct {
	Type            []string          // a shorthand name for a defined group of these options
	Image           string            // the scanner image to use
	PullPolicy      PullPolicy        // how to get the scanner image
	Commands        []string          // one or more commands to invoke for the image rootfs or ContextDir locations
	ContextDir      []string          // one or more "source" directory locations
	SBOMOutput      string            // where to save SBOM scanner output outside of the image (i.e., the local filesystem)
	PURLOutput      string            // where to save PURL list outside of the image (i.e., the local filesystem)
	ImageSBOMOutput string            // where to save SBOM scanner output in the image
	ImagePURLOutput string            // where to save PURL list in the image
	MergeStrategy   SBOMMergeStrategy // how to merge the outputs of multiple scans
}

// TempDirForURL checks if the passed-in string looks like a URL or "-".  If it
// is, TempDirForURL creates a temporary directory, arranges for its contents
// to be the contents of that URL, and returns the temporary directory's path,
// along with the relative name of a subdirectory which should be used as the
// build context (which may be empty or ".").  Removal of the temporary
// directory is the responsibility of the caller.  If the string doesn't look
// like a URL or "-", TempDirForURL returns empty strings and a nil error code.
func TempDirForURL(dir, prefix, url string) (name string, subdir string, err error) {
	if !strings.HasPrefix(url, "http://") &&
		!strings.HasPrefix(url, "https://") &&
		!strings.HasPrefix(url, "git://") &&
		!strings.HasPrefix(url, "github.com/") &&
		url != "-" {
		return "", "", nil
	}
	name, err = os.MkdirTemp(dir, prefix)
	if err != nil {
		return "", "", fmt.Errorf("creating temporary directory for %q: %w", url, err)
	}
	downloadDir := filepath.Join(name, "download")
	if err = os.MkdirAll(downloadDir, 0o700); err != nil {
		return "", "", fmt.Errorf("creating directory %q for %q: %w", downloadDir, url, err)
	}
	urlParsed, err := urlpkg.Parse(url)
	if err != nil {
		return "", "", fmt.Errorf("parsing url %q: %w", url, err)
	}
	if strings.HasPrefix(url, "git://") || strings.HasSuffix(urlParsed.Path, ".git") {
		combinedOutput, gitSubDir, err := cloneToDirectory(url, downloadDir)
		if err != nil {
			if err2 := os.RemoveAll(name); err2 != nil {
				logrus.Debugf("error removing temporary directory %q: %v", name, err2)
			}
			return "", "", fmt.Errorf("cloning %q to %q:\n%s: %w", url, name, string(combinedOutput), err)
		}
		logrus.Debugf("Build context is at %q", filepath.Join(downloadDir, gitSubDir))
		return name, filepath.Join(filepath.Base(downloadDir), gitSubDir), nil
	}
	if strings.HasPrefix(url, "github.com/") {
		ghurl := url
		url = fmt.Sprintf("https://%s/archive/master.tar.gz", ghurl)
		logrus.Debugf("resolving url %q to %q", ghurl, url)
		subdir = path.Base(ghurl) + "-master"
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		err = downloadToDirectory(url, downloadDir)
		if err != nil {
			if err2 := os.RemoveAll(name); err2 != nil {
				logrus.Debugf("error removing temporary directory %q: %v", name, err2)
			}
			return "", "", err
		}
		logrus.Debugf("Build context is at %q", filepath.Join(downloadDir, subdir))
		return name, filepath.Join(filepath.Base(downloadDir), subdir), nil
	}
	if url == "-" {
		err = stdinToDirectory(downloadDir)
		if err != nil {
			if err2 := os.RemoveAll(name); err2 != nil {
				logrus.Debugf("error removing temporary directory %q: %v", name, err2)
			}
			return "", "", err
		}
		logrus.Debugf("Build context is at %q", filepath.Join(downloadDir, subdir))
		return name, filepath.Join(filepath.Base(downloadDir), subdir), nil
	}
	logrus.Debugf("don't know how to retrieve %q", url)
	if err2 := os.RemoveAll(name); err2 != nil {
		logrus.Debugf("error removing temporary directory %q: %v", name, err2)
	}
	return "", "", errors.New("unreachable code reached")
}

// parseGitBuildContext parses git build context to `repo`, `sub-dir`
// `branch/commit`, accepts GitBuildContext in the format of
// `repourl.git[#[branch-or-commit]:subdir]`.
func parseGitBuildContext(url string) (string, string, string) {
	gitSubdir := ""
	gitBranch := ""
	gitBranchPart := strings.Split(url, "#")
	if len(gitBranchPart) > 1 {
		// check if string contains path to a subdir
		gitSubDirPart := strings.Split(gitBranchPart[1], ":")
		if len(gitSubDirPart) > 1 {
			gitSubdir = gitSubDirPart[1]
		}
		gitBranch = gitSubDirPart[0]
	}
	return gitBranchPart[0], gitSubdir, gitBranch
}

func cloneToDirectory(url, dir string) ([]byte, string, error) {
	var cmd *exec.Cmd
	gitRepo, gitSubdir, gitRef := parseGitBuildContext(url)
	// init repo
	cmd = exec.Command("git", "init", dir)
	combinedOutput, err := cmd.CombinedOutput()
	if err != nil {
		// Return err.Error() instead of err as we want buildah to override error code with more predictable
		// value.
		return combinedOutput, gitSubdir, fmt.Errorf("failed while performing `git init`: %s", err.Error())
	}
	// add origin
	cmd = exec.Command("git", "remote", "add", "origin", gitRepo)
	cmd.Dir = dir
	combinedOutput, err = cmd.CombinedOutput()
	if err != nil {
		// Return err.Error() instead of err as we want buildah to override error code with more predictable
		// value.
		return combinedOutput, gitSubdir, fmt.Errorf("failed while performing `git remote add`: %s", err.Error())
	}

	logrus.Debugf("fetching repo %q and branch (or commit ID) %q to %q", gitRepo, gitRef, dir)
	args := []string{"fetch", "-u", "--depth=1", "origin", "--", gitRef}
	cmd = exec.Command("git", args...)
	cmd.Dir = dir
	combinedOutput, err = cmd.CombinedOutput()
	if err != nil {
		// Return err.Error() instead of err as we want buildah to override error code with more predictable
		// value.
		return combinedOutput, gitSubdir, fmt.Errorf("failed while performing `git fetch`: %s", err.Error())
	}

	cmd = exec.Command("git", "checkout", "FETCH_HEAD")
	cmd.Dir = dir
	combinedOutput, err = cmd.CombinedOutput()
	if err != nil {
		// Return err.Error() instead of err as we want buildah to override error code with more predictable
		// value.
		return combinedOutput, gitSubdir, fmt.Errorf("failed while performing `git checkout`: %s", err.Error())
	}
	return combinedOutput, gitSubdir, nil
}

func downloadToDirectory(url, dir string) error {
	logrus.Debugf("extracting %q to %q", url, dir)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("invalid response status %d", resp.StatusCode)
	}
	if resp.ContentLength == 0 {
		return fmt.Errorf("no contents in %q", url)
	}
	if err := chrootarchive.Untar(resp.Body, dir, nil); err != nil {
		resp1, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp1.Body.Close()
		body, err := io.ReadAll(resp1.Body)
		if err != nil {
			return err
		}
		dockerfile := filepath.Join(dir, "Dockerfile")
		// Assume this is a Dockerfile
		if err := ioutils.AtomicWriteFile(dockerfile, body, 0o600); err != nil {
			return fmt.Errorf("failed to write %q to %q: %w", url, dockerfile, err)
		}
	}
	return nil
}

func stdinToDirectory(dir string) error {
	logrus.Debugf("extracting stdin to %q", dir)
	r := bufio.NewReader(os.Stdin)
	b, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("failed to read from stdin: %w", err)
	}
	reader := bytes.NewReader(b)
	if err := chrootarchive.Untar(reader, dir, nil); err != nil {
		dockerfile := filepath.Join(dir, "Dockerfile")
		// Assume this is a Dockerfile
		if err := ioutils.AtomicWriteFile(dockerfile, b, 0o600); err != nil {
			return fmt.Errorf("failed to write bytes to %q: %w", dockerfile, err)
		}
	}
	return nil
}
