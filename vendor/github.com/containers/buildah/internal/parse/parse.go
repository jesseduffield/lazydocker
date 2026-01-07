package parse

import (
	"fmt"
	"path/filepath"
	"strings"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/common/pkg/parse"
	"go.podman.io/storage/pkg/fileutils"
)

// ValidateVolumeMountHostDir validates the host path of buildah --volume
func ValidateVolumeMountHostDir(hostDir string) error {
	if !filepath.IsAbs(hostDir) {
		return fmt.Errorf("invalid host path, must be an absolute path %q", hostDir)
	}
	if err := fileutils.Exists(hostDir); err != nil {
		return err
	}
	return nil
}

// RevertEscapedColon converts "\:" to ":"
func RevertEscapedColon(source string) string {
	return strings.ReplaceAll(source, "\\:", ":")
}

// SplitStringWithColonEscape splits string into slice by colon. Backslash-escaped colon (i.e. "\:") will not be regarded as separator
func SplitStringWithColonEscape(str string) []string {
	result := make([]string, 0, 3)
	sb := &strings.Builder{}
	for idx, r := range str {
		if r == ':' {
			// the colon is backslash-escaped
			if idx-1 > 0 && str[idx-1] == '\\' {
				sb.WriteRune(r)
			} else {
				// os.Stat will fail if path contains escaped colon
				result = append(result, RevertEscapedColon(sb.String()))
				sb.Reset()
			}
		} else {
			sb.WriteRune(r)
		}
	}
	if sb.Len() > 0 {
		result = append(result, RevertEscapedColon(sb.String()))
	}
	return result
}

// Volume parses the input of --volume
func Volume(volume string) (specs.Mount, error) {
	mount := specs.Mount{}
	arr := SplitStringWithColonEscape(volume)
	if len(arr) < 2 {
		return mount, fmt.Errorf("incorrect volume format %q, should be host-dir:ctr-dir[:option]", volume)
	}
	if err := ValidateVolumeMountHostDir(arr[0]); err != nil {
		return mount, err
	}
	if err := parse.ValidateVolumeCtrDir(arr[1]); err != nil {
		return mount, err
	}
	mountOptions := ""
	if len(arr) > 2 {
		mountOptions = arr[2]
		if _, err := parse.ValidateVolumeOpts(strings.Split(arr[2], ",")); err != nil {
			return mount, err
		}
	}
	mountOpts := strings.Split(mountOptions, ",")
	mount.Source = arr[0]
	mount.Destination = arr[1]
	mount.Type = "rbind"
	mount.Options = mountOpts
	return mount, nil
}
