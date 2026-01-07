//go:build !remote && (linux || freebsd)

package libpod

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	metadata "github.com/checkpoint-restore/checkpointctl/lib"
	"github.com/checkpoint-restore/go-criu/v7/stats"
	"github.com/containers/buildah"
	"github.com/containers/buildah/pkg/chrootuser"
	"github.com/containers/buildah/pkg/overlay"
	butil "github.com/containers/buildah/util"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/pkg/annotations"
	"github.com/containers/podman/v5/pkg/checkpoint/crutils"
	"github.com/containers/podman/v5/pkg/criu"
	"github.com/containers/podman/v5/pkg/lookup"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/containers/podman/v5/version"
	securejoin "github.com/cyphar/filepath-securejoin"
	runcuser "github.com/moby/sys/user"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/common/libnetwork/resolvconf"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/apparmor"
	"go.podman.io/common/pkg/chown"
	"go.podman.io/common/pkg/config"
	libartTypes "go.podman.io/common/pkg/libartifact/types"
	"go.podman.io/common/pkg/subscriptions"
	"go.podman.io/common/pkg/umask"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/unshare"
	stypes "go.podman.io/storage/types"
	"golang.org/x/sys/unix"
	cdi "tags.cncf.io/container-device-interface/pkg/cdi"
)

func parseOptionIDs(ctrMappings []idtools.IDMap, option string) ([]idtools.IDMap, error) {
	ranges := strings.Split(option, "#")
	ret := make([]idtools.IDMap, len(ranges))
	for i, m := range ranges {
		var v idtools.IDMap

		if m == "" {
			return nil, fmt.Errorf("invalid empty range for %q", option)
		}

		relative := false
		if m[0] == '@' {
			relative = true
			m = m[1:]
		}
		_, err := fmt.Sscanf(m, "%d-%d-%d", &v.ContainerID, &v.HostID, &v.Size)
		if err != nil {
			return nil, err
		}
		if v.ContainerID < 0 || v.HostID < 0 || v.Size < 1 {
			return nil, fmt.Errorf("invalid value for %q", option)
		}

		if relative {
			found := false
			for _, m := range ctrMappings {
				if v.HostID >= m.ContainerID && v.HostID < m.ContainerID+m.Size {
					v.HostID += m.HostID - m.ContainerID
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("could not find a user namespace mapping for the relative mapping %q", option)
			}
		}
		ret[i] = v
	}
	return ret, nil
}

func parseIDMapMountOption(idMappings stypes.IDMappingOptions, option string) ([]spec.LinuxIDMapping, []spec.LinuxIDMapping, error) {
	uidMap := idMappings.UIDMap
	gidMap := idMappings.GIDMap
	if strings.HasPrefix(option, "idmap=") {
		var err error
		options := strings.SplitSeq(strings.SplitN(option, "=", 2)[1], ";")
		for i := range options {
			switch {
			case strings.HasPrefix(i, "uids="):
				uidMap, err = parseOptionIDs(idMappings.UIDMap, strings.Replace(i, "uids=", "", 1))
				if err != nil {
					return nil, nil, err
				}
			case strings.HasPrefix(i, "gids="):
				gidMap, err = parseOptionIDs(idMappings.GIDMap, strings.Replace(i, "gids=", "", 1))
				if err != nil {
					return nil, nil, err
				}
			default:
				return nil, nil, fmt.Errorf("unknown option %q", i)
			}
		}
	}

	uidMappings := make([]spec.LinuxIDMapping, len(uidMap))
	gidMappings := make([]spec.LinuxIDMapping, len(gidMap))
	for i, uidmap := range uidMap {
		uidMappings[i] = spec.LinuxIDMapping{
			HostID:      uint32(uidmap.HostID),
			ContainerID: uint32(uidmap.ContainerID),
			Size:        uint32(uidmap.Size),
		}
	}
	for i, gidmap := range gidMap {
		gidMappings[i] = spec.LinuxIDMapping{
			HostID:      uint32(gidmap.HostID),
			ContainerID: uint32(gidmap.ContainerID),
			Size:        uint32(gidmap.Size),
		}
	}
	return uidMappings, gidMappings, nil
}

// Internal only function which returns upper and work dir from
// overlay options.
func getOverlayUpperAndWorkDir(options []string) (string, string, error) {
	upperDir := ""
	workDir := ""
	for _, o := range options {
		if strings.HasPrefix(o, "upperdir") {
			splitOpt := strings.SplitN(o, "=", 2)
			if len(splitOpt) > 1 {
				upperDir = splitOpt[1]
				if upperDir == "" {
					return "", "", errors.New("cannot accept empty value for upperdir")
				}
			}
		}
		if strings.HasPrefix(o, "workdir") {
			splitOpt := strings.SplitN(o, "=", 2)
			if len(splitOpt) > 1 {
				workDir = splitOpt[1]
				if workDir == "" {
					return "", "", errors.New("cannot accept empty value for workdir")
				}
			}
		}
	}
	if (upperDir != "" && workDir == "") || (upperDir == "" && workDir != "") {
		return "", "", errors.New("must specify both upperdir and workdir")
	}
	return upperDir, workDir, nil
}

// Internal only function which creates the Rootfs for default internal
// pause image and configures the Rootfs in the Container.
func (c *Container) createInitRootfs() error {
	tmpDir, err := c.runtime.TmpDir()
	if err != nil {
		return fmt.Errorf("getting runtime temporary directory: %w", err)
	}
	tmpDir = filepath.Join(tmpDir, "infra-container")
	err = os.MkdirAll(tmpDir, 0o755)
	if err != nil {
		return fmt.Errorf("creating infra container temporary directory: %w", err)
	}

	c.config.Rootfs = tmpDir
	c.config.RootfsOverlay = true
	return nil
}

// Internal only function which returns the mount-point for the /catatonit.
// This mount-point should be added to the Container spec.
func (c *Container) prepareCatatonitMount() (spec.Mount, error) {
	newMount := spec.Mount{
		Type:        define.TypeBind,
		Source:      "",
		Destination: "",
		Options:     append(bindOptions, "ro", "nosuid", "nodev"),
	}

	// Also look into the path as some distributions install catatonit in
	// /usr/bin.
	catatonitPath, err := c.runtime.config.FindInitBinary()
	if err != nil {
		return newMount, fmt.Errorf("finding catatonit binary: %w", err)
	}
	catatonitPath, err = filepath.EvalSymlinks(catatonitPath)
	if err != nil {
		return newMount, fmt.Errorf("follow symlink to catatonit binary: %w", err)
	}

	newMount.Source = catatonitPath
	newMount.Destination = "/" + filepath.Base(catatonitPath)

	if len(c.config.Entrypoint) == 0 {
		c.config.Entrypoint = []string{"/" + filepath.Base(catatonitPath), "-P"}
		c.config.Spec.Process.Args = c.config.Entrypoint
	}

	return newMount, nil
}

// Generate spec for a container
// Accepts a map of the container's dependencies
func (c *Container) generateSpec(ctx context.Context) (s *spec.Spec, cleanupFuncRet func(), err error) {
	var safeMounts []*safeMountInfo
	// lock the thread so that the current thread will be kept alive until the mounts are used
	runtime.LockOSThread()
	cleanupFunc := func() {
		runtime.UnlockOSThread()
		for _, s := range safeMounts {
			s.Close()
		}
	}
	defer func() {
		if err != nil {
			cleanupFunc()
		}
	}()

	if err := c.makeBindMounts(); err != nil {
		return nil, nil, err
	}

	overrides := c.getUserOverrides()
	execUser, err := lookup.GetUserGroupInfo(c.state.Mountpoint, c.config.User, overrides)
	if err != nil {
		return nil, nil, err
	}

	// NewFromSpec() is deprecated according to its comment
	// however the recommended replace just causes a nil map panic
	g := generate.NewFromSpec(c.config.Spec)

	// If the flag to mount all devices is set for a privileged container, add
	// all the devices from the host's machine into the container
	if c.config.MountAllDevices {
		systemdMode := false
		if c.config.Systemd != nil {
			systemdMode = *c.config.Systemd
		}
		if err := util.AddPrivilegedDevices(&g, systemdMode); err != nil {
			return nil, nil, err
		}
	}

	// If network namespace was requested, add it now
	if err := c.addNetworkNamespace(&g); err != nil {
		return nil, nil, err
	}

	// Apply AppArmor checks and load the default profile if needed.
	if len(c.config.Spec.Process.ApparmorProfile) > 0 {
		updatedProfile, err := apparmor.CheckProfileAndLoadDefault(c.config.Spec.Process.ApparmorProfile)
		if err != nil {
			return nil, nil, err
		}
		g.SetProcessApparmorProfile(updatedProfile)
	}

	if err := c.mountNotifySocket(g); err != nil {
		return nil, nil, err
	}

	// Get host UID and GID based on the container process UID and GID.
	hostUID, hostGID, err := butil.GetHostIDs(util.IDtoolsToRuntimeSpec(c.config.IDMappings.UIDMap), util.IDtoolsToRuntimeSpec(c.config.IDMappings.GIDMap), uint32(execUser.Uid), uint32(execUser.Gid))
	if err != nil {
		return nil, nil, err
	}

	// Add named volumes
	for _, namedVol := range c.config.NamedVolumes {
		volume, err := c.runtime.GetVolume(namedVol.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("retrieving volume %s to add to container %s: %w", namedVol.Name, c.ID(), err)
		}
		mountPoint, err := volume.MountPoint()
		if err != nil {
			return nil, nil, err
		}

		if len(namedVol.SubPath) > 0 {
			safeMount, err := c.safeMountSubPath(mountPoint, namedVol.SubPath)
			if err != nil {
				return nil, nil, err
			}
			safeMounts = append(safeMounts, safeMount)

			mountPoint = safeMount.mountPoint
		}

		overlayFlag := false
		upperDir := ""
		workDir := ""
		for _, o := range namedVol.Options {
			if o == "O" {
				overlayFlag = true
				upperDir, workDir, err = getOverlayUpperAndWorkDir(namedVol.Options)
				if err != nil {
					return nil, nil, err
				}
			}
		}

		if overlayFlag {
			var overlayMount spec.Mount
			var overlayOpts *overlay.Options
			contentDir, err := overlay.TempDir(c.config.StaticDir, c.RootUID(), c.RootGID())
			if err != nil {
				return nil, nil, err
			}

			overlayOpts = &overlay.Options{RootUID: c.RootUID(),
				RootGID:                c.RootGID(),
				UpperDirOptionFragment: upperDir,
				WorkDirOptionFragment:  workDir,
				GraphOpts:              c.runtime.store.GraphOptions(),
			}

			overlayMount, err = overlay.MountWithOptions(contentDir, mountPoint, namedVol.Dest, overlayOpts)
			if err != nil {
				return nil, nil, fmt.Errorf("mounting overlay failed %q: %w", mountPoint, err)
			}

			for _, o := range namedVol.Options {
				if o == "U" {
					if err := c.ChangeHostPathOwnership(mountPoint, true, int(hostUID), int(hostGID)); err != nil {
						return nil, nil, err
					}

					if err := c.ChangeHostPathOwnership(contentDir, true, int(hostUID), int(hostGID)); err != nil {
						return nil, nil, err
					}
				}
			}
			g.AddMount(overlayMount)
		} else {
			volMount := spec.Mount{
				Type:        define.TypeBind,
				Source:      mountPoint,
				Destination: namedVol.Dest,
				Options:     namedVol.Options,
			}
			g.AddMount(volMount)
		}
	}

	// Check if the spec file mounts contain the options z, Z, U or idmap.
	// If they have z or Z, relabel the source directory and then remove the option.
	// If they have U, chown the source directory and then remove the option.
	// If they have idmap, then calculate the mappings to use in the OCI config file.
	for i := range g.Config.Mounts {
		m := &g.Config.Mounts[i]
		var options []string
		for _, o := range m.Options {
			if strings.HasPrefix(o, "subpath=") {
				subpath := strings.Split(o, "=")[1]
				safeMount, err := c.safeMountSubPath(m.Source, subpath)
				if err != nil {
					return nil, nil, err
				}
				safeMounts = append(safeMounts, safeMount)
				m.Source = safeMount.mountPoint
				continue
			}
			if o == "idmap" || strings.HasPrefix(o, "idmap=") {
				var err error
				m.UIDMappings, m.GIDMappings, err = parseIDMapMountOption(c.config.IDMappings, o)
				if err != nil {
					return nil, nil, err
				}
				continue
			}
			switch o {
			case "U":
				if m.Type == define.TypeTmpfs {
					options = append(options, []string{fmt.Sprintf("uid=%d", execUser.Uid), fmt.Sprintf("gid=%d", execUser.Gid)}...)
				} else {
					// only chown on initial creation of container
					if err := c.ChangeHostPathOwnership(m.Source, true, int(hostUID), int(hostGID)); err != nil {
						return nil, nil, err
					}
				}
			case "z":
				fallthrough
			case "Z":
				if err := c.relabel(m.Source, c.MountLabel(), label.IsShared(o)); err != nil {
					return nil, nil, err
				}
			case "no-dereference":
				// crun calls the option `copy-symlink`.
				// Podman decided for --no-dereference as many
				// bin-utils tools (e..g, touch, chown, cp) do.
				options = append(options, "copy-symlink")
			case "copy", "nocopy":
				// no real OCI runtime bind mount options, these should already be handled by the named volume mount above
			default:
				options = append(options, o)
			}
		}
		m.Options = options
	}

	c.setProcessLabel(&g)
	c.setMountLabel(&g)

	if c.IsDefaultInfra() || c.IsService() {
		newMount, err := c.prepareCatatonitMount()
		if err != nil {
			return nil, nil, err
		}
		g.AddMount(newMount)
	}

	// Add bind mounts to container
	for dstPath, srcPath := range c.state.BindMounts {
		newMount := spec.Mount{
			Type:        define.TypeBind,
			Source:      srcPath,
			Destination: dstPath,
			Options:     bindOptions,
		}
		if c.IsReadOnly() && (dstPath != "/dev/shm" || !c.config.ReadWriteTmpfs) {
			newMount.Options = append(newMount.Options, "ro", "nosuid", "noexec", "nodev")
		}
		if dstPath == "/dev/shm" && c.state.BindMounts["/dev/shm"] == c.config.ShmDir {
			newMount.Options = append(newMount.Options, "nosuid", "noexec", "nodev")
		}
		if !MountExists(g.Mounts(), dstPath) {
			g.AddMount(newMount)
		} else {
			logrus.Infof("User mount overriding libpod mount at %q", dstPath)
		}
	}

	// Add overlay volumes
	for _, overlayVol := range c.config.OverlayVolumes {
		upperDir, workDir, err := getOverlayUpperAndWorkDir(overlayVol.Options)
		if err != nil {
			return nil, nil, err
		}
		contentDir, err := overlay.TempDir(c.config.StaticDir, c.RootUID(), c.RootGID())
		if err != nil {
			return nil, nil, err
		}
		overlayOpts := &overlay.Options{RootUID: c.RootUID(),
			RootGID:                c.RootGID(),
			UpperDirOptionFragment: upperDir,
			WorkDirOptionFragment:  workDir,
			GraphOpts:              c.runtime.store.GraphOptions(),
		}

		overlayMount, err := overlay.MountWithOptions(contentDir, overlayVol.Source, overlayVol.Dest, overlayOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("mounting overlay failed %q: %w", overlayVol.Source, err)
		}

		// Check overlay volume options
		for _, o := range overlayVol.Options {
			if o == "U" {
				if err := c.ChangeHostPathOwnership(overlayVol.Source, true, int(hostUID), int(hostGID)); err != nil {
					return nil, nil, err
				}

				if err := c.ChangeHostPathOwnership(contentDir, true, int(hostUID), int(hostGID)); err != nil {
					return nil, nil, err
				}
			}
		}

		g.AddMount(overlayMount)
	}

	// Add image volumes as overlay mounts
	for _, volume := range c.config.ImageVolumes {
		// Mount the specified image.
		img, _, err := c.runtime.LibimageRuntime().LookupImage(volume.Source, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("creating image volume %q:%q: %w", volume.Source, volume.Dest, err)
		}
		mountPoint, err := img.Mount(ctx, nil, "")
		if err != nil {
			return nil, nil, fmt.Errorf("mounting image volume %q:%q: %w", volume.Source, volume.Dest, err)
		}

		contentDir, err := overlay.TempDir(c.config.StaticDir, c.RootUID(), c.RootGID())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create TempDir in the %s directory: %w", c.config.StaticDir, err)
		}

		imagePath := mountPoint
		if volume.SubPath != "" {
			safeMount, err := c.safeMountSubPath(mountPoint, volume.SubPath)
			if err != nil {
				return nil, nil, err
			}

			safeMounts = append(safeMounts, safeMount)

			imagePath = safeMount.mountPoint
		}

		var overlayMount spec.Mount
		if volume.ReadWrite {
			overlayMount, err = overlay.Mount(contentDir, imagePath, volume.Dest, c.RootUID(), c.RootGID(), c.runtime.store.GraphOptions())
		} else {
			overlayMount, err = overlay.MountReadOnly(contentDir, imagePath, volume.Dest, c.RootUID(), c.RootGID(), c.runtime.store.GraphOptions())
		}
		if err != nil {
			return nil, nil, fmt.Errorf("creating overlay mount for image %q failed: %w", volume.Source, err)
		}
		g.AddMount(overlayMount)
	}

	if len(c.config.ArtifactVolumes) > 0 {
		artStore, err := c.runtime.ArtifactStore()
		if err != nil {
			return nil, nil, err
		}
		for _, artifactMount := range c.config.ArtifactVolumes {
			paths, err := artStore.BlobMountPaths(ctx, artifactMount.Source, &libartTypes.BlobMountPathOptions{
				FilterBlobOptions: libartTypes.FilterBlobOptions{
					Title:  artifactMount.Title,
					Digest: artifactMount.Digest,
				},
			})
			if err != nil {
				return nil, nil, err
			}

			destIsFile, err := containerPathIsFile(c.state.Mountpoint, artifactMount.Dest)
			// When the file does not exists and the artifact has only a single blob to mount
			// assume it is a file so we use the dest path as direct mount.
			if err != nil && len(paths) == 1 && errors.Is(err, fs.ErrNotExist) {
				destIsFile = true
			}
			if destIsFile && len(paths) > 1 {
				return nil, nil, fmt.Errorf("artifact %q contains more than one blob and container path %q is a file", artifactMount.Source, artifactMount.Dest)
			}

			for i, path := range paths {
				var dest string
				if destIsFile {
					dest = artifactMount.Dest
				} else {
					var filename string
					if artifactMount.Name != "" {
						filename = artifactMount.Name
						if len(paths) > 1 {
							filename += "-" + strconv.Itoa(i)
						}
					} else {
						filename = path.Name
					}
					dest = filepath.Join(artifactMount.Dest, filename)
				}

				logrus.Debugf("Mounting artifact %q in container %s, mount blob %q to %q", artifactMount.Source, c.ID(), path.SourcePath, dest)

				g.AddMount(spec.Mount{
					Destination: dest,
					Source:      path.SourcePath,
					Type:        define.TypeBind,
					// Important: This must always be mounted read only here, we are using
					// the source in the artifact store directly and because that is digest
					// based a write will break the layout.
					Options: []string{define.TypeBind, "ro"},
				})
			}
		}
	}

	err = c.setHomeEnvIfNeeded()
	if err != nil {
		return nil, nil, err
	}

	if c.config.User != "" {
		// User and Group must go together
		g.SetProcessUID(uint32(execUser.Uid))
		g.SetProcessGID(uint32(execUser.Gid))
		g.AddProcessAdditionalGid(uint32(execUser.Gid))
	}

	if c.config.Umask != "" {
		umask, err := c.umask()
		if err != nil {
			return nil, nil, err
		}
		g.Config.Process.User.Umask = &umask
	}

	// Add addition groups if c.config.GroupAdd is not empty
	if len(c.config.Groups) > 0 {
		gids, err := lookup.GetContainerGroups(c.config.Groups, c.state.Mountpoint, overrides)
		if err != nil {
			return nil, nil, fmt.Errorf("looking up supplemental groups for container %s: %w", c.ID(), err)
		}
		for _, gid := range gids {
			g.AddProcessAdditionalGid(gid)
		}
	}

	if err := c.addSystemdMounts(&g); err != nil {
		return nil, nil, err
	}

	// Look up and add groups the user belongs to, if a group wasn't directly specified
	if !strings.Contains(c.config.User, ":") {
		// the gidMappings that are present inside the container user namespace
		var gidMappings []idtools.IDMap

		switch {
		case len(c.config.IDMappings.GIDMap) > 0:
			gidMappings = c.config.IDMappings.GIDMap
		case rootless.IsRootless():
			// Check whether the current user namespace has enough gids available.
			availableGids, err := rootless.GetAvailableGids()
			if err != nil {
				return nil, nil, fmt.Errorf("cannot read number of available GIDs: %w", err)
			}
			gidMappings = []idtools.IDMap{{
				ContainerID: 0,
				HostID:      0,
				Size:        int(availableGids),
			}}
		default:
			gidMappings = []idtools.IDMap{{
				ContainerID: 0,
				HostID:      0,
				Size:        math.MaxInt32,
			}}
		}
		for _, gid := range execUser.Sgids {
			isGIDAvailable := false
			for _, m := range gidMappings {
				if gid >= m.ContainerID && gid < m.ContainerID+m.Size {
					isGIDAvailable = true
					break
				}
			}
			if isGIDAvailable {
				g.AddProcessAdditionalGid(uint32(gid))
			} else {
				logrus.Warnf("Additional gid=%d is not present in the user namespace, skip setting it", gid)
			}
		}
	}

	// Add shared namespaces from other containers
	if err := c.addSharedNamespaces(&g); err != nil {
		return nil, nil, err
	}

	rootPath, err := c.getRootPathForOCI()
	if err != nil {
		return nil, nil, err
	}
	g.SetRootPath(rootPath)
	g.AddAnnotation("org.opencontainers.image.stopSignal", strconv.FormatUint(uint64(c.config.StopSignal), 10))

	if c.config.StopSignal != 0 {
		g.AddAnnotation("org.systemd.property.KillSignal", strconv.FormatUint(uint64(c.config.StopSignal), 10))
	}

	if c.config.StopTimeout != 0 {
		annotation := fmt.Sprintf("uint64 %d", c.config.StopTimeout*1000000) // sec to usec
		g.AddAnnotation("org.systemd.property.TimeoutStopUSec", annotation)
	}

	if _, exists := g.Config.Annotations[annotations.ContainerManager]; !exists {
		g.AddAnnotation(annotations.ContainerManager, annotations.ContainerManagerLibpod)
	}

	if err := c.setCgroupsPath(&g); err != nil {
		return nil, nil, err
	}

	// Warning: CDI may alter g.Config in place.
	if len(c.config.CDIDevices) > 0 {
		registry, err := cdi.NewCache(
			cdi.WithSpecDirs(c.runtime.config.Engine.CdiSpecDirs.Get()...),
			cdi.WithAutoRefresh(false),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("creating CDI registry: %w", err)
		}
		if err := registry.Refresh(); err != nil {
			logrus.Debugf("The following error was triggered when refreshing the CDI registry: %v", err)
		}
		if _, err := registry.InjectDevices(g.Config, c.config.CDIDevices...); err != nil {
			return nil, nil, fmt.Errorf("setting up CDI devices: %w", err)
		}
	}

	// Mounts need to be sorted so paths will not cover other paths
	mounts := sortMounts(g.Mounts())
	g.ClearMounts()

	for _, m := range mounts {
		// We need to remove all symlinks from tmpfs mounts.
		// Runc and other runtimes may choke on them.
		// Easy solution: use securejoin to do a scoped evaluation of
		// the links, then trim off the mount prefix.
		if m.Type == define.TypeTmpfs {
			finalPath, err := securejoin.SecureJoin(c.state.Mountpoint, m.Destination)
			if err != nil {
				return nil, nil, fmt.Errorf("resolving symlinks for mount destination %s: %w", m.Destination, err)
			}
			trimmedPath := strings.TrimPrefix(finalPath, strings.TrimSuffix(c.state.Mountpoint, "/"))
			m.Destination = trimmedPath
		}
		g.AddMount(m)
	}

	if err := c.addRootPropagation(&g, mounts); err != nil {
		return nil, nil, err
	}

	// Warning: precreate hooks may alter g.Config in place.
	if c.state.ExtensionStageHooks, err = c.setupOCIHooks(ctx, g.Config); err != nil {
		return nil, nil, fmt.Errorf("setting up OCI Hooks: %w", err)
	}
	if len(c.config.EnvSecrets) > 0 {
		manager, err := c.runtime.SecretsManager()
		if err != nil {
			return nil, nil, err
		}
		for name, secr := range c.config.EnvSecrets {
			_, data, err := manager.LookupSecretData(secr.Name)
			if err != nil {
				return nil, nil, err
			}
			g.AddProcessEnv(name, string(data))
		}
	}

	// Pass down the LISTEN_* environment (see #10443).
	for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
		if val, ok := os.LookupEnv(key); ok {
			// Force the PID to `1` since we cannot rely on (all
			// versions of) all runtimes to do it for us.
			if key == "LISTEN_PID" {
				val = "1"
			}
			g.AddProcessEnv(key, val)
		}
	}

	// setup rlimits
	nofileSet := false
	nprocSet := false
	isRunningInUserNs := unshare.IsRootless()
	if isRunningInUserNs && g.Config.Process != nil && g.Config.Process.OOMScoreAdj != nil {
		var err error
		*g.Config.Process.OOMScoreAdj, err = maybeClampOOMScoreAdj(*g.Config.Process.OOMScoreAdj)
		if err != nil {
			return nil, nil, err
		}
	}
	for _, rlimit := range c.config.Spec.Process.Rlimits {
		if rlimit.Type == "RLIMIT_NOFILE" {
			nofileSet = true
		}
		if rlimit.Type == "RLIMIT_NPROC" {
			nprocSet = true
		}
	}
	needsClamping := false
	if !nofileSet || !nprocSet {
		needsClamping = isRunningInUserNs
		if !needsClamping {
			has, err := hasCapSysResource()
			if err != nil {
				return nil, nil, err
			}
			needsClamping = !has
		}
	}
	if !nofileSet {
		max := rlimT(define.RLimitDefaultValue)
		current := rlimT(define.RLimitDefaultValue)
		if needsClamping {
			var rlimit unix.Rlimit
			if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rlimit); err != nil {
				logrus.Warnf("Failed to return RLIMIT_NOFILE ulimit %q", err)
			}
			if rlimT(rlimit.Cur) < current {
				current = rlimT(rlimit.Cur)
			}
			if rlimT(rlimit.Max) < max {
				max = rlimT(rlimit.Max)
			}
		}
		g.AddProcessRlimits("RLIMIT_NOFILE", uint64(max), uint64(current))
	}
	if !nprocSet {
		max := rlimT(define.RLimitDefaultValue)
		current := rlimT(define.RLimitDefaultValue)
		if needsClamping {
			var rlimit unix.Rlimit
			if err := unix.Getrlimit(unix.RLIMIT_NPROC, &rlimit); err != nil {
				logrus.Warnf("Failed to return RLIMIT_NPROC ulimit %q", err)
			}
			if rlimT(rlimit.Cur) < current {
				current = rlimT(rlimit.Cur)
			}
			if rlimT(rlimit.Max) < max {
				max = rlimT(rlimit.Max)
			}
		}
		g.AddProcessRlimits("RLIMIT_NPROC", uint64(max), uint64(current))
	}

	c.addMaskedPaths(&g)

	return g.Config, cleanupFunc, nil
}

// resolveWorkDir resolves the container's workdir and, depending on the
// configuration, will create it, or error out if it does not exist.
// Note that the container must be mounted before.
func (c *Container) resolveWorkDir() error {
	workdir := c.WorkingDir()

	// If the specified workdir is a subdir of a volume or mount,
	// we don't need to do anything.  The runtime is taking care of
	// that.
	if isPathOnVolume(c, workdir) || isPathOnMount(c, workdir) {
		logrus.Debugf("Workdir %q resolved to a volume or mount", workdir)
		return nil
	}

	resolvedWorkdir, err := securejoin.SecureJoin(c.state.Mountpoint, workdir)
	if err != nil {
		return err
	}
	logrus.Debugf("Workdir %q resolved to host path %q", workdir, resolvedWorkdir)

	st, err := os.Stat(resolvedWorkdir)
	if err == nil {
		if !st.IsDir() {
			return fmt.Errorf("workdir %q exists on container %s, but is not a directory", workdir, c.ID())
		}
		return nil
	}
	if !c.config.CreateWorkingDir {
		// No need to create it (e.g., `--workdir=/foo`), so let's make sure
		// the path exists on the container.
		if errors.Is(err, os.ErrNotExist) {
			// Check if path is a symlink, securejoin resolves and follows the links
			// so the path will be different from the normal join if it is one.
			if resolvedWorkdir != filepath.Join(c.state.Mountpoint, workdir) {
				// Path must be a symlink to non existing directory.
				// It could point to mounts that are only created later so that make
				// an assumption here and let's just continue and let the oci runtime
				// do its job.
				return nil
			}
			// If they are the same we know there is no symlink/relative path involved.
			// We can return a nicer error message without having to go through the OCI runtime.
			return fmt.Errorf("workdir %q does not exist on container %s", workdir, c.ID())
		}
		// This might be a serious error (e.g., permission), so
		// we need to return the full error.
		return fmt.Errorf("detecting workdir %q on container %s: %w", workdir, c.ID(), err)
	}
	if err := os.MkdirAll(resolvedWorkdir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return fmt.Errorf("creating container %s workdir: %w", c.ID(), err)
	}

	// Ensure container entrypoint is created (if required).
	uid, gid, _, err := chrootuser.GetUser(c.state.Mountpoint, c.User())
	if err != nil {
		return fmt.Errorf("looking up %s inside of the container %s: %w", c.User(), c.ID(), err)
	}
	if err := idtools.SafeChown(resolvedWorkdir, int(uid), int(gid)); err != nil {
		return fmt.Errorf("chowning container %s workdir to container root: %w", c.ID(), err)
	}

	return nil
}

func (c *Container) getUserOverrides() *lookup.Overrides {
	var hasPasswdFile, hasGroupFile bool
	overrides := lookup.Overrides{}
	for _, m := range c.config.Spec.Mounts {
		if m.Destination == "/etc/passwd" {
			overrides.ContainerEtcPasswdPath = m.Source
			hasPasswdFile = true
		}
		if m.Destination == "/etc/group" {
			overrides.ContainerEtcGroupPath = m.Source
			hasGroupFile = true
		}
		if m.Destination == "/etc" {
			if !hasPasswdFile {
				overrides.ContainerEtcPasswdPath = filepath.Join(m.Source, "passwd")
			}
			if !hasGroupFile {
				overrides.ContainerEtcGroupPath = filepath.Join(m.Source, "group")
			}
		}
	}
	if path, ok := c.state.BindMounts["/etc/passwd"]; ok {
		overrides.ContainerEtcPasswdPath = path
	}
	return &overrides
}

func lookupHostUser(name string) (*runcuser.ExecUser, error) {
	var execUser runcuser.ExecUser
	// Look up User on host
	u, err := util.LookupUser(name)
	if err != nil {
		return &execUser, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return &execUser, err
	}

	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return &execUser, err
	}
	execUser.Uid = int(uid)
	execUser.Gid = int(gid)
	execUser.Home = u.HomeDir
	return &execUser, nil
}

// mountNotifySocket mounts the NOTIFY_SOCKET into the container if it's set
// and if the sdnotify mode is set to container.  It also sets c.notifySocket
// to avoid redundantly looking up the env variable.
func (c *Container) mountNotifySocket(g generate.Generator) error {
	if c.config.SdNotifySocket == "" {
		return nil
	}
	if c.config.SdNotifyMode != define.SdNotifyModeContainer {
		return nil
	}

	notifyDir := filepath.Join(c.bundlePath(), "notify")
	logrus.Debugf("Checking notify %q dir", notifyDir)
	if err := os.MkdirAll(notifyDir, 0o755); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("unable to create notify %q dir: %w", notifyDir, err)
		}
	}
	if err := c.relabel(notifyDir, c.MountLabel(), true); err != nil {
		return fmt.Errorf("relabel failed %q: %w", notifyDir, err)
	}
	logrus.Debugf("Add bindmount notify %q dir", notifyDir)
	if _, ok := c.state.BindMounts["/run/notify"]; !ok {
		c.state.BindMounts["/run/notify"] = notifyDir
	}

	// Set the container's notify socket to the proxy socket created by conmon
	g.AddProcessEnv("NOTIFY_SOCKET", "/run/notify/notify.sock")

	return nil
}

func (c *Container) addCheckpointImageMetadata(importBuilder *buildah.Builder) error {
	// Get information about host environment
	hostInfo, err := c.Runtime().hostInfo()
	if err != nil {
		return fmt.Errorf("getting host info: %v", err)
	}

	criuVersion, err := criu.GetCriuVersion()
	if err != nil {
		return fmt.Errorf("getting criu version: %v", err)
	}

	rootfsImageID, rootfsImageName := c.Image()

	// Add image annotations with information about the container and the host.
	// This information is useful to check compatibility before restoring the checkpoint

	checkpointImageAnnotations := map[string]string{
		define.CheckpointAnnotationName:                c.config.Name,
		define.CheckpointAnnotationRawImageName:        c.config.RawImageName,
		define.CheckpointAnnotationRootfsImageID:       rootfsImageID,
		define.CheckpointAnnotationRootfsImageName:     rootfsImageName,
		define.CheckpointAnnotationPodmanVersion:       version.Version.String(),
		define.CheckpointAnnotationCriuVersion:         strconv.Itoa(criuVersion),
		define.CheckpointAnnotationRuntimeName:         hostInfo.OCIRuntime.Name,
		define.CheckpointAnnotationRuntimeVersion:      hostInfo.OCIRuntime.Version,
		define.CheckpointAnnotationConmonVersion:       hostInfo.Conmon.Version,
		define.CheckpointAnnotationHostArch:            hostInfo.Arch,
		define.CheckpointAnnotationHostKernel:          hostInfo.Kernel,
		define.CheckpointAnnotationCgroupVersion:       hostInfo.CgroupsVersion,
		define.CheckpointAnnotationDistributionVersion: hostInfo.Distribution.Version,
		define.CheckpointAnnotationDistributionName:    hostInfo.Distribution.Distribution,
	}

	for key, value := range checkpointImageAnnotations {
		importBuilder.SetAnnotation(key, value)
	}

	return nil
}

func (c *Container) resolveCheckpointImageName(options *ContainerCheckpointOptions) error {
	if options.CreateImage == "" {
		return nil
	}

	// Resolve image name
	resolvedImageName, err := c.runtime.LibimageRuntime().ResolveName(options.CreateImage)
	if err != nil {
		return err
	}

	options.CreateImage = resolvedImageName
	return nil
}

func (c *Container) createCheckpointImage(ctx context.Context, options ContainerCheckpointOptions) error {
	if options.CreateImage == "" {
		return nil
	}
	logrus.Debugf("Create checkpoint image %s", options.CreateImage)

	// Create storage reference
	imageRef, err := is.Transport.ParseStoreReference(c.runtime.store, options.CreateImage)
	if err != nil {
		return errors.New("failed to parse image name")
	}

	// Build an image scratch
	builderOptions := buildah.BuilderOptions{
		FromImage: "scratch",
	}
	importBuilder, err := buildah.NewBuilder(ctx, c.runtime.store, builderOptions)
	if err != nil {
		return err
	}
	// Clean up buildah working container
	defer func() {
		if err := importBuilder.Delete(); err != nil {
			logrus.Errorf("Image builder delete failed: %v", err)
		}
	}()

	if err := c.prepareCheckpointExport(); err != nil {
		return err
	}

	// Export checkpoint into temporary tar file
	tmpDir, err := os.MkdirTemp("", "checkpoint_image_")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	options.TargetFile = path.Join(tmpDir, "checkpoint.tar")

	if err := c.exportCheckpoint(options); err != nil {
		return err
	}

	// Copy checkpoint from temporary tar file in the image
	addAndCopyOptions := buildah.AddAndCopyOptions{}
	if err := importBuilder.Add("", true, addAndCopyOptions, options.TargetFile); err != nil {
		return err
	}

	if err := c.addCheckpointImageMetadata(importBuilder); err != nil {
		return err
	}

	commitOptions := buildah.CommitOptions{
		Squash:        true,
		SystemContext: c.runtime.imageContext,
	}

	// Create checkpoint image
	id, _, _, err := importBuilder.Commit(ctx, imageRef, commitOptions)
	if err != nil {
		return err
	}
	logrus.Debugf("Created checkpoint image: %s", id)
	return nil
}

func (c *Container) exportCheckpoint(options ContainerCheckpointOptions) error {
	if len(c.Dependencies()) == 1 {
		// Check if the dependency is an infra container. If it is we can checkpoint
		// the container out of the Pod.
		if c.config.Pod == "" {
			return errors.New("cannot export checkpoints of containers with dependencies")
		}

		pod, err := c.runtime.state.Pod(c.config.Pod)
		if err != nil {
			return fmt.Errorf("container %s is in pod %s, but pod cannot be retrieved: %w", c.ID(), c.config.Pod, err)
		}
		infraID, err := pod.InfraContainerID()
		if err != nil {
			return fmt.Errorf("cannot retrieve infra container ID for pod %s: %w", c.config.Pod, err)
		}
		if c.Dependencies()[0] != infraID {
			return errors.New("cannot export checkpoints of containers with dependencies")
		}
	}
	if len(c.Dependencies()) > 1 {
		return errors.New("cannot export checkpoints of containers with dependencies")
	}
	logrus.Debugf("Exporting checkpoint image of container %q to %q", c.ID(), options.TargetFile)

	includeFiles := []string{
		"artifacts",
		metadata.DevShmCheckpointTar,
		metadata.ConfigDumpFile,
		metadata.SpecDumpFile,
		metadata.NetworkStatusFile,
		stats.StatsDump,
	}

	if c.LogDriver() == define.KubernetesLogging ||
		c.LogDriver() == define.JSONLogging {
		includeFiles = append(includeFiles, "ctr.log")
	}
	if options.PreCheckPoint {
		includeFiles = append(includeFiles, preCheckpointDir)
	} else {
		includeFiles = append(includeFiles, metadata.CheckpointDirectory)
	}
	// Get root file-system changes included in the checkpoint archive
	var addToTarFiles []string
	if !options.IgnoreRootfs {
		// To correctly track deleted files, let's go through the output of 'podman diff'
		rootFsChanges, err := c.runtime.GetDiff("", c.ID(), define.DiffContainer)
		if err != nil {
			return fmt.Errorf("exporting root file-system diff for %q: %w", c.ID(), err)
		}

		addToTarFiles, err = crutils.CRCreateRootFsDiffTar(&rootFsChanges, c.state.Mountpoint, c.bundlePath())
		if err != nil {
			return err
		}

		includeFiles = append(includeFiles, addToTarFiles...)
	}

	// Folder containing archived volumes that will be included in the export
	expVolDir := filepath.Join(c.bundlePath(), metadata.CheckpointVolumesDirectory)

	// Create an archive for each volume associated with the container
	if !options.IgnoreVolumes {
		if err := os.MkdirAll(expVolDir, 0o700); err != nil {
			return fmt.Errorf("creating volumes export directory %q: %w", expVolDir, err)
		}

		for _, v := range c.config.NamedVolumes {
			volumeTarFilePath := filepath.Join(metadata.CheckpointVolumesDirectory, v.Name+".tar")
			volumeTarFileFullPath := filepath.Join(c.bundlePath(), volumeTarFilePath)

			volumeTarFile, err := os.Create(volumeTarFileFullPath)
			if err != nil {
				return fmt.Errorf("creating %q: %w", volumeTarFileFullPath, err)
			}

			volume, err := c.runtime.GetVolume(v.Name)
			if err != nil {
				return err
			}

			mp, err := volume.MountPoint()
			if err != nil {
				return err
			}
			if mp == "" {
				return fmt.Errorf("volume %s is not mounted, cannot export: %w", volume.Name(), define.ErrInternal)
			}

			input, err := archive.TarWithOptions(mp, &archive.TarOptions{
				Compression:      archive.Uncompressed,
				IncludeSourceDir: true,
			})
			if err != nil {
				return fmt.Errorf("reading volume directory %q: %w", v.Dest, err)
			}

			_, err = io.Copy(volumeTarFile, input)
			if err != nil {
				return err
			}
			volumeTarFile.Close()

			includeFiles = append(includeFiles, volumeTarFilePath)
		}
	}

	input, err := archive.TarWithOptions(c.bundlePath(), &archive.TarOptions{
		Compression:      options.Compression,
		IncludeSourceDir: true,
		IncludeFiles:     includeFiles,
	})

	if err != nil {
		return fmt.Errorf("reading checkpoint directory %q: %w", c.ID(), err)
	}

	outFile, err := os.Create(options.TargetFile)
	if err != nil {
		return fmt.Errorf("creating checkpoint export file %q: %w", options.TargetFile, err)
	}
	defer outFile.Close()

	if err := os.Chmod(options.TargetFile, 0o600); err != nil {
		return err
	}

	_, err = io.Copy(outFile, input)
	if err != nil {
		return err
	}

	for _, file := range addToTarFiles {
		os.Remove(filepath.Join(c.bundlePath(), file))
	}

	if !options.IgnoreVolumes {
		os.RemoveAll(expVolDir)
	}

	return nil
}

func (c *Container) checkpointRestoreSupported(version int) error {
	if err := criu.CheckForCriu(version); err != nil {
		return err
	}
	if !c.ociRuntime.SupportsCheckpoint() {
		return errors.New("configured runtime does not support checkpoint/restore")
	}
	return nil
}

func (c *Container) checkpoint(ctx context.Context, options ContainerCheckpointOptions) (*define.CRIUCheckpointRestoreStatistics, int64, error) {
	if err := c.checkpointRestoreSupported(criu.MinCriuVersion); err != nil {
		return nil, 0, err
	}

	if c.state.State != define.ContainerStateRunning {
		return nil, 0, fmt.Errorf("%q is not running, cannot checkpoint: %w", c.state.State, define.ErrCtrStateInvalid)
	}

	if c.AutoRemove() && options.TargetFile == "" {
		return nil, 0, errors.New("cannot checkpoint containers that have been started with '--rm' unless '--export' is used")
	}

	if err := c.resolveCheckpointImageName(&options); err != nil {
		return nil, 0, err
	}

	if err := crutils.CRCreateFileWithLabel(c.bundlePath(), "dump.log", c.MountLabel()); err != nil {
		return nil, 0, err
	}

	// Setting CheckpointLog early in case there is a failure.
	c.state.CheckpointLog = path.Join(c.bundlePath(), "dump.log")
	c.state.CheckpointPath = c.CheckpointPath()

	runtimeCheckpointDuration, err := c.ociRuntime.CheckpointContainer(c, options)
	if err != nil {
		return nil, 0, err
	}

	// Keep the content of /dev/shm directory
	if c.config.ShmDir != "" && c.state.BindMounts["/dev/shm"] == c.config.ShmDir {
		shmDirTarFileFullPath := filepath.Join(c.bundlePath(), metadata.DevShmCheckpointTar)

		shmDirTarFile, err := os.Create(shmDirTarFileFullPath)
		if err != nil {
			return nil, 0, err
		}
		defer shmDirTarFile.Close()

		input, err := archive.TarWithOptions(c.config.ShmDir, &archive.TarOptions{
			Compression:      archive.Uncompressed,
			IncludeSourceDir: true,
		})
		if err != nil {
			return nil, 0, err
		}

		if _, err = io.Copy(shmDirTarFile, input); err != nil {
			return nil, 0, err
		}
	}

	// Save network.status. This is needed to restore the container with
	// the same IP. Currently limited to one IP address in a container
	// with one interface.
	// FIXME: will this break something?
	if _, err := metadata.WriteJSONFile(c.getNetworkStatus(), c.bundlePath(), metadata.NetworkStatusFile); err != nil {
		return nil, 0, err
	}

	defer c.newContainerEvent(events.Checkpoint)

	// There is a bug from criu: https://github.com/checkpoint-restore/criu/issues/116
	// We have to change the symbolic link from absolute path to relative path
	if options.WithPrevious {
		os.Remove(path.Join(c.CheckpointPath(), "parent"))
		if err := os.Symlink("../pre-checkpoint", path.Join(c.CheckpointPath(), "parent")); err != nil {
			return nil, 0, err
		}
	}

	if options.TargetFile != "" {
		if err := c.exportCheckpoint(options); err != nil {
			return nil, 0, err
		}
	} else {
		if err := c.createCheckpointImage(ctx, options); err != nil {
			return nil, 0, err
		}
	}

	logrus.Debugf("Checkpointed container %s", c.ID())

	if !options.KeepRunning && !options.PreCheckPoint {
		c.state.State = define.ContainerStateStopped
		c.state.Checkpointed = true
		c.state.CheckpointedTime = time.Now()
		c.state.Restored = false
		c.state.RestoredTime = time.Time{}

		// Clean up Storage and Network
		if err := c.cleanup(ctx); err != nil {
			return nil, 0, err
		}
	}

	criuStatistics, err := func() (*define.CRIUCheckpointRestoreStatistics, error) {
		if !options.PrintStats {
			return nil, nil
		}
		statsDirectory, err := os.Open(c.bundlePath())
		if err != nil {
			return nil, fmt.Errorf("not able to open %q: %w", c.bundlePath(), err)
		}

		dumpStatistics, err := stats.CriuGetDumpStats(statsDirectory)
		if err != nil {
			return nil, fmt.Errorf("displaying checkpointing statistics not possible: %w", err)
		}

		return &define.CRIUCheckpointRestoreStatistics{
			FreezingTime: dumpStatistics.GetFreezingTime(),
			FrozenTime:   dumpStatistics.GetFrozenTime(),
			MemdumpTime:  dumpStatistics.GetMemdumpTime(),
			MemwriteTime: dumpStatistics.GetMemwriteTime(),
			PagesScanned: dumpStatistics.GetPagesScanned(),
			PagesWritten: dumpStatistics.GetPagesWritten(),
		}, nil
	}()
	if err != nil {
		return nil, 0, err
	}

	if !options.Keep && !options.PreCheckPoint {
		cleanup := []string{
			"dump.log",
			stats.StatsDump,
			metadata.ConfigDumpFile,
			metadata.SpecDumpFile,
		}
		for _, del := range cleanup {
			file := filepath.Join(c.bundlePath(), del)
			if err := os.Remove(file); err != nil {
				logrus.Debugf("Unable to remove file %s", file)
			}
		}
		// The file has been deleted. Do not mention it.
		c.state.CheckpointLog = ""
	}

	c.state.FinishedTime = time.Now()
	return criuStatistics, runtimeCheckpointDuration, c.save()
}

func (c *Container) generateContainerSpec() error {
	// Make sure the newly created config.json exists on disk

	// NewFromSpec() is deprecated according to its comment
	// however the recommended replace just causes a nil map panic
	g := generate.NewFromSpec(c.config.Spec)

	if err := c.saveSpec(g.Config); err != nil {
		return fmt.Errorf("saving imported container specification for restore failed: %w", err)
	}

	return nil
}

func (c *Container) importCheckpointImage(ctx context.Context, imageID string) error {
	img, _, err := c.Runtime().LibimageRuntime().LookupImage(imageID, nil)
	if err != nil {
		return err
	}

	mountPoint, err := img.Mount(ctx, nil, "")
	defer func() {
		if err := c.unmount(true); err != nil {
			logrus.Errorf("Failed to unmount container: %v", err)
		}
	}()
	if err != nil {
		return err
	}

	// Import all checkpoint files except ConfigDumpFile and SpecDumpFile. We
	// generate new container config files to enable to specifying a new
	// container name.
	checkpoint := []string{
		"artifacts",
		metadata.CheckpointDirectory,
		metadata.CheckpointVolumesDirectory,
		metadata.DevShmCheckpointTar,
		metadata.RootFsDiffTar,
		metadata.DeletedFilesFile,
		metadata.PodOptionsFile,
		metadata.PodDumpFile,
	}

	for _, name := range checkpoint {
		src := filepath.Join(mountPoint, name)
		dst := filepath.Join(c.bundlePath(), name)
		if err := archive.NewDefaultArchiver().CopyWithTar(src, dst); err != nil {
			logrus.Debugf("Can't import '%s' from checkpoint image", name)
		}
	}

	return c.generateContainerSpec()
}

func (c *Container) importCheckpointTar(input string) error {
	if err := crutils.CRImportCheckpointWithoutConfig(c.bundlePath(), input); err != nil {
		return err
	}

	return c.generateContainerSpec()
}

func (c *Container) importPreCheckpoint(input string) error {
	archiveFile, err := os.Open(input)
	if err != nil {
		return fmt.Errorf("failed to open pre-checkpoint archive for import: %w", err)
	}

	defer archiveFile.Close()

	err = archive.Untar(archiveFile, c.bundlePath(), nil)
	if err != nil {
		return fmt.Errorf("unpacking of pre-checkpoint archive %s failed: %w", input, err)
	}
	return nil
}

func (c *Container) restore(ctx context.Context, options ContainerCheckpointOptions) (criuStatistics *define.CRIUCheckpointRestoreStatistics, runtimeRestoreDuration int64, retErr error) {
	minCriuVersion := func() int {
		if options.Pod == "" {
			return criu.MinCriuVersion
		}
		return criu.PodCriuVersion
	}()
	if err := c.checkpointRestoreSupported(minCriuVersion); err != nil {
		return nil, 0, err
	}

	if options.Pod != "" && !crutils.CRRuntimeSupportsPodCheckpointRestore(c.ociRuntime.Path()) {
		return nil, 0, fmt.Errorf("runtime %s does not support pod restore", c.ociRuntime.Path())
	}

	if !c.ensureState(define.ContainerStateConfigured, define.ContainerStateExited) {
		return nil, 0, fmt.Errorf("container %s is running or paused, cannot restore: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	if options.ImportPrevious != "" {
		if err := c.importPreCheckpoint(options.ImportPrevious); err != nil {
			return nil, 0, err
		}
	}

	if options.TargetFile != "" {
		if err := c.importCheckpointTar(options.TargetFile); err != nil {
			return nil, 0, err
		}
	} else if options.CheckpointImageID != "" {
		if err := c.importCheckpointImage(ctx, options.CheckpointImageID); err != nil {
			return nil, 0, err
		}
	}

	// Let's try to stat() CRIU's inventory file. If it does not exist, it makes
	// no sense to try a restore. This is a minimal check if a checkpoint exists.
	if err := fileutils.Exists(filepath.Join(c.CheckpointPath(), "inventory.img")); errors.Is(err, fs.ErrNotExist) {
		return nil, 0, fmt.Errorf("a complete checkpoint for this container cannot be found, cannot restore: %w", err)
	}

	if err := crutils.CRCreateFileWithLabel(c.bundlePath(), "restore.log", c.MountLabel()); err != nil {
		return nil, 0, err
	}

	// Setting RestoreLog early in case there is a failure.
	c.state.RestoreLog = path.Join(c.bundlePath(), "restore.log")
	c.state.CheckpointPath = c.CheckpointPath()

	if options.IgnoreStaticIP || options.IgnoreStaticMAC {
		networks, err := c.networks()
		if err != nil {
			return nil, 0, err
		}

		for net, opts := range networks {
			if options.IgnoreStaticIP {
				opts.StaticIPs = nil
			}
			if options.IgnoreStaticMAC {
				opts.StaticMAC = nil
			}
			if err := c.runtime.state.NetworkModify(c, net, opts); err != nil {
				return nil, 0, fmt.Errorf("failed to rewrite network config: %w", err)
			}
		}
	}

	// Read network configuration from checkpoint
	var netStatus map[string]types.StatusBlock
	_, err := metadata.ReadJSONFile(&netStatus, c.bundlePath(), metadata.NetworkStatusFile)
	if err != nil {
		logrus.Infof("Failed to unmarshal network status, cannot restore the same ip/mac: %v", err)
	}
	// If the restored container should get a new name, the IP address of
	// the container will not be restored. This assumes that if a new name is
	// specified, the container is restored multiple times.
	// TODO: This implicit restoring with or without IP depending on an
	//       unrelated restore parameter (--name) does not seem like the
	//       best solution.
	if err == nil && options.Name == "" && (!options.IgnoreStaticIP || !options.IgnoreStaticMAC) {
		// The file with the network.status does exist. Let's restore the
		// container with the same networks settings as during checkpointing.
		networkOpts, err := c.networks()
		if err != nil {
			return nil, 0, err
		}

		netOpts := make(map[string]types.PerNetworkOptions, len(netStatus))
		for network, perNetOpts := range networkOpts {
			// unset mac and ips before we start adding the ones from the status
			perNetOpts.StaticMAC = nil
			perNetOpts.StaticIPs = nil
			for name, netInt := range netStatus[network].Interfaces {
				perNetOpts.InterfaceName = name
				if !options.IgnoreStaticMAC {
					perNetOpts.StaticMAC = netInt.MacAddress
				}
				if !options.IgnoreStaticIP {
					for _, netAddress := range netInt.Subnets {
						perNetOpts.StaticIPs = append(perNetOpts.StaticIPs, netAddress.IPNet.IP)
					}
				}
				// Normally interfaces have a length of 1, only for some special cni configs we could get more.
				// For now just use the first interface to get the ips this should be good enough for most cases.
				break
			}
			netOpts[network] = perNetOpts
		}
		c.perNetworkOpts = netOpts
	}

	defer func() {
		if retErr != nil {
			if err := c.cleanup(ctx); err != nil {
				logrus.Errorf("Cleaning up container %s: %v", c.ID(), err)
			}
		}
	}()

	if err := c.prepare(); err != nil {
		return nil, 0, err
	}

	// Read config
	jsonPath := filepath.Join(c.bundlePath(), "config.json")
	logrus.Debugf("generate.NewFromFile at %v", jsonPath)
	g, err := generate.NewFromFile(jsonPath)
	if err != nil {
		logrus.Debugf("generate.NewFromFile failed with %v", err)
		return nil, 0, err
	}

	// Restoring from an import means that we are doing migration
	if options.TargetFile != "" || options.CheckpointImageID != "" {
		g.SetRootPath(c.state.Mountpoint)
	}

	// We want to have the same network namespace as before.
	if err := c.addNetworkNamespace(&g); err != nil {
		return nil, 0, err
	}

	if options.Pod != "" {
		// Running in a Pod means that we have to change all namespace settings to
		// the ones from the infrastructure container.
		pod, err := c.runtime.LookupPod(options.Pod)
		if err != nil {
			return nil, 0, fmt.Errorf("pod %q cannot be retrieved: %w", options.Pod, err)
		}

		infraContainer, err := pod.InfraContainer()
		if err != nil {
			return nil, 0, fmt.Errorf("cannot retrieved infra container from pod %q: %w", options.Pod, err)
		}

		infraContainer.lock.Lock()
		if err := infraContainer.syncContainer(); err != nil {
			infraContainer.lock.Unlock()
			return nil, 0, fmt.Errorf("syncing infrastructure container %s status: %w", infraContainer.ID(), err)
		}
		if infraContainer.state.State != define.ContainerStateRunning {
			if err := infraContainer.initAndStart(ctx); err != nil {
				infraContainer.lock.Unlock()
				return nil, 0, fmt.Errorf("starting infrastructure container %s status: %w", infraContainer.ID(), err)
			}
		}
		infraContainer.lock.Unlock()

		if c.config.IPCNsCtr != "" {
			nsPath, err := infraContainer.namespacePath(IPCNS)
			if err != nil {
				return nil, 0, fmt.Errorf("cannot retrieve IPC namespace path for Pod %q: %w", options.Pod, err)
			}
			if err := g.AddOrReplaceLinuxNamespace(string(spec.IPCNamespace), nsPath); err != nil {
				return nil, 0, err
			}
		}

		if c.config.NetNsCtr != "" {
			nsPath, err := infraContainer.namespacePath(NetNS)
			if err != nil {
				return nil, 0, fmt.Errorf("cannot retrieve network namespace path for Pod %q: %w", options.Pod, err)
			}
			if err := g.AddOrReplaceLinuxNamespace(string(spec.NetworkNamespace), nsPath); err != nil {
				return nil, 0, err
			}
		}

		if c.config.PIDNsCtr != "" {
			nsPath, err := infraContainer.namespacePath(PIDNS)
			if err != nil {
				return nil, 0, fmt.Errorf("cannot retrieve PID namespace path for Pod %q: %w", options.Pod, err)
			}
			if err := g.AddOrReplaceLinuxNamespace(string(spec.PIDNamespace), nsPath); err != nil {
				return nil, 0, err
			}
		}

		if c.config.UTSNsCtr != "" {
			nsPath, err := infraContainer.namespacePath(UTSNS)
			if err != nil {
				return nil, 0, fmt.Errorf("cannot retrieve UTS namespace path for Pod %q: %w", options.Pod, err)
			}
			if err := g.AddOrReplaceLinuxNamespace(string(spec.UTSNamespace), nsPath); err != nil {
				return nil, 0, err
			}
		}

		if c.config.CgroupNsCtr != "" {
			nsPath, err := infraContainer.namespacePath(CgroupNS)
			if err != nil {
				return nil, 0, fmt.Errorf("cannot retrieve Cgroup namespace path for Pod %q: %w", options.Pod, err)
			}
			if err := g.AddOrReplaceLinuxNamespace(string(spec.CgroupNamespace), nsPath); err != nil {
				return nil, 0, err
			}
		}
	}

	if err := c.makeBindMounts(); err != nil {
		return nil, 0, err
	}

	if options.TargetFile != "" || options.CheckpointImageID != "" {
		for dstPath, srcPath := range c.state.BindMounts {
			newMount := spec.Mount{
				Type:        define.TypeBind,
				Source:      srcPath,
				Destination: dstPath,
				Options:     []string{define.TypeBind, "private"},
			}
			if c.IsReadOnly() && (dstPath != "/dev/shm" || !c.config.ReadWriteTmpfs) {
				newMount.Options = append(newMount.Options, "ro", "nosuid", "noexec", "nodev")
			}
			if dstPath == "/dev/shm" && c.state.BindMounts["/dev/shm"] == c.config.ShmDir {
				newMount.Options = append(newMount.Options, "nosuid", "noexec", "nodev")
			}
			if !MountExists(g.Mounts(), dstPath) {
				g.AddMount(newMount)
			}
		}
	}

	// Restore /dev/shm content
	if c.config.ShmDir != "" && c.state.BindMounts["/dev/shm"] == c.config.ShmDir {
		shmDirTarFileFullPath := filepath.Join(c.bundlePath(), metadata.DevShmCheckpointTar)
		if err := fileutils.Exists(shmDirTarFileFullPath); err != nil {
			logrus.Debug("Container checkpoint doesn't contain dev/shm: ", err.Error())
		} else {
			shmDirTarFile, err := os.Open(shmDirTarFileFullPath)
			if err != nil {
				return nil, 0, err
			}
			defer shmDirTarFile.Close()

			if err := archive.UntarUncompressed(shmDirTarFile, c.config.ShmDir, nil); err != nil {
				return nil, 0, err
			}
		}
	}

	// Cleanup for a working restore.
	if err := c.removeConmonFiles(); err != nil {
		return nil, 0, err
	}

	// Save the OCI spec to disk
	if err := c.saveSpec(g.Config); err != nil {
		return nil, 0, err
	}

	// When restoring from an imported archive, allow restoring the content of volumes.
	// Volumes are created in setupContainer()
	if !options.IgnoreVolumes && (options.TargetFile != "" || options.CheckpointImageID != "") {
		for _, v := range c.config.NamedVolumes {
			volumeFilePath := filepath.Join(c.bundlePath(), metadata.CheckpointVolumesDirectory, v.Name+".tar")

			volumeFile, err := os.Open(volumeFilePath)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to open volume file %s: %w", volumeFilePath, err)
			}
			defer volumeFile.Close()

			volume, err := c.runtime.GetVolume(v.Name)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to retrieve volume %s: %w", v.Name, err)
			}

			mountPoint, err := volume.MountPoint()
			if err != nil {
				return nil, 0, err
			}
			if mountPoint == "" {
				return nil, 0, fmt.Errorf("unable to import volume %s as it is not mounted: %w", volume.Name(), err)
			}
			if err := archive.UntarUncompressed(volumeFile, mountPoint, nil); err != nil {
				return nil, 0, fmt.Errorf("failed to extract volume %s to %s: %w", volumeFilePath, mountPoint, err)
			}
		}
	}

	// Before actually restarting the container, apply the root file-system changes
	if !options.IgnoreRootfs {
		if err := crutils.CRApplyRootFsDiffTar(c.bundlePath(), c.state.Mountpoint); err != nil {
			return nil, 0, err
		}

		if err := crutils.CRRemoveDeletedFiles(c.ID(), c.bundlePath(), c.state.Mountpoint); err != nil {
			return nil, 0, err
		}
	}

	// setup hosts/resolv.conf files
	// Note this should normally be called after the container is created in the runtime but before it is started.
	// However restore starts the container right away. This means that if we do the call afterwards there is a
	// short interval where the file is still empty. Thus I decided to call it before which makes it not working
	// with PostConfigureNetNS (userns) but as this does not work anyway today so I don't see it as problem.
	if err := c.completeNetworkSetup(); err != nil {
		return nil, 0, fmt.Errorf("complete network setup: %w", err)
	}

	runtimeRestoreDuration, err = c.ociRuntime.CreateContainer(c, &options)
	if err != nil {
		return nil, 0, err
	}

	criuStatistics, err = func() (*define.CRIUCheckpointRestoreStatistics, error) {
		if !options.PrintStats {
			return nil, nil
		}
		statsDirectory, err := os.Open(c.bundlePath())
		if err != nil {
			return nil, fmt.Errorf("not able to open %q: %w", c.bundlePath(), err)
		}

		restoreStatistics, err := stats.CriuGetRestoreStats(statsDirectory)
		if err != nil {
			return nil, fmt.Errorf("displaying restore statistics not possible: %w", err)
		}

		return &define.CRIUCheckpointRestoreStatistics{
			PagesCompared:   restoreStatistics.GetPagesCompared(),
			PagesSkippedCow: restoreStatistics.GetPagesSkippedCow(),
			ForkingTime:     restoreStatistics.GetForkingTime(),
			RestoreTime:     restoreStatistics.GetRestoreTime(),
			PagesRestored:   restoreStatistics.GetPagesRestored(),
		}, nil
	}()
	if err != nil {
		return nil, 0, err
	}

	logrus.Debugf("Restored container %s", c.ID())

	c.state.State = define.ContainerStateRunning
	c.state.Checkpointed = false
	c.state.Restored = true
	c.state.CheckpointedTime = time.Time{}
	c.state.RestoredTime = time.Now()

	if !options.Keep {
		// Delete all checkpoint related files. At this point, in theory, all files
		// should exist. Still ignoring errors for now as the container should be
		// restored and running. Not erroring out just because some cleanup operation
		// failed. Starting with the checkpoint directory
		err = os.RemoveAll(c.CheckpointPath())
		if err != nil {
			logrus.Debugf("Non-fatal: removal of checkpoint directory (%s) failed: %v", c.CheckpointPath(), err)
		}
		c.state.CheckpointPath = ""
		err = os.RemoveAll(c.PreCheckPointPath())
		if err != nil {
			logrus.Debugf("Non-fatal: removal of pre-checkpoint directory (%s) failed: %v", c.PreCheckPointPath(), err)
		}
		err = os.RemoveAll(c.CheckpointVolumesPath())
		if err != nil {
			logrus.Debugf("Non-fatal: removal of checkpoint volumes directory (%s) failed: %v", c.CheckpointVolumesPath(), err)
		}
		cleanup := [...]string{
			"restore.log",
			"dump.log",
			stats.StatsDump,
			stats.StatsRestore,
			metadata.DevShmCheckpointTar,
			metadata.NetworkStatusFile,
			metadata.RootFsDiffTar,
			metadata.DeletedFilesFile,
		}
		for _, del := range cleanup {
			file := filepath.Join(c.bundlePath(), del)
			err = os.Remove(file)
			if err != nil {
				logrus.Debugf("Non-fatal: removal of checkpoint file (%s) failed: %v", file, err)
			}
		}
		c.state.CheckpointLog = ""
		c.state.RestoreLog = ""
	}

	return criuStatistics, runtimeRestoreDuration, c.save()
}

// Retrieves a container's "root" net namespace container dependency.
func (c *Container) getRootNetNsDepCtr() (depCtr *Container, err error) {
	containersVisited := map[string]int{c.config.ID: 1}
	nextCtr := c.config.NetNsCtr
	for nextCtr != "" {
		// Make sure we aren't in a loop
		if _, visited := containersVisited[nextCtr]; visited {
			return nil, errors.New("loop encountered while determining net namespace container")
		}
		containersVisited[nextCtr] = 1

		depCtr, err = c.runtime.state.Container(nextCtr)
		if err != nil {
			return nil, fmt.Errorf("fetching dependency %s of container %s: %w", c.config.NetNsCtr, c.ID(), err)
		}
		// This should never happen without an error
		if depCtr == nil {
			break
		}
		nextCtr = depCtr.config.NetNsCtr
	}

	if depCtr == nil {
		return nil, errors.New("unexpected error depCtr is nil without reported error from runtime state")
	}
	return depCtr, nil
}

// Ensure standard bind mounts are mounted into all root directories (including chroot directories)
func (c *Container) mountIntoRootDirs(mountName string, mountPath string) error {
	c.state.BindMounts[mountName] = mountPath

	for _, chrootDir := range c.config.ChrootDirs {
		c.state.BindMounts[filepath.Join(chrootDir, mountName)] = mountPath
	}

	return nil
}

// Make standard bind mounts to include in the container
func (c *Container) makeBindMounts() error {
	if c.state.BindMounts == nil {
		c.state.BindMounts = make(map[string]string)
	}
	netDisabled, err := c.NetworkDisabled()
	if err != nil {
		return err
	}

	if !netDisabled {
		// If /etc/resolv.conf and /etc/hosts exist, delete them so we
		// will recreate. Only do this if we aren't sharing them with
		// another container.
		if c.config.NetNsCtr == "" {
			if resolvePath, ok := c.state.BindMounts[resolvconf.DefaultResolvConf]; ok {
				if err := os.Remove(resolvePath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("container %s: %w", c.ID(), err)
				}
				delete(c.state.BindMounts, resolvconf.DefaultResolvConf)
			}
			if hostsPath, ok := c.state.BindMounts[config.DefaultHostsFile]; ok {
				if err := os.Remove(hostsPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("container %s: %w", c.ID(), err)
				}
				delete(c.state.BindMounts, config.DefaultHostsFile)
			}
		}

		if c.config.NetNsCtr != "" && (!c.config.UseImageResolvConf || !c.config.UseImageHosts) {
			// We share a net namespace.
			// We want /etc/resolv.conf and /etc/hosts from the
			// other container. Unless we're not creating both of
			// them.
			depCtr, err := c.getRootNetNsDepCtr()
			if err != nil {
				return fmt.Errorf("fetching network namespace dependency container for container %s: %w", c.ID(), err)
			}

			// We need that container's bind mounts
			bindMounts, err := depCtr.BindMounts()
			if err != nil {
				return fmt.Errorf("fetching bind mounts from dependency %s of container %s: %w", depCtr.ID(), c.ID(), err)
			}

			// The other container may not have a resolv.conf or /etc/hosts
			// If it doesn't, don't copy them
			resolvPath, exists := bindMounts[resolvconf.DefaultResolvConf]
			if !c.config.UseImageResolvConf && exists {
				err := c.mountIntoRootDirs(resolvconf.DefaultResolvConf, resolvPath)

				if err != nil {
					return fmt.Errorf("assigning mounts to container %s: %w", c.ID(), err)
				}
			}

			// check if dependency container has an /etc/hosts file.
			// It may not have one, so only use it if it does.
			hostsPath, exists := bindMounts[config.DefaultHostsFile]
			if !c.config.UseImageHosts && exists {
				// we cannot use the dependency container lock due ABBA deadlocks in cleanup()
				lock, err := lockfile.GetLockFile(hostsPath)
				if err != nil {
					return fmt.Errorf("failed to lock hosts file: %w", err)
				}
				lock.Lock()

				// add the newly added container to the hosts file
				// we always use 127.0.0.1 as ip since they have the same netns
				err = etchosts.Add(hostsPath, getLocalhostHostEntry(c))
				lock.Unlock()
				if err != nil {
					return fmt.Errorf("creating hosts file for container %s which depends on container %s: %w", c.ID(), depCtr.ID(), err)
				}

				// finally, save it in the new container
				err = c.mountIntoRootDirs(config.DefaultHostsFile, hostsPath)
				if err != nil {
					return fmt.Errorf("assigning mounts to container %s: %w", c.ID(), err)
				}
			}
		} else {
			if !c.config.UseImageResolvConf {
				if err := c.createResolvConf(); err != nil {
					return fmt.Errorf("creating resolv.conf for container %s: %w", c.ID(), err)
				}
			}

			if !c.config.UseImageHosts {
				if err := c.createHostsFile(); err != nil {
					return fmt.Errorf("creating hosts file for container %s: %w", c.ID(), err)
				}
			}
		}

		if c.state.BindMounts[config.DefaultHostsFile] != "" {
			if err := c.relabel(c.state.BindMounts[config.DefaultHostsFile], c.config.MountLabel, true); err != nil {
				return err
			}
		}

		if c.state.BindMounts[resolvconf.DefaultResolvConf] != "" {
			if err := c.relabel(c.state.BindMounts[resolvconf.DefaultResolvConf], c.config.MountLabel, true); err != nil {
				return err
			}
		}
	} else if !c.config.UseImageHosts && c.state.BindMounts[config.DefaultHostsFile] == "" {
		if err := c.createHostsFile(); err != nil {
			return fmt.Errorf("creating hosts file for container %s: %w", c.ID(), err)
		}
	}

	if c.config.ShmDir != "" {
		// If ShmDir has a value SHM is always added when we mount the container
		c.state.BindMounts["/dev/shm"] = c.config.ShmDir
	}

	if c.config.Passwd == nil || *c.config.Passwd {
		newPasswd, newGroup, err := c.generatePasswdAndGroup()
		if err != nil {
			return fmt.Errorf("creating temporary passwd file for container %s: %w", c.ID(), err)
		}
		if newPasswd != "" {
			// Make /etc/passwd
			// If it already exists, delete so we can recreate
			delete(c.state.BindMounts, "/etc/passwd")
			c.state.BindMounts["/etc/passwd"] = newPasswd
		}
		if newGroup != "" {
			// Make /etc/group
			// If it already exists, delete so we can recreate
			delete(c.state.BindMounts, "/etc/group")
			c.state.BindMounts["/etc/group"] = newGroup
		}
	}

	runPath, err := c.getPlatformRunPath()
	if err != nil {
		return fmt.Errorf("cannot determine run directory for container: %w", err)
	}
	containerenvPath := filepath.Join(runPath, ".containerenv")

	_, hasRunContainerenv := c.state.BindMounts[containerenvPath]
	if !hasRunContainerenv {
	Loop:
		// check in the spec mounts
		for _, m := range c.config.Spec.Mounts {
			switch {
			case m.Destination == containerenvPath:
				hasRunContainerenv = true
				break Loop
			case m.Destination == runPath && m.Type != define.TypeTmpfs:
				hasRunContainerenv = true
				break Loop
			}
		}
	}

	// Make .containerenv if it does not exist
	if !hasRunContainerenv {
		containerenv := c.runtime.graphRootMountedFlag(c.config.Spec.Mounts)
		isRootless := 0
		if rootless.IsRootless() {
			isRootless = 1
		}
		imageID, imageName := c.Image()

		if c.Privileged() {
			// Populate the .containerenv with container information
			containerenv = fmt.Sprintf(`engine="podman-%s"
name=%q
id=%q
image=%q
imageid=%q
rootless=%d
%s`, version.Version.String(), c.Name(), c.ID(), imageName, imageID, isRootless, containerenv)
		}
		containerenvHostPath, err := c.writeStringToRundir(".containerenv", containerenv)
		if err != nil {
			return fmt.Errorf("creating containerenv file for container %s: %w", c.ID(), err)
		}
		c.state.BindMounts[containerenvPath] = containerenvHostPath
	}

	// Add Subscription Mounts
	subscriptionMounts := subscriptions.MountsWithUIDGID(c.config.MountLabel, c.state.RunDir, c.runtime.config.Containers.DefaultMountsFile, c.state.Mountpoint, c.RootUID(), c.RootGID(), rootless.IsRootless(), false)
	for _, mount := range subscriptionMounts {
		if _, ok := c.state.BindMounts[mount.Destination]; !ok {
			c.state.BindMounts[mount.Destination] = mount.Source
		}
	}

	// Secrets are mounted by getting the secret data from the secrets manager,
	// copying the data into the container's static dir,
	// then mounting the copied dir into /run/secrets.
	// The secrets mounting must come after subscription mounts, since subscription mounts
	// creates the /run/secrets dir in the container where we mount as well.
	if len(c.Secrets()) > 0 {
		// create /run/secrets if subscriptions did not create
		if err := c.createSecretMountDir(runPath); err != nil {
			return fmt.Errorf("creating secrets mount: %w", err)
		}
		for _, secret := range c.Secrets() {
			secretFileName := secret.Name
			base := filepath.Join(runPath, "secrets")
			if secret.Target != "" {
				secretFileName = secret.Target
				// If absolute path for target given remove base.
				if filepath.IsAbs(secretFileName) {
					base = ""
				}
			}
			src := filepath.Join(c.config.SecretsPath, secret.Name)
			dest := filepath.Join(base, secretFileName)
			c.state.BindMounts[dest] = src
		}
	}

	return c.makeHostnameBindMount()
}

// createResolvConf create the resolv.conf file and bind mount it
func (c *Container) createResolvConf() error {
	destPath := filepath.Join(c.state.RunDir, "resolv.conf")
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	f.Close()
	return c.bindMountRootFile(destPath, resolvconf.DefaultResolvConf)
}

// addResolvConf add resolv.conf entries
func (c *Container) addResolvConf() error {
	destPath, ok := c.state.BindMounts[resolvconf.DefaultResolvConf]
	if !ok {
		// no resolv.conf mount, do nothing
		return nil
	}

	var (
		networkNameServers   []string
		networkSearchDomains []string
	)

	netStatus := c.getNetworkStatus()
	for _, status := range netStatus {
		if status.DNSServerIPs != nil {
			for _, nsIP := range status.DNSServerIPs {
				networkNameServers = append(networkNameServers, nsIP.String())
			}
			logrus.Debugf("Adding nameserver(s) from network status of '%q'", status.DNSServerIPs)
		}
		if status.DNSSearchDomains != nil {
			networkSearchDomains = append(networkSearchDomains, status.DNSSearchDomains...)
			logrus.Debugf("Adding search domain(s) from network status of '%q'", status.DNSSearchDomains)
		}
	}

	ipv6 := c.checkForIPv6(netStatus)

	networkBackend := c.runtime.config.Network.NetworkBackend
	nameservers := make([]string, 0, len(c.runtime.config.Containers.DNSServers.Get())+len(c.config.DNSServer))

	// If NetworkBackend is `netavark` do not populate `/etc/resolv.conf`
	// with custom dns server since after https://github.com/containers/netavark/pull/452
	// netavark will always set required `nameservers` in StatusBlock and libpod
	// will correctly populate `networkNameServers`. Also see https://github.com/containers/podman/issues/16172

	// Exception: Populate `/etc/resolv.conf` if container is not connected to any network
	// with dns enabled then we do not get any nameservers back.
	if networkBackend != string(types.Netavark) || len(networkNameServers) == 0 {
		nameservers = append(nameservers, c.runtime.config.Containers.DNSServers.Get()...)
		for _, ip := range c.config.DNSServer {
			nameservers = append(nameservers, ip.String())
		}
	}
	// If the user provided dns, it trumps all; then dns masq; then resolv.conf
	keepHostServers := false
	if len(nameservers) == 0 {
		// when no network name servers or not netavark use host servers
		// for aardvark dns we only want our single server in there
		if len(networkNameServers) == 0 || networkBackend != string(types.Netavark) {
			keepHostServers = true
		}
		if len(networkNameServers) > 0 {
			// add the nameservers from the networks status
			nameservers = networkNameServers
		} else {
			// pasta and slirp4netns have a built in DNS forwarder.
			nameservers = c.addSpecialDNS(nameservers)
		}
	}

	// Set DNS search domains
	var search []string
	keepHostSearches := false
	if len(c.config.DNSSearch) > 0 || len(c.runtime.config.Containers.DNSSearches.Get()) > 0 {
		customSearch := make([]string, 0, len(c.config.DNSSearch)+len(c.runtime.config.Containers.DNSSearches.Get()))
		customSearch = append(customSearch, c.runtime.config.Containers.DNSSearches.Get()...)
		customSearch = append(customSearch, c.config.DNSSearch...)
		search = customSearch
	} else {
		search = networkSearchDomains
		keepHostSearches = true
	}

	options := make([]string, 0, len(c.config.DNSOption)+len(c.runtime.config.Containers.DNSOptions.Get()))
	options = append(options, c.runtime.config.Containers.DNSOptions.Get()...)
	options = append(options, c.config.DNSOption...)

	var namespaces []spec.LinuxNamespace
	if c.config.Spec.Linux != nil {
		namespaces = c.config.Spec.Linux.Namespaces
	}

	if err := resolvconf.New(&resolvconf.Params{
		IPv6Enabled:      ipv6,
		KeepHostServers:  keepHostServers,
		KeepHostSearches: keepHostSearches,
		Nameservers:      nameservers,
		Namespaces:       namespaces,
		Options:          options,
		Path:             destPath,
		Searches:         search,
	}); err != nil {
		return fmt.Errorf("building resolv.conf for container %s: %w", c.ID(), err)
	}

	return nil
}

// Check if a container uses IPv6.
func (c *Container) checkForIPv6(netStatus map[string]types.StatusBlock) bool {
	for _, status := range netStatus {
		for _, netInt := range status.Interfaces {
			for _, netAddress := range netInt.Subnets {
				// Note: only using To16() does not work since it also returns a valid ip for ipv4
				if netAddress.IPNet.IP.To4() == nil && netAddress.IPNet.IP.To16() != nil {
					return true
				}
			}
		}
	}

	if c.pastaResult != nil {
		return c.pastaResult.IPv6
	}

	return c.isSlirp4netnsIPv6()
}

// Add a new nameserver to the container's resolv.conf, ensuring that it is the
// first nameserver present.
// Usable only with running containers.
func (c *Container) addNameserver(ips []string) error {
	// Take no action if container is not running.
	if !c.ensureState(define.ContainerStateRunning, define.ContainerStateCreated) {
		return nil
	}

	// Do we have a resolv.conf at all?
	path, ok := c.state.BindMounts[resolvconf.DefaultResolvConf]
	if !ok {
		return nil
	}

	if err := resolvconf.Add(path, ips); err != nil {
		return fmt.Errorf("adding new nameserver to container %s resolv.conf: %w", c.ID(), err)
	}

	return nil
}

// Remove an entry from the existing resolv.conf of the container.
// Usable only with running containers.
func (c *Container) removeNameserver(ips []string) error {
	// Take no action if container is not running.
	if !c.ensureState(define.ContainerStateRunning, define.ContainerStateCreated) {
		return nil
	}

	// Do we have a resolv.conf at all?
	path, ok := c.state.BindMounts[resolvconf.DefaultResolvConf]
	if !ok {
		return nil
	}

	if err := resolvconf.Remove(path, ips); err != nil {
		return fmt.Errorf("removing nameservers from container %s resolv.conf: %w", c.ID(), err)
	}

	return nil
}

func getLocalhostHostEntry(c *Container) etchosts.HostEntries {
	return etchosts.HostEntries{{IP: "127.0.0.1", Names: []string{c.Hostname(), c.config.Name}}}
}

// getHostsEntries returns the container ip host entries for the correct netmode
func (c *Container) getHostsEntries() (etchosts.HostEntries, error) {
	var entries etchosts.HostEntries
	names := []string{c.Hostname(), c.config.Name}
	switch {
	case c.config.NetMode.IsBridge():
		entries = etchosts.GetNetworkHostEntries(c.state.NetworkStatus, names...)
	case c.config.NetMode.IsPasta():
		// this should never be the case but check just to be sure and not panic
		if len(c.pastaResult.IPAddresses) > 0 {
			entries = etchosts.HostEntries{{IP: c.pastaResult.IPAddresses[0].String(), Names: names}}
		}
	case c.config.NetMode.IsSlirp4netns():
		ip, err := getSlirp4netnsIP(c.slirp4netnsSubnet)
		if err != nil {
			return nil, err
		}
		entries = etchosts.HostEntries{{IP: ip.String(), Names: names}}
	default:
		if c.hasNetNone() {
			entries = etchosts.HostEntries{{IP: "127.0.0.1", Names: names}}
		}
	}
	return entries, nil
}

func (c *Container) createHostsFile() error {
	targetFile := filepath.Join(c.state.RunDir, "hosts")
	f, err := os.Create(targetFile)
	if err != nil {
		return err
	}
	f.Close()
	return c.bindMountRootFile(targetFile, config.DefaultHostsFile)
}

func (c *Container) addHosts() error {
	targetFile, ok := c.state.BindMounts[config.DefaultHostsFile]
	if !ok {
		// no host file nothing to do
		return nil
	}
	containerIPsEntries, err := c.getHostsEntries()
	if err != nil {
		return fmt.Errorf("failed to get container ip host entries: %w", err)
	}

	// Consider container level BaseHostsFile configuration first.
	// If it is empty, fallback to containers.conf level configuration.
	baseHostsFileConf := c.config.BaseHostsFile
	if baseHostsFileConf == "" {
		baseHostsFileConf = c.runtime.config.Containers.BaseHostsFile
	}
	baseHostFile, err := etchosts.GetBaseHostFile(baseHostsFileConf, c.state.Mountpoint)
	if err != nil {
		return err
	}

	var exclude []net.IP
	var preferIP string
	if c.pastaResult != nil {
		exclude = c.pastaResult.IPAddresses
		if len(c.pastaResult.MapGuestAddrIPs) > 0 {
			// we used --map-guest-addr to setup pasta so prefer this address
			preferIP = c.pastaResult.MapGuestAddrIPs[0]
		}
	} else if c.config.NetMode.IsBridge() {
		// When running rootless we have to check the rootless netns ip addresses
		// to not assign a ip that is already used in the rootless netns as it would
		// not be routed to the host.
		// https://github.com/containers/podman/issues/22653
		info, err := c.runtime.network.RootlessNetnsInfo()
		if err == nil && info != nil {
			exclude = info.IPAddresses
			if len(info.MapGuestIps) > 0 {
				// we used --map-guest-addr to setup pasta so prefer this address
				preferIP = info.MapGuestIps[0]
			}
		}
	}

	hostContainersInternalIP := etchosts.GetHostContainersInternalIP(etchosts.HostContainersInternalOptions{
		Conf:             c.runtime.config,
		NetStatus:        c.state.NetworkStatus,
		NetworkInterface: c.runtime.network,
		Exclude:          exclude,
		PreferIP:         preferIP,
	})

	return etchosts.New(&etchosts.Params{
		BaseFile:                 baseHostFile,
		ExtraHosts:               c.config.HostAdd,
		ContainerIPs:             containerIPsEntries,
		HostContainersInternalIP: hostContainersInternalIP,
		TargetFile:               targetFile,
	})
}

// bindMountRootFile will chown and relabel the source file to make it usable in the container.
// It will also add the path to the container bind mount map.
// source is the path on the host, dest is the path in the container.
func (c *Container) bindMountRootFile(source, dest string) error {
	if err := idtools.SafeChown(source, c.RootUID(), c.RootGID()); err != nil {
		return err
	}
	if err := c.relabel(source, c.MountLabel(), false); err != nil {
		return err
	}

	return c.mountIntoRootDirs(dest, source)
}

// generateGroupEntry generates an entry or entries into /etc/group as
// required by container configuration.
// Generally speaking, we will make an entry under two circumstances:
//  1. The container is started as a specific user:group, and that group is both
//     numeric, and does not already exist in /etc/group.
//  2. It is requested that Libpod add the group that launched Podman to
//     /etc/group via AddCurrentUserPasswdEntry (though this does not trigger if
//     the group in question already exists in /etc/passwd).
//
// Returns group entry (as a string that can be appended to /etc/group) and any
// error that occurred.
func (c *Container) generateGroupEntry() (string, error) {
	groupString := ""

	// Things we *can't* handle: adding the user we added in
	// generatePasswdEntry to any *existing* groups.
	addedGID := -1
	if c.config.AddCurrentUserPasswdEntry {
		entry, gid, err := c.generateCurrentUserGroupEntry()
		if err != nil {
			return "", err
		}
		groupString += entry
		addedGID = gid
	}
	if c.config.User != "" || c.config.GroupEntry != "" {
		entry, err := c.generateUserGroupEntry(addedGID)
		if err != nil {
			return "", err
		}
		groupString += entry
	}

	return groupString, nil
}

// Make an entry in /etc/group for the group of the user running podman iff we
// are rootless.
func (c *Container) generateCurrentUserGroupEntry() (string, int, error) {
	gid := rootless.GetRootlessGID()
	if gid == 0 {
		return "", 0, nil
	}

	g, err := user.LookupGroupId(strconv.Itoa(gid))
	if err != nil {
		return "", 0, fmt.Errorf("failed to get current group: %w", err)
	}

	// Look up group name to see if it exists in the image.
	_, err = lookup.GetGroup(c.state.Mountpoint, g.Name)
	if err != runcuser.ErrNoGroupEntries {
		return "", 0, err
	}

	// Look up GID to see if it exists in the image.
	_, err = lookup.GetGroup(c.state.Mountpoint, g.Gid)
	if err != runcuser.ErrNoGroupEntries {
		return "", 0, err
	}

	// We need to get the username of the rootless user so we can add it to
	// the group.
	username := ""
	uid := rootless.GetRootlessUID()
	if uid != 0 {
		u, err := user.LookupId(strconv.Itoa(uid))
		if err != nil {
			return "", 0, fmt.Errorf("failed to get current user to make group entry: %w", err)
		}
		username = u.Username
	}

	// Make the entry.
	return fmt.Sprintf("%s:x:%s:%s\n", g.Name, g.Gid, username), gid, nil
}

// Make an entry in /etc/group for the group the container was specified to run
// as.
func (c *Container) generateUserGroupEntry(addedGID int) (string, error) {
	if c.config.User == "" && c.config.GroupEntry == "" {
		return "", nil
	}

	splitUser := strings.SplitN(c.config.User, ":", 2)
	group := "0"
	if len(splitUser) > 1 {
		group = splitUser[1]
	}

	gid, err := strconv.ParseUint(group, 10, 32)
	if err != nil {
		return "", nil //nolint: nilerr
	}

	if addedGID != -1 && addedGID == int(gid) {
		return "", nil
	}

	// Check if the group already exists
	g, err := lookup.GetGroup(c.state.Mountpoint, group)
	if err != runcuser.ErrNoGroupEntries {
		return "", err
	}

	if c.config.GroupEntry != "" {
		return c.groupEntry(g.Name, strconv.Itoa(g.Gid), g.List), nil
	}

	return fmt.Sprintf("%d:x:%d:%s\n", gid, gid, splitUser[0]), nil
}

func (c *Container) groupEntry(groupname, gid string, list []string) string {
	s := c.config.GroupEntry
	s = strings.ReplaceAll(s, "$GROUPNAME", groupname)
	s = strings.ReplaceAll(s, "$GID", gid)
	s = strings.ReplaceAll(s, "$USERLIST", strings.Join(list, ","))
	return s + "\n"
}

// generatePasswdEntry generates an entry or entries into /etc/passwd as
// required by container configuration.
// Generally speaking, we will make an entry under two circumstances:
//  1. The container is started as a specific user who is not in /etc/passwd.
//     This only triggers if the user is given as a *numeric* ID.
//  2. It is requested that Libpod add the user that launched Podman to
//     /etc/passwd via AddCurrentUserPasswdEntry (though this does not trigger if
//     the user in question already exists in /etc/passwd) or the UID to be added
//     is 0).
//  3. The user specified additional host user accounts to add to the /etc/passwd file
//
// Returns password entry (as a string that can be appended to /etc/passwd) and
// any error that occurred.
func (c *Container) generatePasswdEntry() (string, error) {
	passwdString := ""

	addedUID := 0
	for _, userid := range c.config.HostUsers {
		// Look up User on host
		u, err := util.LookupUser(userid)
		if err != nil {
			return "", err
		}
		entry, err := c.userPasswdEntry(u)
		if err != nil {
			return "", err
		}
		passwdString += entry
	}
	if c.config.AddCurrentUserPasswdEntry {
		entry, uid, _, err := c.generateCurrentUserPasswdEntry()
		if err != nil {
			return "", err
		}
		passwdString += entry
		addedUID = uid
	}
	if c.config.User != "" {
		entry, err := c.generateUserPasswdEntry(addedUID)
		if err != nil {
			return "", err
		}
		passwdString += entry
	}

	return passwdString, nil
}

// generateCurrentUserPasswdEntry generates an /etc/passwd entry for the user
// running the container engine.
// Returns a passwd entry for the user, and the UID and GID of the added entry.
func (c *Container) generateCurrentUserPasswdEntry() (string, int, int, error) {
	uid := rootless.GetRootlessUID()
	if uid == 0 {
		return "", 0, 0, nil
	}

	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to get current user: %w", err)
	}
	pwd, err := c.userPasswdEntry(u)
	if err != nil {
		return "", 0, 0, err
	}

	return pwd, uid, rootless.GetRootlessGID(), nil
}

// Sets the HOME env. variable with precedence: existing home env. variable, execUser home
func (c *Container) setHomeEnvIfNeeded() error {
	getExecUserHome := func() (string, error) {
		overrides := c.getUserOverrides()
		execUser, err := lookup.GetUserGroupInfo(c.state.Mountpoint, c.config.User, overrides)
		if err != nil {
			if slices.Contains(c.config.HostUsers, c.config.User) {
				execUser, err = lookupHostUser(c.config.User)
			}

			if err != nil {
				return "", err
			}
		}

		return execUser.Home, nil
	}

	// Ensure HOME is not already set in Env
	for _, s := range c.config.Spec.Process.Env {
		if strings.HasPrefix(s, "HOME=") {
			return nil
		}
	}

	home, err := getExecUserHome()
	if err != nil {
		return err
	}

	c.config.Spec.Process.Env = append(c.config.Spec.Process.Env, fmt.Sprintf("HOME=%s", home))
	return nil
}

func (c *Container) userPasswdEntry(u *user.User) (string, error) {
	// Look up the user to see if it exists in the container image.
	_, err := lookup.GetUser(c.state.Mountpoint, u.Username)
	if err != runcuser.ErrNoPasswdEntries {
		return "", err
	}

	// Look up the UID to see if it exists in the container image.
	_, err = lookup.GetUser(c.state.Mountpoint, u.Uid)
	if err != runcuser.ErrNoPasswdEntries {
		return "", err
	}

	// If the user's actual home directory exists, or was mounted in - use
	// that.
	homeDir := c.WorkingDir()
	hDir := u.HomeDir
	for hDir != "/" {
		if MountExists(c.config.Spec.Mounts, hDir) {
			homeDir = u.HomeDir
			break
		}
		hDir = filepath.Dir(hDir)
	}
	if homeDir != u.HomeDir {
		if slices.Contains(c.UserVolumes(), u.HomeDir) {
			homeDir = u.HomeDir
		}
	}

	if c.config.PasswdEntry != "" {
		return c.passwdEntry(u.Username, u.Uid, u.Gid, u.Name, homeDir), nil
	}

	return fmt.Sprintf("%s:*:%s:%s:%s:%s:/bin/sh\n", u.Username, u.Uid, u.Gid, u.Name, homeDir), nil
}

// generateUserPasswdEntry generates an /etc/passwd entry for the container user
// to run in the container.
// The UID and GID of the added entry will also be returned.
// Accepts one argument, that being any UID that has already been added to the
// passwd file by other functions; if it matches the UID we were given, we don't
// need to do anything.
func (c *Container) generateUserPasswdEntry(addedUID int) (string, error) {
	var (
		groupspec string
		gid       int
	)
	if c.config.User == "" {
		return "", nil
	}
	splitSpec := strings.SplitN(c.config.User, ":", 2)
	userspec := splitSpec[0]
	if len(splitSpec) > 1 {
		groupspec = splitSpec[1]
	}
	// If a non numeric User, then don't generate passwd
	uid, err := strconv.ParseUint(userspec, 10, 32)
	if err != nil {
		return "", nil //nolint: nilerr
	}

	if addedUID != 0 && int(uid) == addedUID {
		return "", nil
	}

	// Look up the user to see if it exists in the container image
	_, err = lookup.GetUser(c.state.Mountpoint, userspec)
	if err != runcuser.ErrNoPasswdEntries {
		return "", err
	}

	if groupspec != "" {
		ugid, err := strconv.ParseUint(groupspec, 10, 32)
		if err == nil {
			gid = int(ugid)
		} else {
			group, err := lookup.GetGroup(c.state.Mountpoint, groupspec)
			if err != nil {
				return "", fmt.Errorf("unable to get gid %s from group file: %w", groupspec, err)
			}
			gid = group.Gid
		}
	}

	if c.config.PasswdEntry != "" {
		entry := c.passwdEntry(strconv.FormatUint(uid, 10), strconv.FormatUint(uid, 10), strconv.FormatInt(int64(gid), 10), "container user", c.WorkingDir())
		return entry, nil
	}

	u, err := user.LookupId(strconv.FormatUint(uid, 10))
	if err == nil {
		return fmt.Sprintf("%s:*:%d:%d:%s:%s:/bin/sh\n", u.Username, uid, gid, u.Name, c.WorkingDir()), nil
	}
	return fmt.Sprintf("%d:*:%d:%d:container user:%s:/bin/sh\n", uid, uid, gid, c.WorkingDir()), nil
}

func (c *Container) passwdEntry(username, uid, gid, name, homeDir string) string {
	s := c.config.PasswdEntry
	s = strings.ReplaceAll(s, "$USERNAME", username)
	s = strings.ReplaceAll(s, "$UID", uid)
	s = strings.ReplaceAll(s, "$GID", gid)
	s = strings.ReplaceAll(s, "$NAME", name)
	s = strings.ReplaceAll(s, "$HOME", homeDir)
	return s + "\n"
}

// generatePasswdAndGroup generates container-specific passwd and group files
// iff g.config.User is a number or we are configured to make a passwd entry for
// the current user or the user specified HostsUsers
// Returns path to file to mount at /etc/passwd, path to file to mount at
// /etc/group, and any error that occurred. If no passwd/group file were
// required, the empty string will be returned for those path (this may occur
// even if no error happened).
// This may modify the mounted container's /etc/passwd and /etc/group instead of
// making copies to bind-mount in, so we don't break useradd (it wants to make a
// copy of /etc/passwd and rename the copy to /etc/passwd, which is impossible
// with a bind mount). This is done in cases where the container is *not*
// read-only. In this case, the function will return nothing ("", "", nil).
func (c *Container) generatePasswdAndGroup() (string, string, error) {
	if !c.config.AddCurrentUserPasswdEntry && c.config.User == "" &&
		len(c.config.HostUsers) == 0 && c.config.GroupEntry == "" {
		return "", "", nil
	}

	needPasswd := true
	needGroup := true

	// First, check if there's a mount at /etc/passwd or group, we don't
	// want to interfere with user mounts.
	if MountExists(c.config.Spec.Mounts, "/etc/passwd") {
		needPasswd = false
	}
	if MountExists(c.config.Spec.Mounts, "/etc/group") {
		needGroup = false
	}

	// Next, check if we already made the files. If we didn't, don't need to
	// do anything more.
	if needPasswd {
		passwdPath := filepath.Join(c.config.StaticDir, "passwd")
		if err := fileutils.Exists(passwdPath); err == nil {
			needPasswd = false
		}
	}
	if needGroup {
		groupPath := filepath.Join(c.config.StaticDir, "group")
		if err := fileutils.Exists(groupPath); err == nil {
			needGroup = false
		}
	}

	// If we don't need a /etc/passwd or /etc/group at this point we can
	// just return.
	if !needPasswd && !needGroup {
		return "", "", nil
	}

	passwdPath := ""
	groupPath := ""

	ro := c.IsReadOnly()

	if needPasswd {
		passwdEntry, err := c.generatePasswdEntry()
		if err != nil {
			return "", "", err
		}

		needsWrite := passwdEntry != ""
		switch {
		case ro && needsWrite:
			logrus.Debugf("Making /etc/passwd for container %s", c.ID())
			originPasswdFile, err := securejoin.SecureJoin(c.state.Mountpoint, "/etc/passwd")
			if err != nil {
				return "", "", fmt.Errorf("creating path to container %s /etc/passwd: %w", c.ID(), err)
			}
			orig, err := os.ReadFile(originPasswdFile)
			if err != nil && !os.IsNotExist(err) {
				return "", "", err
			}
			passwdFile, err := c.writeStringToStaticDir("passwd", string(orig)+passwdEntry)
			if err != nil {
				return "", "", fmt.Errorf("failed to create temporary passwd file: %w", err)
			}
			if err := os.Chmod(passwdFile, 0o644); err != nil {
				return "", "", err
			}
			passwdPath = passwdFile
		case !ro && needsWrite:
			logrus.Debugf("Modifying container %s /etc/passwd", c.ID())
			containerPasswd, err := securejoin.SecureJoin(c.state.Mountpoint, "/etc/passwd")
			if err != nil {
				return "", "", fmt.Errorf("looking up location of container %s /etc/passwd: %w", c.ID(), err)
			}

			f, err := os.OpenFile(containerPasswd, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return "", "", fmt.Errorf("container %s: %w", c.ID(), err)
			}
			defer f.Close()

			if _, err := f.WriteString(passwdEntry); err != nil {
				return "", "", fmt.Errorf("unable to append to container %s /etc/passwd: %w", c.ID(), err)
			}
		default:
			logrus.Debugf("Not modifying container %s /etc/passwd", c.ID())
		}
	}
	if needGroup {
		groupEntry, err := c.generateGroupEntry()
		if err != nil {
			return "", "", err
		}

		needsWrite := groupEntry != ""
		switch {
		case ro && needsWrite:
			logrus.Debugf("Making /etc/group for container %s", c.ID())
			originGroupFile, err := securejoin.SecureJoin(c.state.Mountpoint, "/etc/group")
			if err != nil {
				return "", "", fmt.Errorf("creating path to container %s /etc/group: %w", c.ID(), err)
			}
			orig, err := os.ReadFile(originGroupFile)
			if err != nil && !os.IsNotExist(err) {
				return "", "", err
			}
			groupFile, err := c.writeStringToStaticDir("group", string(orig)+groupEntry)
			if err != nil {
				return "", "", fmt.Errorf("failed to create temporary group file: %w", err)
			}
			if err := os.Chmod(groupFile, 0o644); err != nil {
				return "", "", err
			}
			groupPath = groupFile
		case !ro && needsWrite:
			logrus.Debugf("Modifying container %s /etc/group", c.ID())
			containerGroup, err := securejoin.SecureJoin(c.state.Mountpoint, "/etc/group")
			if err != nil {
				return "", "", fmt.Errorf("looking up location of container %s /etc/group: %w", c.ID(), err)
			}

			f, err := os.OpenFile(containerGroup, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return "", "", fmt.Errorf("container %s: %w", c.ID(), err)
			}
			defer f.Close()

			if _, err := f.WriteString(groupEntry); err != nil {
				return "", "", fmt.Errorf("unable to append to container %s /etc/group: %w", c.ID(), err)
			}
		default:
			logrus.Debugf("Not modifying container %s /etc/group", c.ID())
		}
	}

	return passwdPath, groupPath, nil
}

func (c *Container) cleanupOverlayMounts() error {
	return overlay.CleanupContent(c.config.StaticDir)
}

// Creates and mounts an empty dir to mount secrets into, if it does not already exist
func (c *Container) createSecretMountDir(runPath string) error {
	src := filepath.Join(c.state.RunDir, "/run/secrets")
	err := fileutils.Exists(src)
	if os.IsNotExist(err) {
		if err := umask.MkdirAllIgnoreUmask(src, os.FileMode(0o755)); err != nil {
			return err
		}
		if err := c.relabel(src, c.config.MountLabel, false); err != nil {
			return err
		}
		if err := idtools.SafeChown(src, c.RootUID(), c.RootGID()); err != nil {
			return err
		}
		c.state.BindMounts[filepath.Join(runPath, "secrets")] = src
		return nil
	}

	return err
}

func hasIdmapOption(options []string) bool {
	for _, o := range options {
		if o == "idmap" || strings.HasPrefix(o, "idmap=") {
			return true
		}
	}
	return false
}

// Fix ownership and permissions of the specified volume if necessary.
func (c *Container) fixVolumePermissions(v *ContainerNamedVolume) error {
	vol, err := c.runtime.state.Volume(v.Name)
	if err != nil {
		return fmt.Errorf("retrieving named volume %s for container %s: %w", v.Name, c.ID(), err)
	}

	vol.lock.Lock()
	defer vol.lock.Unlock()

	return c.fixVolumePermissionsUnlocked(v, vol)
}

func (c *Container) fixVolumePermissionsUnlocked(v *ContainerNamedVolume, vol *Volume) error {
	// The volume may need a copy-up. Check the state.
	if err := vol.update(); err != nil {
		return err
	}

	// If the volume is not empty, and it is not the first copy-up event -
	// we should not do a chown.
	if vol.state.NeedsChown && !vol.state.CopiedUp {
		contents, err := os.ReadDir(vol.mountPoint())
		if err != nil {
			return fmt.Errorf("reading contents of volume %q: %w", vol.Name(), err)
		}
		// Not empty, do nothing and unset NeedsChown.
		if len(contents) > 0 {
			vol.state.NeedsChown = false
			if err := vol.save(); err != nil {
				return fmt.Errorf("saving volume %q state: %w", vol.Name(), err)
			}
			return nil
		}
	}

	// Volumes owned by a volume driver are not chowned - we don't want to
	// mess with a mount not managed by us.
	if vol.state.NeedsChown && (!vol.UsesVolumeDriver() && vol.config.Driver != "image") {
		uid := int(c.config.Spec.Process.User.UID)
		gid := int(c.config.Spec.Process.User.GID)

		idmapped := hasIdmapOption(v.Options)

		// if the volume is mounted with "idmap", leave the IDs in from the current environment.
		if c.config.IDMappings.UIDMap != nil && !idmapped {
			p := idtools.IDPair{
				UID: uid,
				GID: gid,
			}
			mappings := idtools.NewIDMappingsFromMaps(c.config.IDMappings.UIDMap, c.config.IDMappings.GIDMap)
			newPair, err := mappings.ToHost(p)
			if err != nil {
				return fmt.Errorf("mapping user %d:%d: %w", uid, gid, err)
			}
			uid = newPair.UID
			gid = newPair.GID
		}

		if vol.state.CopiedUp {
			vol.state.NeedsChown = false
		}
		vol.state.CopiedUp = false
		vol.state.UIDChowned = uid
		vol.state.GIDChowned = gid

		if err := vol.save(); err != nil {
			return err
		}

		mountPoint, err := vol.MountPoint()
		if err != nil {
			return err
		}

		if err := idtools.SafeLchown(mountPoint, uid, gid); err != nil {
			return err
		}

		// Make sure the new volume matches the permissions of the target directory unless 'U' is
		// provided (since the volume was already chowned in this case).
		// https://github.com/containers/podman/issues/10188
		if slices.Contains(v.Options, "U") {
			return nil
		}

		finalPath, err := securejoin.SecureJoin(c.state.Mountpoint, v.Dest)
		if err != nil {
			return err
		}
		st, err := os.Lstat(finalPath)
		if err == nil {
			if stat, ok := st.Sys().(*syscall.Stat_t); ok {
				uid, gid := int(stat.Uid), int(stat.Gid)

				// If the volume is idmapped then undo the conversion to obtain the desired UID/GID in the container
				if c.config.IDMappings.UIDMap != nil && idmapped {
					p := idtools.IDPair{
						UID: uid,
						GID: gid,
					}
					mappings := idtools.NewIDMappingsFromMaps(c.config.IDMappings.UIDMap, c.config.IDMappings.GIDMap)
					newUID, newGID, err := mappings.ToContainer(p)
					if err != nil {
						return fmt.Errorf("mapping user %d:%d: %w", uid, gid, err)
					}
					uid, gid = newUID, newGID
				}

				if err := idtools.SafeLchown(mountPoint, uid, gid); err != nil {
					return err
				}

				// UID/GID 0 are sticky - if we chown to root,
				// we stop chowning thereafter.
				if uid == 0 && gid == 0 && vol.state.NeedsChown {
					vol.state.NeedsChown = false

					if err := vol.save(); err != nil {
						return fmt.Errorf("saving volume %q state to database: %w", vol.Name(), err)
					}
				}
			}
			if err := os.Chmod(mountPoint, st.Mode()); err != nil {
				return err
			}
			if err := setVolumeAtime(mountPoint, st); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *Container) relabel(src, mountLabel string, shared bool) error {
	if !selinux.GetEnabled() || mountLabel == "" {
		return nil
	}
	// only relabel on initial creation of container
	if !c.ensureState(define.ContainerStateConfigured, define.ContainerStateUnknown) {
		label, err := selinux.FileLabel(src)
		if err != nil {
			return err
		}
		// If labels are different, might be on a tmpfs
		if label == mountLabel {
			return nil
		}
	}
	err := label.Relabel(src, mountLabel, shared)
	if errors.Is(err, unix.ENOTSUP) {
		logrus.Debugf("Labeling not supported on %q", src)
		return nil
	}
	return err
}

func (c *Container) ChangeHostPathOwnership(src string, recurse bool, uid, gid int) error {
	// only chown on initial creation of container
	if !c.ensureState(define.ContainerStateConfigured, define.ContainerStateUnknown) {
		st, err := os.Stat(src)
		if err != nil {
			return err
		}

		// If labels are different, might be on a tmpfs
		if int(st.Sys().(*syscall.Stat_t).Uid) == uid && int(st.Sys().(*syscall.Stat_t).Gid) == gid {
			return nil
		}
	}
	return chown.ChangeHostPathOwnership(src, recurse, uid, gid)
}

func (c *Container) umask() (uint32, error) {
	decVal, err := strconv.ParseUint(c.config.Umask, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid Umask Value: %w", err)
	}
	return uint32(decVal), nil
}

func maybeClampOOMScoreAdj(oomScoreValue int) (int, error) {
	v, err := os.ReadFile("/proc/self/oom_score_adj")
	if err != nil {
		return oomScoreValue, err
	}
	currentValue, err := strconv.Atoi(strings.TrimRight(string(v), "\n"))
	if err != nil {
		return oomScoreValue, err
	}
	if currentValue > oomScoreValue {
		logrus.Warnf("Requested oom_score_adj=%d is lower than the current one, changing to %d", oomScoreValue, currentValue)
		return currentValue, nil
	}
	return oomScoreValue, nil
}
