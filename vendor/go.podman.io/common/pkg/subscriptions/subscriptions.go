package subscriptions

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/umask"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
)

var (
	// DefaultMountsFile holds the default mount paths in the form
	// "host_path:container_path".
	DefaultMountsFile = "/usr/share/containers/mounts.conf"
	// OverrideMountsFile holds the default mount paths in the form
	// "host_path:container_path" overridden by the user.
	OverrideMountsFile = "/etc/containers/mounts.conf"
	// UserOverrideMountsFile holds the default mount paths in the form
	// "host_path:container_path" overridden by the rootless user.
	UserOverrideMountsFile = filepath.Join(os.Getenv("HOME"), ".config/containers/mounts.conf")
)

// subscriptionData stores the relative name of the file and the content read from it.
type subscriptionData struct {
	// relPath is the relative path to the file
	relPath string
	data    []byte
	mode    os.FileMode
	dirMode os.FileMode
}

// saveTo saves subscription data to given directory.
func (s subscriptionData) saveTo(dir string) error {
	// We need to join the path here and create all parent directories, only
	// creating dir is not good enough as relPath could also contain directories.
	path := filepath.Join(dir, s.relPath)
	if err := umask.MkdirAllIgnoreUmask(filepath.Dir(path), s.dirMode); err != nil {
		return fmt.Errorf("create subscription directory: %w", err)
	}
	if err := umask.WriteFileIgnoreUmask(path, s.data, s.mode); err != nil {
		return fmt.Errorf("write subscription data: %w", err)
	}
	return nil
}

func readAll(root, prefix string, parentMode os.FileMode) ([]subscriptionData, error) {
	path := filepath.Join(root, prefix)

	data := []subscriptionData{}

	files, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return data, nil
		}

		return nil, err
	}

	for _, f := range files {
		fileData, err := readFileOrDir(root, filepath.Join(prefix, f.Name()), parentMode)
		if err != nil {
			// If the file did not exist, might be a dangling symlink
			// Ignore the error
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		data = append(data, fileData...)
	}

	return data, nil
}

func readFileOrDir(root, name string, parentMode os.FileMode) ([]subscriptionData, error) {
	path := filepath.Join(root, name)

	s, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if s.IsDir() {
		dirData, err := readAll(root, name, s.Mode())
		if err != nil {
			return nil, err
		}
		return dirData, nil
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return []subscriptionData{{
		relPath: name,
		data:    bytes,
		mode:    s.Mode(),
		dirMode: parentMode,
	}}, nil
}

func getHostSubscriptionData(hostDir string, mode os.FileMode) ([]subscriptionData, error) {
	var allSubscriptions []subscriptionData
	hostSubscriptions, err := readAll(hostDir, "", mode)
	if err != nil {
		return nil, fmt.Errorf("failed to read subscriptions from %q: %w", hostDir, err)
	}
	return append(allSubscriptions, hostSubscriptions...), nil
}

func getMounts(filePath string) []string {
	file, err := os.Open(filePath)
	if err != nil {
		// This is expected on most systems
		logrus.Debugf("File %q not found, skipping...", filePath)
		return nil
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if err = scanner.Err(); err != nil {
		logrus.Errorf("Reading file %q, %v skipping...", filePath, err)
		return nil
	}
	var mounts []string
	for scanner.Scan() {
		if strings.HasPrefix(strings.TrimSpace(scanner.Text()), "/") {
			mounts = append(mounts, scanner.Text())
		} else {
			logrus.Debugf("Skipping unrecognized mount in %v: %q",
				filePath, scanner.Text())
		}
	}
	return mounts
}

// getHostAndCtrDir separates the host:container paths.
func getMountsMap(path string) (string, string) {
	host, ctr, ok := strings.Cut(path, ":")
	if !ok {
		return path, path
	}
	return host, ctr
}

// Return true iff the system is in FIPS mode as determined by reading
// /proc/sys/crypto/fips_enabled.
func shouldAddFIPSMounts() bool {
	fipsEnabled, err := os.ReadFile("/proc/sys/crypto/fips_enabled")
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logrus.Errorf("Failed to read /proc/sys/crypto/fips_enabled to determine FIPS state: %v", err)
		}
		return false
	}

	if strings.TrimSpace(string(fipsEnabled)) != "1" {
		logrus.Debug("/proc/sys/crypto/fips_enabled does not contain '1', not adding FIPS mode bind mounts")
		return false
	}

	return true
}

// MountsWithUIDGID copies, adds, and mounts the subscriptions to the container root filesystem
// mountLabel: MAC/SELinux label for container content
// containerRunDir: Private data for storing subscriptions on the host mounted in container.
// mountFile: Additional mount points required for the container.
// mountPoint: Container image mountpoint, or the directory from the hosts perspective that
//
//	corresponds to `/` in the container.
//
// uid: to assign to content created for subscriptions
// gid: to assign to content created for subscriptions
// rootless: indicates whether container is running in rootless mode
// disableFips: indicates whether system should ignore fips mode.
func MountsWithUIDGID(mountLabel, containerRunDir, mountFile, mountPoint string, uid, gid int, rootless, disableFips bool) []rspec.Mount {
	var (
		subscriptionMounts []rspec.Mount
		mountFiles         []string
	)
	// Add subscriptions from paths given in the mounts.conf files
	// mountFile will have a value if the hidden --default-mounts-file flag is set
	// Note for testing purposes only
	if mountFile == "" {
		mountFiles = append(mountFiles, []string{OverrideMountsFile, DefaultMountsFile}...)
		if rootless {
			mountFiles = append([]string{UserOverrideMountsFile}, mountFiles...)
		}
	} else {
		mountFiles = append(mountFiles, mountFile)
	}
	for _, file := range mountFiles {
		if err := fileutils.Exists(file); err == nil {
			mounts, err := addSubscriptionsFromMountsFile(file, mountLabel, containerRunDir, uid, gid)
			if err != nil {
				logrus.Warnf("Failed to mount subscriptions, skipping entry in %s: %v", file, err)
			}
			subscriptionMounts = mounts
			break
		}
	}

	// Only add FIPS subscription mount if disableFips is false and
	// /proc/sys/crypto/fips_enabled contains "1"
	if disableFips || !shouldAddFIPSMounts() {
		return subscriptionMounts
	}

	if err := addFIPSMounts(&subscriptionMounts, containerRunDir, mountPoint, mountLabel, uid, gid); err != nil {
		logrus.Errorf("Adding FIPS mode bind mounts to container: %v", err)
	}

	return subscriptionMounts
}

func rchown(chowndir string, uid, gid int) error {
	return filepath.Walk(chowndir, func(filePath string, _ os.FileInfo, err error) error {
		return os.Lchown(filePath, uid, gid)
	})
}

// addSubscriptionsFromMountsFile copies the contents of host directory to container directory
// and returns a list of mounts.
func addSubscriptionsFromMountsFile(filePath, mountLabel, containerRunDir string, uid, gid int) ([]rspec.Mount, error) {
	defaultMountsPaths := getMounts(filePath)
	mounts := make([]rspec.Mount, 0, len(defaultMountsPaths))
	for _, path := range defaultMountsPaths {
		hostDirOrFile, ctrDirOrFile := getMountsMap(path)
		// skip if the hostDirOrFile path doesn't exist
		fileInfo, err := os.Stat(hostDirOrFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logrus.Infof("Path %q from %q doesn't exist, skipping", hostDirOrFile, filePath)
				continue
			}
			return nil, err
		}

		ctrDirOrFileOnHost := filepath.Join(containerRunDir, ctrDirOrFile)

		// In the event of a restart, don't want to copy subscriptions over again as they already would exist in ctrDirOrFileOnHost
		err = fileutils.Exists(ctrDirOrFileOnHost)
		if errors.Is(err, os.ErrNotExist) {
			hostDirOrFile, err = resolveSymbolicLink(hostDirOrFile)
			if err != nil {
				return nil, err
			}

			switch mode := fileInfo.Mode(); {
			case mode.IsDir():
				if err = umask.MkdirAllIgnoreUmask(ctrDirOrFileOnHost, mode.Perm()); err != nil {
					return nil, fmt.Errorf("making container directory: %w", err)
				}
				data, err := getHostSubscriptionData(hostDirOrFile, mode.Perm())
				if err != nil {
					return nil, fmt.Errorf("getting host subscription data: %w", err)
				}
				for _, s := range data {
					if err := s.saveTo(ctrDirOrFileOnHost); err != nil {
						return nil, fmt.Errorf("saving data to container filesystem on host %q: %w", ctrDirOrFileOnHost, err)
					}
				}
			case mode.IsRegular():
				data, err := readFileOrDir("", hostDirOrFile, mode.Perm())
				if err != nil {
					return nil, err
				}
				for _, s := range data {
					dir := filepath.Dir(ctrDirOrFileOnHost)
					if err := umask.MkdirAllIgnoreUmask(dir, s.dirMode); err != nil {
						return nil, fmt.Errorf("create container dir: %w", err)
					}
					if err := umask.WriteFileIgnoreUmask(ctrDirOrFileOnHost, s.data, s.mode); err != nil {
						return nil, fmt.Errorf("saving data to container filesystem: %w", err)
					}
				}
			default:
				return nil, fmt.Errorf("unsupported file type for: %q", hostDirOrFile)
			}

			err = label.Relabel(ctrDirOrFileOnHost, mountLabel, false)
			if err != nil {
				return nil, fmt.Errorf("applying correct labels: %w", err)
			}
			if uid != 0 || gid != 0 {
				if err := rchown(ctrDirOrFileOnHost, uid, gid); err != nil {
					return nil, err
				}
			}
		} else if err != nil {
			return nil, err
		}

		m := rspec.Mount{
			Source:      ctrDirOrFileOnHost,
			Destination: ctrDirOrFile,
			Type:        "bind",
			Options:     []string{"bind", "rprivate"},
		}

		mounts = append(mounts, m)
	}
	return mounts, nil
}

func containerHasEtcSystemFips(subscriptionsDir, mountPoint string) (bool, error) {
	containerEtc, err := securejoin.SecureJoin(mountPoint, "etc")
	if err != nil {
		return false, fmt.Errorf("container /etc resolution error: %w", err)
	}
	if fileutils.Lexists(filepath.Join(containerEtc, "system-fips")) != nil {
		logrus.Debug("/etc/system-fips does not exist in the container, not creating /run/secrets/system-fips")
		return false, nil
	}

	fipsFileTarget, err := securejoin.SecureJoin(mountPoint, "etc/system-fips")
	if err != nil {
		return false, fmt.Errorf("container /etc/system-fips resolution error: %w", err)
	}
	if fipsFileTarget != filepath.Join(mountPoint, subscriptionsDir, "system-fips") {
		logrus.Warnf("/etc/system-fips exists in the container, but is not a symlink to %[1]v/system-fips; not creating %[1]v/system-fips", subscriptionsDir)
		return false, nil
	}

	return true, nil
}

// addFIPSMounts adds mounts to the `mounts` slice that are needed
// for the container to run cryptographic libraries (openssl, gnutls, NSS, ...)
// in FIPS mode (i.e: be FIPS compliant).
// It should only be called if /proc/sys/crypto/fips_enabled on the host
// contains '1'.
// It does three things:
//   - creates /run/secrets/system-fips in the container root filesystem if
//     /etc/system-fips exists and is a symlink to /run/secrets/system-fips,
//     and adds it to the `mounts` slice. This is, for example, the case on
//     RHEL 8, but not on newer RHEL, since /etc/system-fips is deprecated.
//   - Bind-mounts `/usr/share/crypto-policies/back-ends/FIPS` over
//     `/etc/crypto-policies/back-ends` if the former exists inside of the
//     container. This is done from within the container to avoid policy
//     incompatibility between container and host.
//   - If a bind mount for `/etc/crypto-policies/back-ends` was created,
//     bind-mounts `/usr/share/crypto-policies/default-fips-config` over
//     `/etc/crypto-policies/config` if the former exists inside of the
//     container. If it does not exist, creates a new temporary file containing
//     "FIPS\n", and bind-mounts that over `/etc/crypto-policies/config`.
//
// Starting in CentOS 10 Stream, the crypto-policies package gracefully recognizes the two bind mounts
//
//   - /etc/crypto-policies/config -> /usr/share/crypto-policies/default-fips-config
//   - /etc/crypto-policies/back-ends/FIPS -> /usr/share/crypto-policies/back-ends/FIPS
//
// and unmounts them when users manually change the policy, or removes and
// restores the mounts when the crypto-policies package is upgraded.
func addFIPSMounts(mounts *[]rspec.Mount, containerRunDir, mountPoint, mountLabel string, uid, gid int) error {
	// Check whether $container/etc/system-fips exists and is a symlink to /run/secrets/system-fips
	subscriptionsDir := "/run/secrets"

	createSystemFipsSecret, err := containerHasEtcSystemFips(subscriptionsDir, mountPoint)
	if err != nil {
		return err
	}
	if createSystemFipsSecret {
		// This container contains
		//   /etc/system-fips -> /run/secrets/system-fips
		// and expects podman to create this file if the container should
		// be in FIPS mode
		ctrDirOnHost := filepath.Join(containerRunDir, subscriptionsDir)
		if err := fileutils.Exists(ctrDirOnHost); errors.Is(err, os.ErrNotExist) {
			if err = idtools.MkdirAllAs(ctrDirOnHost, 0o755, uid, gid); err != nil { //nolint
				return err
			}
			if err = label.Relabel(ctrDirOnHost, mountLabel, false); err != nil {
				return fmt.Errorf("applying correct labels on %q: %w", ctrDirOnHost, err)
			}
		}
		fipsFile := filepath.Join(ctrDirOnHost, "system-fips")

		// In the event of restart, it is possible for the FIPS mode file to already exist
		if err := fileutils.Exists(fipsFile); errors.Is(err, os.ErrNotExist) {
			file, err := os.Create(fipsFile)
			if err != nil {
				return fmt.Errorf("creating system-fips file in container for FIPS mode: %w", err)
			}
			file.Close()
		}

		if !mountExists(*mounts, subscriptionsDir) {
			m := rspec.Mount{
				Source:      ctrDirOnHost,
				Destination: subscriptionsDir,
				Type:        "bind",
				Options:     []string{"bind", "rprivate"},
			}
			*mounts = append(*mounts, m)
		}
	}

	srcBackendDir := "/usr/share/crypto-policies/back-ends/FIPS"
	destDir := "/etc/crypto-policies/back-ends"
	srcOnHost, err := securejoin.SecureJoin(mountPoint, srcBackendDir)
	if err != nil {
		return fmt.Errorf("resolve %s in the container: %w", srcBackendDir, err)
	}
	if err := fileutils.Exists(srcOnHost); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("FIPS Backend directory: %w", err)
	}

	if !mountExists(*mounts, destDir) {
		m := rspec.Mount{
			Source:      srcOnHost,
			Destination: destDir,
			Type:        "bind",
			Options:     []string{"bind", "rprivate"},
		}
		*mounts = append(*mounts, m)
	}

	// Make sure we set the config to FIPS so that the container does not overwrite
	// /etc/crypto-policies/back-ends when crypto-policies-scripts is reinstalled.
	//
	// Starting in CentOS 10 Stream, crypto-policies provides
	// /usr/share/crypto-policies/default-fips-config as bind mount source
	// file and the crypto-policies tooling gracefully deals with the two bind-mounts
	//   /etc/crypto-policies/back-ends -> /usr/share/crypto-policies/back-ends/FIPS
	//   /etc/crypto-policies/config -> /usr/share/crypto-policies/default-fips-config
	// if they both exist.
	srcPolicyConfig := "/usr/share/crypto-policies/default-fips-config"
	destPolicyConfig := "/etc/crypto-policies/config"
	srcPolicyConfigOnHost, err := securejoin.SecureJoin(mountPoint, srcPolicyConfig)
	if err != nil {
		return fmt.Errorf("could not expand %q in container: %w", srcPolicyConfig, err)
	}

	if err = fileutils.Exists(srcPolicyConfigOnHost); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not check whether %q exists in container: %w", srcPolicyConfig, err)
		}

		// /usr/share/crypto-policies/default-fips-config does not exist, let's create it ourselves
		cryptoPoliciesConfigFile := filepath.Join(containerRunDir, "fips-config")
		if err := os.WriteFile(cryptoPoliciesConfigFile, []byte("FIPS\n"), 0o644); err != nil {
			return fmt.Errorf("failed to write fips config file in container for FIPS mode: %w", err)
		}
		if err = label.Relabel(cryptoPoliciesConfigFile, mountLabel, false); err != nil {
			return fmt.Errorf("failed to apply correct labels on fips config file: %w", err)
		}
		if err := os.Chown(cryptoPoliciesConfigFile, uid, gid); err != nil {
			return fmt.Errorf("failed to chown fips config file: %w", err)
		}

		srcPolicyConfigOnHost = cryptoPoliciesConfigFile
	}

	if !mountExists(*mounts, destPolicyConfig) {
		m := rspec.Mount{
			Source:      srcPolicyConfigOnHost,
			Destination: destPolicyConfig,
			Type:        "bind",
			Options:     []string{"bind", "rprivate"},
		}
		*mounts = append(*mounts, m)
	}
	return nil
}

// mountExists checks if a mount already exists in the spec.
func mountExists(mounts []rspec.Mount, dest string) bool {
	for _, mount := range mounts {
		if mount.Destination == dest {
			return true
		}
	}
	return false
}

// resolveSymbolicLink resolves symlink paths. If the path is a symlink, returns resolved
// path; if not, returns the original path.
func resolveSymbolicLink(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != os.ModeSymlink {
		return path, nil
	}
	return filepath.EvalSymlinks(path)
}
