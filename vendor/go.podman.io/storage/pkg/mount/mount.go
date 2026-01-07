package mount

import (
	"sort"
	"strconv"
	"strings"
)

// mountError holds an error from a mount or unmount operation
type mountError struct {
	op             string
	source, target string
	flags          uintptr
	data           string
	err            error
}

// Error returns a string representation of mountError
func (e *mountError) Error() string {
	out := e.op + " "

	if e.source != "" {
		out += e.source + ":" + e.target
	} else {
		out += e.target
	}

	if e.flags != uintptr(0) {
		out += ", flags: 0x" + strconv.FormatUint(uint64(e.flags), 16)
	}
	if e.data != "" {
		out += ", data: " + e.data
	}

	out += ": " + e.err.Error()
	return out
}

// Cause returns the underlying cause of the error
func (e *mountError) Cause() error {
	return e.err
}

// Unwrap returns the underlying cause of the error
func (e *mountError) Unwrap() error {
	return e.err
}

// Mount will mount filesystem according to the specified configuration, on the
// condition that the target path is *not* already mounted. Options must be
// specified like the mount or fstab unix commands: "opt1=val1,opt2=val2". See
// flags.go for supported option flags.
func Mount(device, target, mType, options string) error {
	flag, data := ParseOptions(options)
	if flag&REMOUNT != REMOUNT {
		if mounted, err := Mounted(target); err != nil || mounted {
			return err
		}
	}
	return mount(device, target, mType, uintptr(flag), data)
}

// ForceMount will mount a filesystem according to the specified configuration,
// *regardless* if the target path is not already mounted. Options must be
// specified like the mount or fstab unix commands: "opt1=val1,opt2=val2". See
// flags.go for supported option flags.
func ForceMount(device, target, mType, options string) error {
	flag, data := ParseOptions(options)
	return mount(device, target, mType, uintptr(flag), data)
}

// Unmount lazily unmounts a filesystem on supported platforms, otherwise
// does a normal unmount.
func Unmount(target string) error {
	return unmount(target, mntDetach)
}

// RecursiveUnmount unmounts the target and all mounts underneath, starting with
// the deepest mount first.
func RecursiveUnmount(target string) error {
	mounts, err := GetMounts()
	if err != nil {
		return err
	}

	// Make the deepest mount be first
	sort.Slice(mounts, func(i, j int) bool {
		return len(mounts[i].Mountpoint) > len(mounts[j].Mountpoint)
	})

	for i, m := range mounts {
		if !strings.HasPrefix(m.Mountpoint, target) {
			continue
		}
		if err := Unmount(m.Mountpoint); err != nil && i == len(mounts)-1 {
			return err
			// Ignore errors for submounts and continue trying to unmount others
			// The final unmount should fail if there are any submounts remaining
		}
	}
	return nil
}

// ForceUnmount lazily unmounts a filesystem on supported platforms,
// otherwise does a normal unmount.
//
// Deprecated: please use Unmount instead, it is identical.
func ForceUnmount(target string) error {
	return unmount(target, mntDetach)
}
