//go:build !remote && (linux || freebsd)

package libpod

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	buildahCopiah "github.com/containers/buildah/copier"
	"github.com/containers/buildah/pkg/chrootuser"
	"github.com/containers/buildah/util"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/shutdown"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/stringid"
)

func (c *Container) copyFromArchive(path string, chown, noOverwriteDirNonDir bool, rename map[string]string, reader io.Reader) (func() error, error) {
	var (
		mountPoint   string
		resolvedRoot string
		resolvedPath string
		unmount      func()
		cleanupFuncs []func()
		err          error
		locked       = true
	)

	// Make sure that "/" copies the *contents* of the mount point and not
	// the directory.
	if path == "/" {
		path = "/."
	}

	// Optimization: only mount if the container is not already.
	if c.state.Mounted {
		mountPoint = c.state.Mountpoint
		unmount = func() {}
	} else {
		// NOTE: make sure to unmount in error paths.
		mountPoint, err = c.mount()
		if err != nil {
			return nil, err
		}
		c.state.Mountpoint = mountPoint
		if err := c.save(); err != nil {
			return nil, err
		}

		unmount = func() {
			if !locked {
				c.lock.Lock()
				defer c.lock.Unlock()
			}

			if err := c.syncContainer(); err != nil {
				logrus.Errorf("Unable to sync container %s state: %v", c.ID(), err)
				return
			}

			// These have to be first, some of them rely on container rootfs still being mounted.
			for _, cleanupFunc := range cleanupFuncs {
				cleanupFunc()
			}
			if err := c.unmount(false); err != nil {
				logrus.Errorf("Failed to unmount container: %v", err)
			}

			if c.ensureState(define.ContainerStateConfigured, define.ContainerStateExited) {
				c.state.Mountpoint = ""
				if err := c.save(); err != nil {
					logrus.Errorf("Writing container %s state: %v", c.ID(), err)
				}
			}
		}

		// Before we proceed, mount all named volumes associated with the
		// container.
		// This solves two issues:
		// Firstly, it ensures that if the volume actually requires a mount, we
		// will mount it for safe use.
		// (For example, image volumes, volume plugins).
		// Secondly, it copies up into the volume if necessary.
		// This ensures that permissions are correct for copies into volumes on
		// containers that have never started.
		if len(c.config.NamedVolumes) > 0 {
			for _, v := range c.config.NamedVolumes {
				vol, err := c.mountNamedVolume(v, mountPoint)
				if err != nil {
					unmount()
					return nil, err
				}

				volUnmountName := fmt.Sprintf("volume unmount %s %s", vol.Name(), stringid.GenerateNonCryptoID()[0:12])

				// The unmount function can be called in two places:
				// First, from unmount(), our generic cleanup function that gets
				// called on success or on failure by error.
				// Second, from the shutdown handler on receipt of a SIGTERM
				// or similar.
				volUnmountFunc := func() error {
					vol.lock.Lock()
					defer vol.lock.Unlock()

					if err := vol.unmount(false); err != nil {
						return err
					}

					return nil
				}

				cleanupFuncs = append(cleanupFuncs, func() {
					_ = shutdown.Unregister(volUnmountName)

					if err := volUnmountFunc(); err != nil {
						logrus.Errorf("Unmounting container %s volume %s: %v", c.ID(), vol.Name(), err)
					}
				})

				if err := shutdown.Register(volUnmountName, func(_ os.Signal) error {
					return volUnmountFunc()
				}); err != nil && !errors.Is(err, shutdown.ErrHandlerExists) {
					return nil, fmt.Errorf("adding shutdown handler for volume %s unmount: %w", vol.Name(), err)
				}
			}
		}
	}

	resolvedRoot, resolvedPath, volume, err := c.resolveCopyTarget(mountPoint, path)
	if err != nil {
		unmount()
		return nil, err
	}

	if volume != nil {
		// This must be the first cleanup function so it fires before volume unmounts happen.
		cleanupFuncs = append([]func(){func() {
			// This is a gross hack to ensure correct permissions
			// on a volume that was copied into that needed, but did
			// not receive, a copy-up.
			// Why do we need this?
			// Basically: fixVolumePermissions is needed to ensure
			// the volume has the right permissions.
			// However, fixVolumePermissions only fires on a volume
			// that is not empty iff a copy-up occurred.
			// In this case, the volume is not empty as we just
			// copied into it, so in order to get
			// fixVolumePermissions to actually run, we must
			// convince it that a copy-up occurred - even if it did
			// not.
			// At the same time, clear NeedsCopyUp as we just
			// populated the volume and that will block a future
			// copy-up.
			volume.lock.Lock()
			defer volume.lock.Unlock()

			if err := volume.update(); err != nil {
				logrus.Errorf("Unable to update volume %s status: %v", volume.Name(), err)
				return
			}

			if volume.state.NeedsCopyUp && volume.state.NeedsChown {
				volume.state.NeedsCopyUp = false
				volume.state.CopiedUp = true
				if err := volume.save(); err != nil {
					logrus.Errorf("Unable to save volume %s state: %v", volume.Name(), err)
					return
				}

				for _, namedVol := range c.config.NamedVolumes {
					if namedVol.Name == volume.Name() {
						if err := c.fixVolumePermissionsUnlocked(namedVol, volume); err != nil {
							logrus.Errorf("Unable to fix volume %s permissions: %v", volume.Name(), err)
						}
						return
					}
				}
			}
		}}, cleanupFuncs...)
	}

	var idPair *idtools.IDPair
	if chown {
		// Make sure we chown the files to the container's main user and group ID.
		user, err := getContainerUser(c, mountPoint)
		if err != nil {
			unmount()
			return nil, err
		}
		idPair = &idtools.IDPair{UID: int(user.UID), GID: int(user.GID)}
	}

	decompressed, err := archive.DecompressStream(reader)
	if err != nil {
		unmount()
		return nil, err
	}

	locked = false

	logrus.Debugf("Container copy *to* %q (resolved: %q) on container %q (ID: %s)", path, resolvedPath, c.Name(), c.ID())

	return func() error {
		defer unmount()
		defer decompressed.Close()
		putOptions := buildahCopiah.PutOptions{
			UIDMap:               c.config.IDMappings.UIDMap,
			GIDMap:               c.config.IDMappings.GIDMap,
			ChownDirs:            idPair,
			ChownFiles:           idPair,
			NoOverwriteDirNonDir: noOverwriteDirNonDir,
			NoOverwriteNonDirDir: noOverwriteDirNonDir,
			Rename:               rename,
		}

		return c.joinMountAndExec(
			func() error {
				return buildahCopiah.Put(resolvedRoot, resolvedPath, putOptions, decompressed)
			},
		)
	}, nil
}

func (c *Container) copyToArchive(path string, writer io.Writer) (func() error, error) {
	var (
		mountPoint string
		unmount    func()
		err        error
	)

	// Optimization: only mount if the container is not already.
	if c.state.Mounted {
		mountPoint = c.state.Mountpoint
		unmount = func() {}
	} else {
		// NOTE: make sure to unmount in error paths.
		mountPoint, err = c.mount()
		if err != nil {
			return nil, err
		}
		unmount = func() {
			if err := c.unmount(false); err != nil {
				logrus.Errorf("Failed to unmount container: %v", err)
			}
		}
	}

	statInfo, resolvedRoot, resolvedPath, err := c.stat(mountPoint, path)
	if err != nil {
		unmount()
		return nil, err
	}

	// We optimistically chown to the host user.  In case of a hypothetical
	// container-to-container copy, the reading side will chown back to the
	// container user.
	user, err := getContainerUser(c, mountPoint)
	if err != nil {
		unmount()
		return nil, err
	}
	hostUID, hostGID, err := util.GetHostIDs(
		idtoolsToRuntimeSpec(c.config.IDMappings.UIDMap),
		idtoolsToRuntimeSpec(c.config.IDMappings.GIDMap),
		user.UID,
		user.GID,
	)
	if err != nil {
		unmount()
		return nil, err
	}
	idPair := idtools.IDPair{UID: int(hostUID), GID: int(hostGID)}

	logrus.Debugf("Container copy *from* %q (resolved: %q) on container %q (ID: %s)", path, resolvedPath, c.Name(), c.ID())

	return func() error {
		defer unmount()
		getOptions := buildahCopiah.GetOptions{
			// Unless the specified points to ".", we want to copy the base directory.
			KeepDirectoryNames: statInfo.IsDir && filepath.Base(path) != ".",
			UIDMap:             c.config.IDMappings.UIDMap,
			GIDMap:             c.config.IDMappings.GIDMap,
			ChownDirs:          &idPair,
			ChownFiles:         &idPair,
			Excludes:           []string{"dev", "proc", "sys"},
			// Ignore EPERMs when copying from rootless containers
			// since we cannot read TTY devices.  Those are owned
			// by the host's root and hence "nobody" inside the
			// container's user namespace.
			IgnoreUnreadable: rootless.IsRootless() && c.state.State == define.ContainerStateRunning,
		}
		return c.joinMountAndExec(
			func() error {
				return buildahCopiah.Get(resolvedRoot, "", getOptions, []string{resolvedPath}, writer)
			},
		)
	}, nil
}

// getContainerUser returns the specs.User and ID mappings of the container.
func getContainerUser(container *Container, mountPoint string) (specs.User, error) {
	userspec := container.config.User

	uid, gid, _, err := chrootuser.GetUser(mountPoint, userspec)
	u := specs.User{
		UID:      uid,
		GID:      gid,
		Username: userspec,
	}

	if !strings.Contains(userspec, ":") {
		groups, err2 := chrootuser.GetAdditionalGroupsForUser(mountPoint, uint64(u.UID))
		if err2 != nil {
			if !errors.Is(err2, chrootuser.ErrNoSuchUser) && err == nil {
				err = err2
			}
		} else {
			u.AdditionalGids = groups
		}
	}

	return u, err
}

// idtoolsToRuntimeSpec converts idtools ID mapping to the one of the runtime spec.
func idtoolsToRuntimeSpec(idMaps []idtools.IDMap) (convertedIDMap []specs.LinuxIDMapping) {
	for _, idmap := range idMaps {
		tempIDMap := specs.LinuxIDMapping{
			ContainerID: uint32(idmap.ContainerID),
			HostID:      uint32(idmap.HostID),
			Size:        uint32(idmap.Size),
		}
		convertedIDMap = append(convertedIDMap, tempIDMap)
	}
	return convertedIDMap
}
