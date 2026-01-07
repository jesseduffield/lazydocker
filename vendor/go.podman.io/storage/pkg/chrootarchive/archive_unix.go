//go:build !windows && !darwin

package chrootarchive

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"

	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/reexec"
)

type unpackDestination struct {
	root *os.File
	dest string
}

func (dst *unpackDestination) Close() error {
	return dst.root.Close()
}

// tarOptionsDescriptor is passed as an extra file
const tarOptionsDescriptor = 3

// rootFileDescriptor is passed as an extra file
const rootFileDescriptor = 4

// procPathForFd gives us a string for a descriptor.
// Note that while Linux supports actually *reading* this
// path, FreeBSD and other platforms don't; but in this codebase
// we only compare strings.
func procPathForFd(fd int) string {
	return fmt.Sprintf("/proc/self/fd/%d", fd)
}

// untar is the entry-point for storage-untar on re-exec. This is not used on
// Windows as it does not support chroot, hence no point sandboxing through
// chroot and rexec.
func untar() {
	runtime.LockOSThread()
	flag.Parse()

	var options archive.TarOptions

	// read the options from the pipe "ExtraFiles"
	if err := json.NewDecoder(os.NewFile(tarOptionsDescriptor, "options")).Decode(&options); err != nil {
		fatal(err)
	}

	dst := flag.Arg(0)
	var root string
	if len(flag.Args()) > 1 {
		root = flag.Arg(1)
	}

	// FreeBSD doesn't have proc/self, but we can handle it here
	if root == procPathForFd(rootFileDescriptor) {
		// Take ownership to ensure it's closed; no need to leak
		// this afterwards.
		rootFd := os.NewFile(rootFileDescriptor, "tar-root")
		defer rootFd.Close()
		if err := unix.Fchdir(int(rootFd.Fd())); err != nil {
			fatal(err)
		}
		root = "."
	} else if root == "" {
		root = dst
	}

	if err := chroot(root); err != nil {
		fatal(err)
	}

	if err := archive.Unpack(os.Stdin, dst, &options); err != nil {
		fatal(err)
	}
	// fully consume stdin in case it is zero padded
	if _, err := flush(os.Stdin); err != nil {
		fatal(err)
	}

	os.Exit(0)
}

// newUnpackDestination takes a root directory and a destination which
// must be underneath it, and returns an object that can unpack
// in the target root using a file descriptor.
func newUnpackDestination(root, dest string) (*unpackDestination, error) {
	if root == "" {
		return nil, errors.New("must specify a root to chroot to")
	}
	relDest, err := filepath.Rel(root, dest)
	if err != nil {
		return nil, err
	}
	if relDest == "." {
		relDest = "/"
	}
	if relDest[0] != '/' {
		relDest = "/" + relDest
	}

	rootfdRaw, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: root, Err: err}
	}
	return &unpackDestination{
		root: os.NewFile(uintptr(rootfdRaw), "rootfs"),
		dest: relDest,
	}, nil
}

func invokeUnpack(decompressedArchive io.Reader, dest *unpackDestination, options *archive.TarOptions) error {
	// We can't pass a potentially large exclude list directly via cmd line
	// because we easily overrun the kernel's max argument/environment size
	// when the full image list is passed (e.g. when this is used by
	// `docker load`). We will marshall the options via a pipe to the
	// child
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("untar pipe failure: %w", err)
	}

	cmd := reexec.Command("storage-untar", dest.dest, procPathForFd(rootFileDescriptor))
	cmd.Stdin = decompressedArchive

	// If you change this, change tarOptionsDescriptor above
	cmd.ExtraFiles = append(cmd.ExtraFiles, r) // fd 3
	// If you change this, change rootFileDescriptor above too
	cmd.ExtraFiles = append(cmd.ExtraFiles, dest.root) // fd 4
	output := bytes.NewBuffer(nil)
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		w.Close()
		return fmt.Errorf("untar error on re-exec cmd: %w", err)
	}

	// write the options to the pipe for the untar exec to read
	if err := json.NewEncoder(w).Encode(options); err != nil {
		w.Close()
		return fmt.Errorf("untar json encode to pipe failed: %w", err)
	}
	w.Close()

	if err := cmd.Wait(); err != nil {
		errorOut := fmt.Errorf("unpacking failed (error: %w; output: %s)", err, output)
		// when `xz -d -c -q | storage-untar ...` failed on storage-untar side,
		// we need to exhaust `xz`'s output, otherwise the `xz` side will be
		// pending on write pipe forever
		if _, err := io.Copy(io.Discard, decompressedArchive); err != nil {
			return fmt.Errorf("%w\nexhausting input failed (error: %w)", errorOut, err)
		}

		return errorOut
	}
	return nil
}

func tar() {
	runtime.LockOSThread()
	flag.Parse()

	src := flag.Arg(0)
	var root string
	if len(flag.Args()) > 1 {
		root = flag.Arg(1)
	}

	if root == "" {
		root = src
	}

	if err := realChroot(root); err != nil {
		fatal(err)
	}

	var options archive.TarOptions
	if err := json.NewDecoder(os.Stdin).Decode(&options); err != nil {
		fatal(err)
	}

	rdr, err := archive.TarWithOptions(src, &options)
	if err != nil {
		fatal(err)
	}
	defer rdr.Close()

	if _, err := io.Copy(os.Stdout, rdr); err != nil {
		fatal(err)
	}

	os.Exit(0)
}

func invokePack(srcPath string, options *archive.TarOptions, root string) (io.ReadCloser, error) {
	if root == "" {
		return nil, errors.New("root path must not be empty")
	}

	relSrc, err := filepath.Rel(root, srcPath)
	if err != nil {
		return nil, err
	}
	if relSrc == "." {
		relSrc = "/"
	}
	if relSrc[0] != '/' {
		relSrc = "/" + relSrc
	}

	// make sure we didn't trim a trailing slash with the call to `Rel`
	if strings.HasSuffix(srcPath, "/") && !strings.HasSuffix(relSrc, "/") {
		relSrc += "/"
	}

	cmd := reexec.Command("storage-tar", relSrc, root)

	errBuff := bytes.NewBuffer(nil)
	cmd.Stderr = errBuff

	tarR, tarW := io.Pipe()
	cmd.Stdout = tarW

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("getting options pipe for tar process: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tar error on re-exec cmd: %w", err)
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("processing tar file(%s): %w", errBuff, err)
		}
		tarW.CloseWithError(err)
	}()

	if err := json.NewEncoder(stdin).Encode(options); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("tar json encode to pipe failed: %w", err)
	}
	stdin.Close()

	return tarR, nil
}
