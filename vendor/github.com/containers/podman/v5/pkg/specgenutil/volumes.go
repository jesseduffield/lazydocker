package specgenutil

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/containers/podman/v5/pkg/specgenutilexternal"
	"github.com/containers/podman/v5/pkg/util"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/parse"
)

var (
	errOptionArg = errors.New("must provide an argument for option")
	errNoDest    = errors.New("must set volume destination")
)

type containerMountSlice struct {
	mounts          []spec.Mount
	volumes         []*specgen.NamedVolume
	overlayVolumes  []*specgen.OverlayVolume
	imageVolumes    []*specgen.ImageVolume
	artifactVolumes []*specgen.ArtifactVolume
}

// containerMountMap contains the container mounts with the destination path as map key
type containerMountMap struct {
	mounts          map[string]spec.Mount
	volumes         map[string]*specgen.NamedVolume
	imageVolumes    map[string]*specgen.ImageVolume
	artifactVolumes map[string]*specgen.ArtifactVolume
}

type universalMount struct {
	mount spec.Mount
	// Used only with Named Volume type mounts
	subPath string
}

// Parse all volume-related options in the create config into a set of mounts
// and named volumes to add to the container.
// Handles --volumes, --mount, and --tmpfs flags.
// Does not handle image volumes, init, and --volumes-from flags.
// Can also add tmpfs mounts from read-only tmpfs.
// TODO: handle options parsing/processing via containers/storage/pkg/mount
func parseVolumes(rtc *config.Config, volumeFlag, mountFlag, tmpfsFlag []string) (*containerMountSlice, error) {
	// Get mounts from the --mounts flag.
	// TODO: The runtime config part of this needs to move into pkg/specgen/generate to avoid querying containers.conf on the client.
	unifiedContainerMounts, err := mounts(mountFlag, rtc.Mounts())
	if err != nil {
		return nil, err
	}

	// Next --volumes flag.
	volumeMounts, volumeVolumes, overlayVolumes, err := specgen.GenVolumeMounts(volumeFlag)
	if err != nil {
		return nil, err
	}

	// Next --tmpfs flag.
	tmpfsMounts, err := getTmpfsMounts(tmpfsFlag)
	if err != nil {
		return nil, err
	}

	// Unify mounts from --mount, --volume, --tmpfs.
	// Start with --volume.
	for dest, mount := range volumeMounts {
		if vol, ok := unifiedContainerMounts.mounts[dest]; ok {
			if mount.Source == vol.Source &&
				specgen.StringSlicesEqual(vol.Options, mount.Options) {
				continue
			}
			return nil, fmt.Errorf("%v: %w", dest, specgen.ErrDuplicateDest)
		}
		unifiedContainerMounts.mounts[dest] = mount
	}
	for dest, volume := range volumeVolumes {
		if vol, ok := unifiedContainerMounts.volumes[dest]; ok {
			if volume.Name == vol.Name &&
				specgen.StringSlicesEqual(vol.Options, volume.Options) {
				continue
			}
			return nil, fmt.Errorf("%v: %w", dest, specgen.ErrDuplicateDest)
		}
		unifiedContainerMounts.volumes[dest] = volume
	}
	// Now --tmpfs
	for dest, tmpfs := range tmpfsMounts {
		if vol, ok := unifiedContainerMounts.mounts[dest]; ok {
			if vol.Type != define.TypeTmpfs {
				return nil, fmt.Errorf("%v: %w", dest, specgen.ErrDuplicateDest)
			}
			continue
		}
		unifiedContainerMounts.mounts[dest] = tmpfs
	}

	// Check for conflicts between named volumes, overlay & image volumes,
	// and mounts
	allMounts := make(map[string]bool)
	testAndSet := func(dest string) error {
		if _, ok := allMounts[dest]; ok {
			return fmt.Errorf("%v: %w", dest, specgen.ErrDuplicateDest)
		}
		allMounts[dest] = true
		return nil
	}
	for dest := range unifiedContainerMounts.mounts {
		if err := testAndSet(dest); err != nil {
			return nil, err
		}
	}
	for dest := range unifiedContainerMounts.volumes {
		if err := testAndSet(dest); err != nil {
			return nil, err
		}
	}
	for dest := range overlayVolumes {
		if err := testAndSet(dest); err != nil {
			return nil, err
		}
	}
	for dest := range unifiedContainerMounts.imageVolumes {
		if err := testAndSet(dest); err != nil {
			return nil, err
		}
	}
	for dest := range unifiedContainerMounts.artifactVolumes {
		if err := testAndSet(dest); err != nil {
			return nil, err
		}
	}

	// Final step: maps to arrays
	finalMounts := make([]spec.Mount, 0, len(unifiedContainerMounts.mounts))
	for _, mount := range unifiedContainerMounts.mounts {
		if mount.Type == define.TypeBind {
			absSrc, err := specgen.ConvertWinMountPath(mount.Source)
			if err != nil {
				return nil, fmt.Errorf("getting absolute path of %s: %w", mount.Source, err)
			}
			mount.Source = absSrc
		}
		finalMounts = append(finalMounts, mount)
	}
	finalVolumes := make([]*specgen.NamedVolume, 0, len(unifiedContainerMounts.volumes))
	for _, volume := range unifiedContainerMounts.volumes {
		finalVolumes = append(finalVolumes, volume)
	}
	finalOverlayVolume := make([]*specgen.OverlayVolume, 0, len(overlayVolumes))
	for _, volume := range overlayVolumes {
		absSrc, err := specgen.ConvertWinMountPath(volume.Source)
		if err != nil {
			return nil, fmt.Errorf("getting absolute path of %s: %w", volume.Source, err)
		}
		volume.Source = absSrc
		finalOverlayVolume = append(finalOverlayVolume, volume)
	}
	finalImageVolumes := make([]*specgen.ImageVolume, 0, len(unifiedContainerMounts.imageVolumes))
	for _, volume := range unifiedContainerMounts.imageVolumes {
		finalImageVolumes = append(finalImageVolumes, volume)
	}
	finalArtifactVolumes := make([]*specgen.ArtifactVolume, 0, len(unifiedContainerMounts.artifactVolumes))
	for _, volume := range unifiedContainerMounts.artifactVolumes {
		finalArtifactVolumes = append(finalArtifactVolumes, volume)
	}

	return &containerMountSlice{
		mounts:          finalMounts,
		volumes:         finalVolumes,
		overlayVolumes:  finalOverlayVolume,
		imageVolumes:    finalImageVolumes,
		artifactVolumes: finalArtifactVolumes,
	}, nil
}

// mounts takes user-provided input from the --mount flag as well as mounts
// specified in containers.conf and creates OCI spec mounts and Libpod named volumes.
// podman run --mount type=bind,src=/etc/resolv.conf,target=/etc/resolv.conf ...
// podman run --mount type=tmpfs,target=/dev/shm ...
// podman run --mount type=volume,source=test-volume, ...
// podman run --mount type=artifact,source=$artifact,dest=...
func mounts(mountFlag []string, configMounts []string) (*containerMountMap, error) {
	finalMounts := make(map[string]spec.Mount)
	finalNamedVolumes := make(map[string]*specgen.NamedVolume)
	finalImageVolumes := make(map[string]*specgen.ImageVolume)
	finalArtifactVolumes := make(map[string]*specgen.ArtifactVolume)
	parseMounts := func(mounts []string, ignoreDup bool) error {
		for _, mount := range mounts {
			// TODO: Docker defaults to "volume" if no mount type is specified.
			mountType, tokens, err := specgenutilexternal.FindMountType(mount)
			if err != nil {
				return err
			}
			switch mountType {
			case define.TypeBind:
				mount, err := getBindMount(tokens)
				if err != nil {
					return err
				}
				if _, ok := finalMounts[mount.Destination]; ok {
					if ignoreDup {
						continue
					}
					return fmt.Errorf("%v: %w", mount.Destination, specgen.ErrDuplicateDest)
				}
				finalMounts[mount.Destination] = mount
			case "glob":
				mounts, err := getGlobMounts(tokens)
				if err != nil {
					return err
				}
				for _, mount := range mounts {
					if _, ok := finalMounts[mount.Destination]; ok {
						if ignoreDup {
							continue
						}
						return fmt.Errorf("%v: %w", mount.Destination, specgen.ErrDuplicateDest)
					}
					finalMounts[mount.Destination] = mount
				}
			case define.TypeTmpfs, define.TypeRamfs:
				mount, err := parseMemoryMount(tokens, mountType)
				if err != nil {
					return err
				}
				if _, ok := finalMounts[mount.Destination]; ok {
					if ignoreDup {
						continue
					}
					return fmt.Errorf("%v: %w", mount.Destination, specgen.ErrDuplicateDest)
				}
				finalMounts[mount.Destination] = mount
			case define.TypeDevpts:
				mount, err := getDevptsMount(tokens)
				if err != nil {
					return err
				}
				if _, ok := finalMounts[mount.Destination]; ok {
					if ignoreDup {
						continue
					}
					return fmt.Errorf("%v: %w", mount.Destination, specgen.ErrDuplicateDest)
				}
				finalMounts[mount.Destination] = mount
			case "image":
				volume, err := getImageVolume(tokens)
				if err != nil {
					return err
				}
				if _, ok := finalImageVolumes[volume.Destination]; ok {
					if ignoreDup {
						continue
					}
					return fmt.Errorf("%v: %w", volume.Destination, specgen.ErrDuplicateDest)
				}
				finalImageVolumes[volume.Destination] = volume
			case "artifact":
				volume, err := getArtifactVolume(tokens)
				if err != nil {
					return err
				}
				if _, ok := finalArtifactVolumes[volume.Destination]; ok {
					if ignoreDup {
						continue
					}
					return fmt.Errorf("%v: %w", volume.Destination, specgen.ErrDuplicateDest)
				}
				finalArtifactVolumes[volume.Destination] = volume
			case "volume":
				volume, err := getNamedVolume(tokens)
				if err != nil {
					return err
				}
				if _, ok := finalNamedVolumes[volume.Dest]; ok {
					if ignoreDup {
						continue
					}
					return fmt.Errorf("%v: %w", volume.Dest, specgen.ErrDuplicateDest)
				}
				finalNamedVolumes[volume.Dest] = volume
			default:
				return fmt.Errorf("invalid filesystem type %q", mountType)
			}
		}
		return nil
	}

	// Parse mounts passed in from the user
	if err := parseMounts(mountFlag, false); err != nil {
		return nil, err
	}

	// If user specified a mount flag that conflicts with a containers.conf flag, then ignore
	// the duplicate. This means that the parsing of the containers.conf configMounts should always
	// happen second.
	if err := parseMounts(configMounts, true); err != nil {
		return nil, fmt.Errorf("parsing containers.conf mounts: %w", err)
	}

	return &containerMountMap{
		mounts:          finalMounts,
		volumes:         finalNamedVolumes,
		imageVolumes:    finalImageVolumes,
		artifactVolumes: finalArtifactVolumes,
	}, nil
}

func parseMountOptions(mountType string, args []string) (*universalMount, error) {
	var setTmpcopyup, setRORW, setSuid, setDev, setExec, setRelabel, setOwnership, setSwap bool

	mnt := new(universalMount)
	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "bind-nonrecursive":
			if mountType != define.TypeBind {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			mnt.mount.Options = append(mnt.mount.Options, define.TypeBind)
		case "bind-propagation":
			if mountType != define.TypeBind {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			switch value {
			case "shared", "rshared", "private", "rprivate", "slave", "rslave", "unbindable", "runbindable":
				// Do nothing, sane value
			default:
				return nil, fmt.Errorf("invalid value %q", arg)
			}
			mnt.mount.Options = append(mnt.mount.Options, value)
		case "consistency":
			// Often used on MACs and mistakenly on Linux platforms.
			// Since Docker ignores this option so shall we.
			continue
		case "idmap":
			if hasValue {
				mnt.mount.Options = append(mnt.mount.Options, fmt.Sprintf("idmap=%s", value))
			} else {
				mnt.mount.Options = append(mnt.mount.Options, "idmap")
			}
		case "readonly", "ro", "rw":
			if setRORW {
				return nil, fmt.Errorf("cannot pass 'readonly', 'ro', or 'rw' mnt.Options more than once: %w", errOptionArg)
			}
			setRORW = true
			// Can be formatted as one of:
			// readonly
			// readonly=[true|false]
			// ro
			// ro=[true|false]
			// rw
			// rw=[true|false]
			if name == "readonly" {
				name = "ro"
			}
			if hasValue {
				switch strings.ToLower(value) {
				case "true":
					mnt.mount.Options = append(mnt.mount.Options, name)
				case "false":
					// Set the opposite only for rw
					// ro's opposite is the default
					if name == "rw" {
						mnt.mount.Options = append(mnt.mount.Options, "ro")
					}
				}
			} else {
				mnt.mount.Options = append(mnt.mount.Options, name)
			}
		case "nodev", "dev":
			if setDev {
				return nil, fmt.Errorf("cannot pass 'nodev' and 'dev' mnt.Options more than once: %w", errOptionArg)
			}
			setDev = true
			mnt.mount.Options = append(mnt.mount.Options, name)
		case "noexec", "exec":
			if setExec {
				return nil, fmt.Errorf("cannot pass 'noexec' and 'exec' mnt.Options more than once: %w", errOptionArg)
			}
			setExec = true
			mnt.mount.Options = append(mnt.mount.Options, name)
		case "nosuid", "suid":
			if setSuid {
				return nil, fmt.Errorf("cannot pass 'nosuid' and 'suid' mnt.Options more than once: %w", errOptionArg)
			}
			setSuid = true
			mnt.mount.Options = append(mnt.mount.Options, name)
		case "noswap":
			if setSwap {
				return nil, fmt.Errorf("cannot pass 'noswap' mnt.Options more than once: %w", errOptionArg)
			}
			if rootless.IsRootless() {
				return nil, fmt.Errorf("the 'noswap' option is only allowed with rootful tmpfs mounts: %w", errOptionArg)
			}
			setSwap = true
			mnt.mount.Options = append(mnt.mount.Options, name)
		case "relabel":
			if setRelabel {
				return nil, fmt.Errorf("cannot pass 'relabel' option more than once: %w", errOptionArg)
			}
			setRelabel = true
			if !hasValue {
				return nil, fmt.Errorf("%s mount option must be 'private' or 'shared': %w", name, util.ErrBadMntOption)
			}
			switch value {
			case "private":
				mnt.mount.Options = append(mnt.mount.Options, "Z")
			case "shared":
				mnt.mount.Options = append(mnt.mount.Options, "z")
			default:
				return nil, fmt.Errorf("%s mount option must be 'private' or 'shared': %w", name, util.ErrBadMntOption)
			}
		case "shared", "rshared", "private", "rprivate", "slave", "rslave", "unbindable", "runbindable", "Z", "z", "no-dereference":
			mnt.mount.Options = append(mnt.mount.Options, name)
		case "src", "source":
			if mountType == define.TypeTmpfs {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			if mnt.mount.Source != "" {
				return nil, fmt.Errorf("cannot pass %q option more than once: %w", name, errOptionArg)
			}
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			if len(value) == 0 {
				return nil, fmt.Errorf("host directory cannot be empty: %w", errOptionArg)
			}
			mnt.mount.Source = value
		case "subpath", "volume-subpath":
			if mountType != define.TypeVolume {
				return nil, fmt.Errorf("cannot set option %q on non-volume mounts", name)
			}
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			mnt.subPath = value
		case "target", "dst", "dest", "destination":
			if mnt.mount.Destination != "" {
				return nil, fmt.Errorf("cannot pass %q option more than once: %w", name, errOptionArg)
			}
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			if err := parse.ValidateVolumeCtrDir(value); err != nil {
				return nil, err
			}
			mnt.mount.Destination = unixPathClean(value)
		case "tmpcopyup", "notmpcopyup":
			if mountType != define.TypeTmpfs {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			if setTmpcopyup {
				return nil, fmt.Errorf("cannot pass 'tmpcopyup' and 'notmpcopyup' mnt.Options more than once: %w", errOptionArg)
			}
			setTmpcopyup = true
			mnt.mount.Options = append(mnt.mount.Options, name)
		case "tmpfs-mode":
			if mountType != define.TypeTmpfs {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			mnt.mount.Options = append(mnt.mount.Options, fmt.Sprintf("mode=%s", value))
		case "tmpfs-size":
			if mountType != define.TypeTmpfs {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			mnt.mount.Options = append(mnt.mount.Options, fmt.Sprintf("size=%s", value))
		case "U", "chown":
			if setOwnership {
				return nil, fmt.Errorf("cannot pass 'U' or 'chown' option more than once: %w", errOptionArg)
			}
			ok, err := validChownFlag(value)
			if err != nil {
				return nil, err
			}
			if ok {
				mnt.mount.Options = append(mnt.mount.Options, "U")
			}
			setOwnership = true
		case "volume-label":
			if mountType != define.TypeVolume {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			return nil, fmt.Errorf("the --volume-label option is not presently implemented")
		case "volume-opt":
			if mountType != define.TypeVolume {
				return nil, fmt.Errorf("%q option not supported for %q mount types", name, mountType)
			}
			mnt.mount.Options = append(mnt.mount.Options, arg)
		default:
			return nil, fmt.Errorf("%s: %w", name, util.ErrBadMntOption)
		}
	}
	if mountType != "glob" && len(mnt.mount.Destination) == 0 {
		return nil, errNoDest
	}
	return mnt, nil
}

// Parse glob mounts entry from the --mount flag.
func getGlobMounts(args []string) ([]spec.Mount, error) {
	mounts := []spec.Mount{}

	uMnt, err := parseMountOptions("glob", args)
	if err != nil {
		return nil, err
	}
	mnt := uMnt.mount

	globs, err := filepath.Glob(mnt.Source)
	if err != nil {
		return nil, err
	}
	if len(globs) == 0 {
		return nil, fmt.Errorf("no file paths matching glob %q", mnt.Source)
	}

	options, err := parse.ValidateVolumeOpts(mnt.Options)
	if err != nil {
		return nil, err
	}
	for _, src := range globs {
		var newMount spec.Mount
		newMount.Type = define.TypeBind
		newMount.Options = options
		newMount.Source = src
		if len(mnt.Destination) == 0 {
			newMount.Destination = src
		} else {
			newMount.Destination = filepath.Join(mnt.Destination, filepath.Base(src))
		}
		mounts = append(mounts, newMount)
	}

	return mounts, nil
}

// Parse a single bind mount entry from the --mount flag.
func getBindMount(args []string) (spec.Mount, error) {
	newMount := spec.Mount{
		Type: define.TypeBind,
	}
	var err error
	uMnt, err := parseMountOptions(newMount.Type, args)
	if err != nil {
		return newMount, err
	}
	mnt := uMnt.mount

	if len(mnt.Destination) == 0 {
		return newMount, errNoDest
	}

	if len(mnt.Source) == 0 {
		mnt.Source = mnt.Destination
	}

	options, err := parse.ValidateVolumeOpts(mnt.Options)
	if err != nil {
		return newMount, err
	}
	newMount.Source = mnt.Source
	newMount.Destination = mnt.Destination
	newMount.Options = options
	return newMount, nil
}

// Parse a single tmpfs/ramfs mount entry from the --mount flag
func parseMemoryMount(args []string, mountType string) (spec.Mount, error) {
	newMount := spec.Mount{
		Type:   mountType,
		Source: mountType,
	}

	var err error
	uMnt, err := parseMountOptions(newMount.Type, args)
	if err != nil {
		return newMount, err
	}
	mnt := uMnt.mount
	if len(mnt.Destination) == 0 {
		return newMount, errNoDest
	}
	newMount.Destination = mnt.Destination
	newMount.Options = mnt.Options
	return newMount, nil
}

// Parse a single devpts mount entry from the --mount flag
func getDevptsMount(args []string) (spec.Mount, error) {
	newMount := spec.Mount{
		Type:   define.TypeDevpts,
		Source: define.TypeDevpts,
	}

	var setDest bool

	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "uid", "gid", "mode", "ptmxmode", "newinstance", "max":
			newMount.Options = append(newMount.Options, arg)
		case "target", "dst", "dest", "destination":
			if !hasValue {
				return newMount, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			if err := parse.ValidateVolumeCtrDir(value); err != nil {
				return newMount, err
			}
			newMount.Destination = unixPathClean(value)
			setDest = true
		default:
			return newMount, fmt.Errorf("%s: %w", name, util.ErrBadMntOption)
		}
	}

	if !setDest {
		return newMount, errNoDest
	}

	return newMount, nil
}

// Parse a single volume mount entry from the --mount flag.
// Note that the volume-label option for named volumes is currently NOT supported.
// TODO: add support for --volume-label
func getNamedVolume(args []string) (*specgen.NamedVolume, error) {
	newVolume := new(specgen.NamedVolume)

	mnt, err := parseMountOptions(define.TypeVolume, args)
	if err != nil {
		return nil, err
	}
	if len(mnt.mount.Destination) == 0 {
		return nil, errNoDest
	}

	newVolume.Options = mnt.mount.Options
	newVolume.SubPath = mnt.subPath
	newVolume.Name = mnt.mount.Source
	newVolume.Dest = mnt.mount.Destination
	return newVolume, nil
}

// Parse the arguments into an image volume. An image volume is a volume based
// on a container image.  The container image is first mounted on the host and
// is then bind-mounted into the container.  An ImageVolume is always mounted
// read-only.
func getImageVolume(args []string) (*specgen.ImageVolume, error) {
	newVolume := new(specgen.ImageVolume)

	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "src", "source":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			newVolume.Source = value
		case "target", "dst", "dest", "destination":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			if err := parse.ValidateVolumeCtrDir(value); err != nil {
				return nil, err
			}
			newVolume.Destination = unixPathClean(value)
		case "rw", "readwrite":
			switch value {
			case "true":
				newVolume.ReadWrite = true
			case "false":
				// Nothing to do. RO is default.
			default:
				return nil, fmt.Errorf("invalid rw value %q: %w", value, util.ErrBadMntOption)
			}
		case "subpath":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			if !filepath.IsAbs(value) {
				return nil, fmt.Errorf("volume subpath %q must be an absolute path", value)
			}
			newVolume.SubPath = value
		case "consistency":
			// Often used on MACs and mistakenly on Linux platforms.
			// Since Docker ignores this option so shall we.
			continue
		default:
			return nil, fmt.Errorf("%s: %w", name, util.ErrBadMntOption)
		}
	}

	if len(newVolume.Source)*len(newVolume.Destination) == 0 {
		return nil, errors.New("must set source and destination for image volume")
	}

	return newVolume, nil
}

// Parse the arguments into an artifact volume. An artifact volume creates mounts
// based on an existing artifact in the store.
func getArtifactVolume(args []string) (*specgen.ArtifactVolume, error) {
	newVolume := new(specgen.ArtifactVolume)

	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "src", "source":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			newVolume.Source = value
		case "target", "dst", "dest", "destination":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			if err := parse.ValidateVolumeCtrDir(value); err != nil {
				return nil, err
			}
			newVolume.Destination = unixPathClean(value)
		case "title":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			newVolume.Title = value

		case "digest":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			newVolume.Digest = value
		case "name":
			if !hasValue {
				return nil, fmt.Errorf("%v: %w", name, errOptionArg)
			}
			newVolume.Name = value
		default:
			return nil, fmt.Errorf("%s: %w", name, util.ErrBadMntOption)
		}
	}

	if len(newVolume.Source)*len(newVolume.Destination) == 0 {
		return nil, errors.New("must set source and destination for artifact volume")
	}

	return newVolume, nil
}

// GetTmpfsMounts creates spec.Mount structs for user-requested tmpfs mounts
func getTmpfsMounts(tmpfsFlag []string) (map[string]spec.Mount, error) {
	m := make(map[string]spec.Mount)
	for _, i := range tmpfsFlag {
		// Default options if nothing passed
		var options []string
		spliti := strings.Split(i, ":")
		destPath := spliti[0]
		if err := parse.ValidateVolumeCtrDir(spliti[0]); err != nil {
			return nil, err
		}
		if len(spliti) > 1 {
			options = strings.Split(spliti[1], ",")
		}

		if vol, ok := m[destPath]; ok {
			if specgen.StringSlicesEqual(vol.Options, options) {
				continue
			}
			return nil, fmt.Errorf("%v: %w", destPath, specgen.ErrDuplicateDest)
		}
		mount := spec.Mount{
			Destination: unixPathClean(destPath),
			Type:        define.TypeTmpfs,
			Options:     options,
			Source:      define.TypeTmpfs,
		}
		m[destPath] = mount
	}
	return m, nil
}

// validChownFlag ensures that the U or chown flag is correctly used
func validChownFlag(value string) (bool, error) {
	// U=[true|false]
	switch {
	case strings.EqualFold(value, "true"), value == "":
		return true, nil
	case strings.EqualFold(value, "false"):
		return false, nil
	default:
		return false, fmt.Errorf("'U' or 'chown' must be set to true or false, instead received %q: %w", value, errOptionArg)
	}
}

// Use path instead of filepath to preserve Unix style paths on Windows
func unixPathClean(p string) string {
	return path.Clean(p)
}
