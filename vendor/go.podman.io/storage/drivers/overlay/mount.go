//go:build linux

package overlay

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/sys/unix"
)

func init() {
	reexec.Register("storage-mountfrom", mountOverlayFromMain)
}

func fatal(err error) {
	fmt.Fprint(os.Stderr, err)
	os.Exit(1)
}

type mountOptions struct {
	Device string
	Target string
	Type   string
	Label  string
	Flag   uint32
}

func mountOverlayFrom(dir, device, target, mType string, flags uintptr, label string) error {
	options := &mountOptions{
		Device: device,
		Target: target,
		Type:   mType,
		Flag:   uint32(flags),
		Label:  label,
	}

	cmd := reexec.Command("storage-mountfrom", dir)
	w, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mountfrom error on pipe creation: %w", err)
	}

	output := bytes.NewBuffer(nil)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		w.Close()
		return fmt.Errorf("mountfrom error on re-exec cmd: %w", err)
	}
	// write the options to the pipe for the untar exec to read
	if err := json.NewEncoder(w).Encode(options); err != nil {
		w.Close()
		return fmt.Errorf("mountfrom json encode to pipe failed: %w", err)
	}
	w.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("mountfrom re-exec output: %s: error: %w", output, err)
	}
	return nil
}

// mountfromMain is the entry-point for storage-mountfrom on re-exec.
func mountOverlayFromMain() {
	runtime.LockOSThread()
	flag.Parse()

	var options *mountOptions

	if err := json.NewDecoder(os.Stdin).Decode(&options); err != nil {
		fatal(err)
	}

	// Mount the arguments passed from the specified directory. Some of the
	// paths mentioned in the values we pass to the kernel are relative to
	// the specified directory.
	homedir := flag.Arg(0)
	if err := os.Chdir(homedir); err != nil {
		fatal(err)
	}

	pageSize := unix.Getpagesize()
	if len(options.Label) < pageSize {
		if err := unix.Mount(options.Device, options.Target, options.Type, uintptr(options.Flag), options.Label); err != nil {
			fatal(err)
		}
		os.Exit(0)
	}

	// Those arguments still took up too much space.  Open the diff
	// directories and use their descriptor numbers as lowers, using
	// /proc/self/fd as the current directory.

	// Split out the various options, since we need to manipulate the
	// paths, but we don't want to mess with other options.
	var upperk, upperv, workk, workv, lowerk, lowerv, labelk, labelv, others string
	for arg := range strings.SplitSeq(options.Label, ",") {
		key, val, _ := strings.Cut(arg, "=")
		switch key {
		case "upperdir":
			upperk = "upperdir="
			upperv = val
		case "workdir":
			workk = "workdir="
			workv = val
		case "lowerdir":
			lowerk = "lowerdir="
			lowerv = val
		case "label":
			labelk = "label="
			labelv = val
		default:
			if others == "" {
				others = arg
			} else {
				others = others + "," + arg
			}
		}
	}

	// Make sure upperdir, workdir, and the target are absolute paths.
	if upperv != "" && !filepath.IsAbs(upperv) {
		upperv = filepath.Join(homedir, upperv)
	}
	if workv != "" && !filepath.IsAbs(workv) {
		workv = filepath.Join(homedir, workv)
	}
	if !filepath.IsAbs(options.Target) {
		options.Target = filepath.Join(homedir, options.Target)
	}

	// Get a descriptor for each lower, and use that descriptor's name as
	// the new value for the list of lowers, because it's shorter.
	if lowerv != "" {
		var newLowers []string
		dataOnly := false
		for lowerPath := range strings.SplitSeq(lowerv, ":") {
			if lowerPath == "" {
				dataOnly = true
				continue
			}
			lowerFd, err := unix.Open(lowerPath, unix.O_RDONLY, 0)
			if err != nil {
				fatal(err)
			}
			var lower string
			if dataOnly {
				lower = fmt.Sprintf(":%d", lowerFd)
				dataOnly = false
			} else {
				lower = fmt.Sprintf("%d", lowerFd)
			}
			newLowers = append(newLowers, lower)
		}
		lowerv = strings.Join(newLowers, ":")
	}

	// Reconstruct the Label field.
	options.Label = upperk + upperv + "," + workk + workv + "," + lowerk + lowerv + "," + labelk + labelv + "," + others
	options.Label = strings.ReplaceAll(options.Label, ",,", ",")

	// Okay, try this, if we managed to make the arguments fit.
	var err error
	if len(options.Label) < pageSize {
		if err := os.Chdir("/proc/self/fd"); err != nil {
			fatal(err)
		}
		err = unix.Mount(options.Device, options.Target, options.Type, uintptr(options.Flag), options.Label)
	} else {
		err = fmt.Errorf("cannot mount layer, mount data %q too large %d >= page size %d", options.Label, len(options.Label), pageSize)
	}

	// Clean up.
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating overlay mount to %s, mount_data=%q\n", options.Target, options.Label)
		fatal(err)
	}

	os.Exit(0)
}
