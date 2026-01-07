package open

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/sys/unix"
)

const (
	bindFdToPathCommand = "buildah-bind-fd-to-path"
)

func init() {
	reexec.Register(bindFdToPathCommand, bindFdToPathMain)
}

// BindFdToPath creates a bind mount from the open file (which is actually a
// directory) to the specified location.  If it succeeds, the caller will need
// to unmount the targetPath when it's finished using it.  Regardless, it
// closes the passed-in descriptor.
func BindFdToPath(fd uintptr, targetPath string) error {
	f := os.NewFile(fd, "passed-in directory descriptor")
	defer func() {
		if err := f.Close(); err != nil {
			logrus.Debugf("closing descriptor %d after attempting to bind to %q: %v", fd, targetPath, err)
		}
	}()
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := reexec.Command(bindFdToPathCommand)
	cmd.Stdin = pipeReader
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.ExtraFiles = append(cmd.ExtraFiles, f)

	err = cmd.Start()
	pipeReader.Close()
	if err != nil {
		pipeWriter.Close()
		return fmt.Errorf("starting child: %w", err)
	}

	encoder := json.NewEncoder(pipeWriter)
	if err := encoder.Encode(&targetPath); err != nil {
		return fmt.Errorf("sending target path to child: %w", err)
	}
	pipeWriter.Close()
	err = cmd.Wait()
	trimmedOutput := strings.TrimSpace(stdout.String()) + strings.TrimSpace(stderr.String())
	if err != nil {
		if len(trimmedOutput) > 0 {
			err = fmt.Errorf("%s: %w", trimmedOutput, err)
		}
	} else {
		if len(trimmedOutput) > 0 {
			err = errors.New(trimmedOutput)
		}
	}
	return err
}

func bindFdToPathMain() {
	var targetPath string
	decoder := json.NewDecoder(os.Stdin)
	if err := decoder.Decode(&targetPath); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding target path")
		os.Exit(1)
	}
	if err := unix.Fchdir(3); err != nil {
		fmt.Fprintf(os.Stderr, "fchdir(): %v", err)
		os.Exit(1)
	}
	if err := unix.Mount(".", targetPath, "bind", unix.MS_BIND, ""); err != nil {
		fmt.Fprintf(os.Stderr, "bind-mounting passed-in directory to %q: %v", targetPath, err)
		os.Exit(1)
	}
	os.Exit(0)
}
