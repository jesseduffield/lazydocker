package parse

// this package contains functions that parse and validate
// user input and is shared either amongst container engine subcommands

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"go.podman.io/storage/pkg/fileutils"
)

// ValidateVolumeOpts validates a volume's options.
func ValidateVolumeOpts(options []string) ([]string, error) {
	var foundRootPropagation, foundRWRO, foundLabelChange, bindType, foundExec, foundDev, foundSuid, foundChown, foundUpperDir, foundWorkDir, foundCopy, foundCopySymlink int
	finalOpts := make([]string, 0, len(options))
	for _, opt := range options {
		// support advanced options like upperdir=/path, workdir=/path
		if strings.Contains(opt, "upperdir") {
			foundUpperDir++
			if foundUpperDir > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 upperdir per overlay", strings.Join(options, ", "))
			}
			finalOpts = append(finalOpts, opt)
			continue
		}
		if strings.Contains(opt, "workdir") {
			foundWorkDir++
			if foundWorkDir > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 workdir per overlay", strings.Join(options, ", "))
			}
			finalOpts = append(finalOpts, opt)
			continue
		}
		if strings.HasPrefix(opt, "idmap") {
			finalOpts = append(finalOpts, opt)
			continue
		}

		switch opt {
		case "noexec", "exec":
			foundExec++
			if foundExec > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'noexec' or 'exec' option", strings.Join(options, ", "))
			}
		case "nodev", "dev":
			foundDev++
			if foundDev > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'nodev' or 'dev' option", strings.Join(options, ", "))
			}
		case "nosuid", "suid":
			foundSuid++
			if foundSuid > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'nosuid' or 'suid' option", strings.Join(options, ", "))
			}
		case "rw", "ro":
			foundRWRO++
			if foundRWRO > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'rw' or 'ro' option", strings.Join(options, ", "))
			}
		case "z", "Z", "O":
			foundLabelChange++
			if foundLabelChange > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'z', 'Z', or 'O' option", strings.Join(options, ", "))
			}
		case "U":
			foundChown++
			if foundChown > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'U' option", strings.Join(options, ", "))
			}
		case "private", "rprivate", "shared", "rshared", "slave", "rslave", "unbindable", "runbindable":
			foundRootPropagation++
			if foundRootPropagation > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 '[r]shared', '[r]private', '[r]slave' or '[r]unbindable' option", strings.Join(options, ", "))
			}
		case "bind", "rbind":
			bindType++
			if bindType > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 '[r]bind' option", strings.Join(options, ", "))
			}
		case "cached", "delegated":
			// The discarded ops are OS X specific volume options
			// introduced in a recent Docker version.
			// They have no meaning on Linux, so here we silently
			// drop them. This matches Docker's behavior (the options
			// are intended to be always safe to use, even not on OS
			// X).
			continue
		case "copy", "nocopy":
			foundCopy++
			if foundCopy > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'copy' or 'nocopy' option", strings.Join(options, ", "))
			}
		case "no-dereference":
			foundCopySymlink++
			if foundCopySymlink > 1 {
				return nil, fmt.Errorf("invalid options %q, can only specify 1 'no-dereference' option", strings.Join(options, ", "))
			}
		default:
			return nil, fmt.Errorf("invalid option type %q", opt)
		}
		finalOpts = append(finalOpts, opt)
	}
	return finalOpts, nil
}

// Device parses device mapping string to a src, dest & permissions string
// Valid values for device looklike:
//
//	'/dev/sdc"
//	'/dev/sdc:/dev/xvdc"
//	'/dev/sdc:/dev/xvdc:rwm"
//	'/dev/sdc:rm"
func Device(device string) (src, dest, permissions string, err error) {
	permissions = "rwm"
	arr := strings.Split(device, ":")
	switch len(arr) {
	case 3:
		if !isValidDeviceMode(arr[2]) {
			return "", "", "", fmt.Errorf("invalid device mode: %s", arr[2])
		}
		permissions = arr[2]
		fallthrough
	case 2:
		if isValidDeviceMode(arr[1]) {
			permissions = arr[1]
		} else {
			if arr[1] == "" || arr[1][0] != '/' {
				return "", "", "", fmt.Errorf("invalid device mode: %s", arr[1])
			}
			dest = arr[1]
		}
		fallthrough
	case 1:
		if len(arr[0]) > 0 {
			src = arr[0]
			break
		}
		fallthrough
	default:
		return "", "", "", fmt.Errorf("invalid device specification: %s", device)
	}

	if dest == "" {
		dest = src
	}
	return src, dest, permissions, nil
}

// isValidDeviceMode checks if the mode for device is valid or not.
// isValid mode is a composition of r (read), w (write), and m (mknod).
func isValidDeviceMode(mode string) bool {
	legalDeviceMode := map[rune]bool{
		'r': true,
		'w': true,
		'm': true,
	}
	if mode == "" {
		return false
	}
	for _, c := range mode {
		if !legalDeviceMode[c] {
			return false
		}
		legalDeviceMode[c] = false
	}
	return true
}

// ValidateVolumeHostDir validates a volume mount's source directory.
func ValidateVolumeHostDir(hostDir string) error {
	if hostDir == "" {
		return errors.New("host directory cannot be empty")
	}
	if filepath.IsAbs(hostDir) {
		if err := fileutils.Exists(hostDir); err != nil {
			return err
		}
	}
	// If hostDir is not an absolute path, that means the user wants to create a
	// named volume. This will be done later on in the code.
	return nil
}

// ValidateVolumeCtrDir validates a volume mount's destination directory.
func ValidateVolumeCtrDir(ctrDir string) error {
	if ctrDir == "" {
		return errors.New("container directory cannot be empty")
	}
	if !path.IsAbs(ctrDir) {
		return fmt.Errorf("invalid container path %q, must be an absolute path", ctrDir)
	}
	return nil
}
