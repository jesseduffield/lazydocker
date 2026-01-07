package specgen

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/parse"
)

// NamedVolume holds information about a named volume that will be mounted into
// the container.
type NamedVolume struct {
	// Name is the name of the named volume to be mounted. May be empty.
	// If empty, a new named volume with a pseudorandomly generated name
	// will be mounted at the given destination.
	Name string
	// Destination to mount the named volume within the container. Must be
	// an absolute path. Path will be created if it does not exist.
	Dest string
	// Options are options that the named volume will be mounted with.
	Options []string
	// IsAnonymous sets the named volume as anonymous even if it has a name
	// This is used for emptyDir volumes from a kube yaml
	IsAnonymous bool
	// SubPath stores the sub directory of the named volume to be mounted in the container
	SubPath string
}

// OverlayVolume holds information about an overlay volume that will be mounted into
// the container.
type OverlayVolume struct {
	// Destination is the absolute path where the mount will be placed in the container.
	Destination string `json:"destination"`
	// Source specifies the source path of the mount.
	Source string `json:"source,omitempty"`
	// Options holds overlay volume options.
	Options []string `json:"options,omitempty"`
}

// ImageVolume is a volume based on a container image.  The container image is
// first mounted on the host and is then bind-mounted into the container.  An
// ImageVolume is always mounted read-only.
type ImageVolume struct {
	// Source is the source of the image volume.  The image can be referred
	// to by name and by ID.
	Source string
	// Destination is the absolute path of the mount in the container.
	Destination string
	// ReadWrite sets the volume writable.
	ReadWrite bool
	// SubPath mounts a particular path within the image.
	// If empty, the whole image is mounted.
	SubPath string `json:"subPath,omitempty"`
}

// ArtifactVolume is a volume based on a artifact. The artifact blobs will
// be bind mounted directly as files and must always be read only.
type ArtifactVolume struct {
	// Source is the name or digest of the artifact that should be mounted
	Source string `json:"source"`
	// Destination is the absolute path of the mount in the container.
	// If path is a file in the container, then the artifact must consist of a single blob.
	// Otherwise if it is a directory or does not exists all artifact blobs will be mounted
	// into this path as files. As name the "org.opencontainers.image.title" will be used if
	// available otherwise the digest is used as name.
	Destination string `json:"destination"`
	// Title can be used for multi blob artifacts to only mount the one specific blob that
	// matches the "org.opencontainers.image.title" annotation.
	// Optional. Conflicts with Digest.
	Title string `json:"title,omitempty"`
	// Digest can be used to filter a single blob from a multi blob artifact by the given digest.
	// When this option is set the file name in the container defaults to the digest even when
	// the title annotation exist.
	// Optional. Conflicts with Title.
	Digest string `json:"digest,omitempty"`
	// Name is the name that should be used for the path inside the container. When a single blob
	// is mounted the name is used as is. If multiple blobs are mounted then mount them as
	// "<name>-x" where x is a 0 indexed integer based on the layer order.
	// Optional.
	Name string `json:"name,omitempty"`
}

// GenVolumeMounts parses user input into mounts, volumes and overlay volumes
func GenVolumeMounts(volumeFlag []string) (map[string]spec.Mount, map[string]*NamedVolume, map[string]*OverlayVolume, error) {
	mounts := make(map[string]spec.Mount)
	volumes := make(map[string]*NamedVolume)
	overlayVolumes := make(map[string]*OverlayVolume)

	volumeFormatErr := errors.New("incorrect volume format, should be [host-dir:]ctr-dir[:option]")
	for _, vol := range volumeFlag {
		var (
			options []string
			src     string
			dest    string
			err     error
		)

		splitVol := SplitVolumeString(vol)
		if len(splitVol) > 3 {
			return nil, nil, nil, fmt.Errorf("%v: %w", vol, volumeFormatErr)
		}

		src = splitVol[0]

		// Support relative paths beginning with ./
		if strings.HasPrefix(src, "./") {
			path, err := filepath.EvalSymlinks(src)
			if err != nil {
				return nil, nil, nil, err
			}
			src, err = filepath.Abs(path)
			if err != nil {
				return nil, nil, nil, err
			}
			splitVol[0] = src
		}

		if len(splitVol) == 1 {
			// This is an anonymous named volume. Only thing given
			// is destination.
			// Name/source will be blank, and populated by libpod.
			src = ""
			dest = splitVol[0]
		} else if len(splitVol) > 1 {
			dest = splitVol[1]
		}
		if len(splitVol) > 2 {
			if options, err = parse.ValidateVolumeOpts(strings.Split(splitVol[2], ",")); err != nil {
				return nil, nil, nil, err
			}
		}

		// Do not check source dir for anonymous volumes
		if len(splitVol) > 1 {
			if len(src) == 0 {
				return nil, nil, nil, errors.New("host directory cannot be empty")
			}
		}

		if strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".") || IsHostWinPath(src) {
			// This is not a named volume
			overlayFlag := false
			chownFlag := false
			upperDirFlag := false
			workDirFlag := false
			for _, o := range options {
				if o == "O" {
					overlayFlag = true

					joinedOpts := strings.Join(options, "")
					if strings.Contains(joinedOpts, "U") {
						chownFlag = true
					}
					if strings.Contains(joinedOpts, "upperdir") {
						upperDirFlag = true
					}
					if strings.Contains(joinedOpts, "workdir") {
						workDirFlag = true
					}
					if (workDirFlag && !upperDirFlag) || (!workDirFlag && upperDirFlag) {
						return nil, nil, nil, errors.New("must set both `upperdir` and `workdir`")
					}
					if len(options) > 2 && (len(options) != 3 || !upperDirFlag || !workDirFlag) || (len(options) == 2 && !chownFlag) {
						return nil, nil, nil, errors.New("can't use 'O' with other options")
					}
				}
			}
			if overlayFlag {
				// This is an overlay volume
				newOverlayVol := new(OverlayVolume)
				newOverlayVol.Destination = dest
				// convert src to absolute path so we don't end up passing
				// relative values as lowerdir for overlay mounts
				source, err := filepath.Abs(src)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("failed while resolving absolute path for source %v for overlay mount: %w", src, err)
				}
				newOverlayVol.Source = source
				newOverlayVol.Options = options

				if vol, ok := overlayVolumes[newOverlayVol.Destination]; ok {
					if vol.Source == newOverlayVol.Source &&
						StringSlicesEqual(vol.Options, newOverlayVol.Options) {
						continue
					}
					return nil, nil, nil, fmt.Errorf("%v: %w", newOverlayVol.Destination, ErrDuplicateDest)
				}
				overlayVolumes[newOverlayVol.Destination] = newOverlayVol
			} else {
				newMount := spec.Mount{
					Destination: dest,
					Type:        define.TypeBind,
					Source:      src,
					Options:     options,
				}
				if vol, ok := mounts[newMount.Destination]; ok {
					if vol.Source == newMount.Source &&
						StringSlicesEqual(vol.Options, newMount.Options) {
						continue
					}

					return nil, nil, nil, fmt.Errorf("%v: %w", newMount.Destination, ErrDuplicateDest)
				}
				mounts[newMount.Destination] = newMount
			}
		} else {
			// This is a named volume
			newNamedVol := new(NamedVolume)
			newNamedVol.Name = src
			newNamedVol.Dest = dest
			newNamedVol.Options = options

			if vol, ok := volumes[newNamedVol.Dest]; ok {
				if vol.Name == newNamedVol.Name {
					continue
				}
				return nil, nil, nil, fmt.Errorf("%v: %w", newNamedVol.Dest, ErrDuplicateDest)
			}
			volumes[newNamedVol.Dest] = newNamedVol
		}

		logrus.Debugf("User mount %s:%s options %v", src, dest, options)
	}

	return mounts, volumes, overlayVolumes, nil
}

// SplitVolumeString Splits a volume string, accounting for Win drive paths
// when running as a WSL linux guest or Windows client
// Format: [[SOURCE-VOLUME|HOST-DIR:]CONTAINER-DIR[:OPTIONS]]
func SplitVolumeString(vol string) []string {
	parts := strings.Split(vol, ":")
	if !shouldResolveWinPaths() {
		return parts
	}

	// Skip extended marker prefix if present
	n := 0
	if strings.HasPrefix(vol, `\\?\`) {
		n = 4
	}

	// Determine if the last part is an absolute path (if true, it means we don't have any options such as ro, rw etc.)
	lastPartIsPath := strings.HasPrefix(parts[len(parts)-1], "/")

	// Case: Volume or relative host path (e.g., "vol-name:/container" or "./hello:/container")
	if lastPartIsPath && len(parts) == 2 {
		return parts
	}

	// Case: Volume or relative host path with options (e.g., "vol-name:/container:ro" or "./hello:/container:ro")
	if !lastPartIsPath && len(parts) == 3 {
		return parts
	}

	// Case: Windows absolute path (e.g., "C:/Users:/mnt:ro")
	if hasWinDriveScheme(vol, n) {
		first := parts[0] + ":" + parts[1]
		parts = parts[1:]
		parts[0] = first
	}

	return parts
}
