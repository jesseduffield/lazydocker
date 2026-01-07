//go:build !remote

package libpod

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/api/handlers/utils/apiutil"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/storage/pkg/fileutils"
	"golang.org/x/sys/unix"
)

// MountExists returns true if dest exists in the list of mounts
func MountExists(specMounts []spec.Mount, dest string) bool {
	for _, m := range specMounts {
		if m.Destination == dest {
			return true
		}
	}
	return false
}

func parts(m spec.Mount) int {
	// We must special case a root mount /.
	// The count of "/" and "/proc" are both 1 but of course logically "/" must
	// be mounted before "/proc" as such set the count to 0.
	if m.Destination == "/" {
		return 0
	}
	return strings.Count(filepath.Clean(m.Destination), string(os.PathSeparator))
}

func sortMounts(m []spec.Mount) []spec.Mount {
	slices.SortStableFunc(m, func(a, b spec.Mount) int {
		aLen := parts(a)
		bLen := parts(b)
		if aLen < bLen {
			return -1
		}
		if aLen == bLen {
			return 0
		}
		return 1
	})
	return m
}

// JSONDeepCopy performs a deep copy by performing a JSON encode/decode of the
// given structures. From and To should be identically typed structs.
func JSONDeepCopy(from, to any) error {
	tmp, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(tmp, to)
}

// DefaultSeccompPath returns the path to the default seccomp.json file
// if it exists, first it checks OverrideSeccomp and then default.
// If neither exist function returns ""
func DefaultSeccompPath() (string, error) {
	def, err := config.Default()
	if err != nil {
		return "", err
	}
	if def.Containers.SeccompProfile != "" {
		return def.Containers.SeccompProfile, nil
	}

	err = fileutils.Exists(config.SeccompOverridePath)
	if err == nil {
		return config.SeccompOverridePath, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if err := fileutils.Exists(config.SeccompDefaultPath); err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}
	return config.SeccompDefaultPath, nil
}

// CheckDependencyContainer verifies the given container can be used as a
// dependency of another container.
// Both the dependency to check and the container that will be using the
// dependency must be passed in.
// It is assumed that ctr is locked, and depCtr is unlocked.
func checkDependencyContainer(depCtr, ctr *Container) error {
	state, err := depCtr.State()
	if err != nil {
		return fmt.Errorf("accessing dependency container %s state: %w", depCtr.ID(), err)
	}
	if state == define.ContainerStateRemoving {
		return fmt.Errorf("cannot use container %s as a dependency as it is being removed: %w", depCtr.ID(), define.ErrCtrStateInvalid)
	}

	if depCtr.ID() == ctr.ID() {
		return fmt.Errorf("must specify another container: %w", define.ErrInvalidArg)
	}

	if ctr.config.Pod != "" && depCtr.PodID() != ctr.config.Pod {
		return fmt.Errorf("container has joined pod %s and dependency container %s is not a member of the pod: %w", ctr.config.Pod, depCtr.ID(), define.ErrInvalidArg)
	}

	return nil
}

// hijackWriteError writes an error to a hijacked HTTP session.
func hijackWriteError(toWrite error, cid string, terminal bool, httpBuf *bufio.ReadWriter) {
	if toWrite != nil && !errors.Is(toWrite, define.ErrDetach) {
		errString := fmt.Appendf(nil, "Error: %v\n", toWrite)
		if !terminal {
			// We need a header.
			header := makeHTTPAttachHeader(2, uint32(len(errString)))
			if _, err := httpBuf.Write(header); err != nil {
				logrus.Errorf("Writing header for container %s attach connection error: %v", cid, err)
			}
		}
		if _, err := httpBuf.Write(errString); err != nil {
			logrus.Errorf("Writing error to container %s HTTP attach connection: %v", cid, err)
		}
		if err := httpBuf.Flush(); err != nil {
			logrus.Errorf("Flushing HTTP buffer for container %s HTTP attach connection: %v", cid, err)
		}
	}
}

// hijackWriteErrorAndClose writes an error to a hijacked HTTP session and
// closes it. Intended to HTTPAttach function.
// If error is nil, it will not be written; we'll only close the connection.
func hijackWriteErrorAndClose(toWrite error, cid string, terminal bool, httpCon io.Closer, httpBuf *bufio.ReadWriter) {
	hijackWriteError(toWrite, cid, terminal, httpBuf)

	if err := httpCon.Close(); err != nil {
		logrus.Errorf("Closing container %s HTTP attach connection: %v", cid, err)
	}
}

// makeHTTPAttachHeader makes an 8-byte HTTP header for a buffer of the given
// length and stream. Accepts an integer indicating which stream we are sending
// to (STDIN = 0, STDOUT = 1, STDERR = 2).
func makeHTTPAttachHeader(stream byte, length uint32) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], length)
	return header
}

// writeHijackHeader writes a header appropriate for the type of HTTP Hijack
// that occurred in a hijacked HTTP connection used for attach.
func writeHijackHeader(r *http.Request, conn io.Writer, tty bool) {
	// AttachHeader is the literal header sent for upgraded/hijacked connections for
	// attach, sourced from Docker at:
	// https://raw.githubusercontent.com/moby/moby/b95fad8e51bd064be4f4e58a996924f343846c85/api/server/router/container/container_routes.go
	// Using literally to ensure compatibility with existing clients.

	// New docker API uses a different header for the non tty case.
	// Lets do the same for libpod. Only do this for the new api versions to not break older clients.
	header := "application/vnd.docker.raw-stream"
	if !tty {
		version := "4.7.0"
		if !apiutil.IsLibpodRequest(r) {
			version = "1.42.0" // docker only used two digest "1.42" but our semver lib needs the extra .0 to work
		}
		if _, err := apiutil.SupportedVersion(r, ">= "+version); err == nil {
			header = "application/vnd.docker.multiplexed-stream"
		}
	}

	c := r.Header.Get("Connection")
	proto := r.Header.Get("Upgrade")
	if len(proto) == 0 || !strings.EqualFold(c, "Upgrade") {
		// OK - can't upgrade if not requested or protocol is not specified
		fmt.Fprintf(conn,
			"HTTP/1.1 200 OK\r\nContent-Type: %s\r\n\r\n", header)
	} else {
		// Upgraded
		fmt.Fprintf(conn,
			"HTTP/1.1 101 UPGRADED\r\nContent-Type: %s\r\nConnection: Upgrade\r\nUpgrade: %s\r\n\r\n",
			header, proto)
	}
}

// Generate inspect-formatted port mappings from the format used in our config file
func makeInspectPortBindings(bindings []types.PortMapping) map[string][]define.InspectHostPort {
	portBindings := make(map[string][]define.InspectHostPort)
	for _, port := range bindings {
		for protocol := range strings.SplitSeq(port.Protocol, ",") {
			for i := uint16(0); i < port.Range; i++ {
				key := fmt.Sprintf("%d/%s", port.ContainerPort+i, protocol)
				hostPorts := portBindings[key]
				var hostIP = port.HostIP
				if len(port.HostIP) == 0 {
					hostIP = "0.0.0.0"
				}
				hostPorts = append(hostPorts, define.InspectHostPort{
					HostIP:   hostIP,
					HostPort: strconv.FormatUint(uint64(port.HostPort+i), 10),
				})
				portBindings[key] = hostPorts
			}
		}
	}

	return portBindings
}

// Add exposed ports to inspect port bindings. These must be done on a per-container basis, not per-netns basis.
func addInspectPortsExpose(expose map[uint16][]string, portBindings map[string][]define.InspectHostPort) {
	for port, protocols := range expose {
		for _, protocol := range protocols {
			key := fmt.Sprintf("%d/%s", port, protocol)
			if _, ok := portBindings[key]; !ok {
				portBindings[key] = nil
			}
		}
	}
}

// Write a given string to a new file at a given path.
// Will error if a file with the given name already exists.
// Will be chown'd to the UID/GID provided and have the provided SELinux label
// set.
func writeStringToPath(path, contents, mountLabel string, uid, gid int) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("unable to create %s: %w", path, err)
	}
	defer f.Close()
	if err := f.Chown(uid, gid); err != nil {
		return err
	}

	if _, err := f.WriteString(contents); err != nil {
		return fmt.Errorf("unable to write %s: %w", path, err)
	}
	// Relabel runDirResolv for the container
	if err := label.Relabel(path, mountLabel, false); err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			logrus.Debugf("Labeling not supported on %q", path)
			return nil
		}
		return err
	}

	return nil
}

// If the given path exists, evaluate any symlinks in it. If it does not, clean
// the path and return it. Used to try and verify path equality in a somewhat
// sane fashion.
func evalSymlinksIfExists(toCheck string) (string, error) {
	checkedVal, err := filepath.EvalSymlinks(toCheck)
	if err != nil {
		// If the error is not ENOENT, something more serious has gone
		// wrong, return it.
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		// This is an ENOENT. On ENOENT, EvalSymlinks returns "".
		// We don't want that. Return a cleaned version of the original
		// path.
		return filepath.Clean(toCheck), nil
	}
	return checkedVal, nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
