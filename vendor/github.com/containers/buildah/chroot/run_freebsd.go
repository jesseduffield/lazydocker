//go:build freebsd

package chroot

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/containers/buildah/pkg/jail"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

var (
	rlimitsMap = map[string]int{
		"RLIMIT_AS":      unix.RLIMIT_AS,
		"RLIMIT_CORE":    unix.RLIMIT_CORE,
		"RLIMIT_CPU":     unix.RLIMIT_CPU,
		"RLIMIT_DATA":    unix.RLIMIT_DATA,
		"RLIMIT_FSIZE":   unix.RLIMIT_FSIZE,
		"RLIMIT_MEMLOCK": unix.RLIMIT_MEMLOCK,
		"RLIMIT_NOFILE":  unix.RLIMIT_NOFILE,
		"RLIMIT_NPROC":   unix.RLIMIT_NPROC,
		"RLIMIT_RSS":     unix.RLIMIT_RSS,
		"RLIMIT_STACK":   unix.RLIMIT_STACK,
	}
	rlimitsReverseMap = map[int]string{}
)

type runUsingChrootSubprocOptions struct {
	Spec       *specs.Spec
	BundlePath string
	NoPivot    bool
}

func setPlatformUnshareOptions(spec *specs.Spec, cmd *unshare.Cmd) error {
	return nil
}

func setContainerHostname(name string) {
	// On FreeBSD, we have to set this later when we create the
	// jail below in createPlatformContainer
}

func setSelinuxLabel(spec *specs.Spec) error {
	// Ignore this on FreeBSD
	return nil
}

func setApparmorProfile(spec *specs.Spec) error {
	// FreeBSD doesn't have apparmor`
	return nil
}

func setCapabilities(spec *specs.Spec, keepCaps ...string) error {
	// FreeBSD capabilities are nothing like Linux
	return nil
}

func makeRlimit(limit specs.POSIXRlimit) unix.Rlimit {
	return unix.Rlimit{Cur: int64(limit.Soft), Max: int64(limit.Hard)}
}

func createPlatformContainer(options runUsingChrootExecSubprocOptions) error {
	path := options.Spec.Root.Path
	jconf := jail.NewConfig()
	jconf.Set("name", filepath.Base(path)+"-chroot")
	jconf.Set("host.hostname", options.Spec.Hostname)
	jconf.Set("persist", false)
	jconf.Set("path", path)
	jconf.Set("ip4", jail.INHERIT)
	jconf.Set("ip6", jail.INHERIT)
	jconf.Set("allow.raw_sockets", true)
	jconf.Set("enforce_statfs", 1)
	_, err := jail.CreateAndAttach(jconf)
	if err != nil {
		return fmt.Errorf("creating jail: %w", err)
	}
	return nil
}

// logNamespaceDiagnostics knows which namespaces we want to create.
// Output debug messages when that differs from what we're being asked to do.
func logNamespaceDiagnostics(spec *specs.Spec) {
	// Nothing here for FreeBSD
}

func makeReadOnly(mntpoint string, flags uintptr) error {
	var fs unix.Statfs_t
	// Make sure it's read-only.
	if err := unix.Statfs(mntpoint, &fs); err != nil {
		return fmt.Errorf("checking if directory %q was bound read-only: %w", mntpoint, err)
	}
	return nil
}

func saveDir(spec *specs.Spec, path string) string {
	id := filepath.Base(spec.Root.Path)
	return filepath.Join(filepath.Dir(path), ".save-"+id)
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

type rename struct {
	from, to string
}

// setupChrootBindMounts actually bind mounts things under the rootfs, and returns a
// callback that will clean up its work.
func setupChrootBindMounts(spec *specs.Spec, bundlePath string) (undoBinds func() error, err error) {
	renames := []rename{}
	unmounts := []string{}
	removes := []string{}
	undoBinds = func() error {
		for _, r := range renames {
			if err2 := os.Rename(r.to, r.from); err2 != nil {
				logrus.Warnf("pkg/chroot: error renaming %q to %q: %v", r.to, r.from, err2)
				if err == nil {
					err = err2
				}
			}
		}
		for _, path := range unmounts {
			if err2 := mount.Unmount(path); err2 != nil {
				logrus.Warnf("pkg/chroot: error unmounting %q: %v", spec.Root.Path, err2)
				if err == nil {
					err = err2
				}
			}
		}
		for _, path := range removes {
			if err2 := os.Remove(path); err2 != nil {
				logrus.Warnf("pkg/chroot: error removing %q: %v", path, err2)
				if err == nil {
					err = err2
				}
			}
		}
		return err
	}

	// Now mount all of those things to be under the rootfs's location in this
	// mount namespace.
	for _, m := range spec.Mounts {
		// If the target is there, we can just mount it.
		var srcinfo os.FileInfo
		switch m.Type {
		case "nullfs":
			srcinfo, err = os.Stat(m.Source)
			if err != nil {
				return undoBinds, fmt.Errorf("examining %q for mounting in mount namespace: %w", m.Source, err)
			}
		}
		target := filepath.Join(spec.Root.Path, m.Destination)
		if err := fileutils.Exists(target); err != nil {
			// If the target can't be stat()ted, check the error.
			if !errors.Is(err, fs.ErrNotExist) {
				return undoBinds, fmt.Errorf("examining %q for mounting in mount namespace: %w", target, err)
			}
			// The target isn't there yet, so create it, and make a
			// note to remove it later.
			// XXX: This was copied from the linux version which supports bind mounting files.
			// Leaving it here since I plan to add this to FreeBSD's nullfs.
			if m.Type != "nullfs" || srcinfo.IsDir() {
				if err = os.MkdirAll(target, 0o111); err != nil {
					return undoBinds, fmt.Errorf("creating mountpoint %q in mount namespace: %w", target, err)
				}
				removes = append(removes, target)
			} else {
				if err = os.MkdirAll(filepath.Dir(target), 0o111); err != nil {
					return undoBinds, fmt.Errorf("ensuring parent of mountpoint %q (%q) is present in mount namespace: %w", target, filepath.Dir(target), err)
				}
				// Don't do this until we can support file mounts in nullfs
				/*var file *os.File
				if file, err = os.OpenFile(target, os.O_WRONLY|os.O_CREATE, 0); err != nil {
					return undoBinds, errors.Wrapf(err, "error creating mountpoint %q in mount namespace", target)
				}
				file.Close()
				removes = append(removes, target)*/
			}
		}
		logrus.Debugf("mount: %v", m)
		switch m.Type {
		case "nullfs":
			// Do the bind mount.
			if !srcinfo.IsDir() {
				logrus.Debugf("emulating file mount %q on %q", m.Source, target)
				err := fileutils.Exists(target)
				if err == nil {
					save := saveDir(spec, target)
					if err := fileutils.Exists(save); err != nil {
						if errors.Is(err, fs.ErrNotExist) {
							err = os.MkdirAll(save, 0o111)
						}
						if err != nil {
							return undoBinds, fmt.Errorf("creating file mount save directory %q: %w", save, err)
						}
						removes = append(removes, save)
					}
					savePath := filepath.Join(save, filepath.Base(target))
					if err := fileutils.Exists(target); err == nil {
						logrus.Debugf("moving %q to %q", target, savePath)
						if err := os.Rename(target, savePath); err != nil {
							return undoBinds, fmt.Errorf("moving %q to %q: %w", target, savePath, err)
						}
						renames = append(renames, rename{
							from: target,
							to:   savePath,
						})
					}
				} else {
					removes = append(removes, target)
				}
				if err := copyFile(m.Source, target); err != nil {
					return undoBinds, fmt.Errorf("copying %q to %q: %w", m.Source, target, err)
				}
			} else {
				logrus.Debugf("bind mounting %q on %q", m.Destination, filepath.Join(spec.Root.Path, m.Destination))
				if err := mount.Mount(m.Source, target, "nullfs", strings.Join(m.Options, ",")); err != nil {
					return undoBinds, fmt.Errorf("bind mounting %q from host to %q in mount namespace (%q): %w", m.Source, m.Destination, target, err)
				}
				logrus.Debugf("bind mounted %q to %q", m.Source, target)
				unmounts = append(unmounts, target)
			}
		case "devfs", "fdescfs", "tmpfs":
			// Mount /dev, /dev/fd.
			if err := mount.Mount(m.Source, target, m.Type, strings.Join(m.Options, ",")); err != nil {
				return undoBinds, fmt.Errorf("mounting %q to %q in mount namespace (%q, %q): %w", m.Type, m.Destination, target, strings.Join(m.Options, ","), err)
			}
			logrus.Debugf("mounted a %q to %q", m.Type, target)
			unmounts = append(unmounts, target)
		}
	}
	return undoBinds, nil
}

// setPdeathsig sets a parent-death signal for the process
func setPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
