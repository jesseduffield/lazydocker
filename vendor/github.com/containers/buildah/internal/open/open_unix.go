//go:build linux || freebsd || darwin

package open

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"

	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/sys/unix"
)

const (
	inChrootCommand = "buildah-open-in-chroot"
)

func init() {
	reexec.Register(inChrootCommand, inChrootMain)
}

func inChroot(requests requests) results {
	sock, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return results{Err: fmt.Errorf("creating socket pair: %w", err).Error()}
	}
	parentSock := sock[0]
	childSock := sock[1]
	parentEnd := os.NewFile(uintptr(parentSock), "parent end of socket pair")
	childEnd := os.NewFile(uintptr(childSock), "child end of socket pair")
	cmd := reexec.Command(inChrootCommand)
	cmd.ExtraFiles = append(cmd.ExtraFiles, childEnd)
	err = cmd.Start()
	childEnd.Close()
	defer parentEnd.Close()
	if err != nil {
		return results{Err: err.Error()}
	}
	encoder := json.NewEncoder(parentEnd)
	if err := encoder.Encode(&requests); err != nil {
		return results{Err: fmt.Errorf("sending request down socket: %w", err).Error()}
	}
	if err := unix.Shutdown(parentSock, unix.SHUT_WR); err != nil {
		return results{Err: fmt.Errorf("finishing sending request down socket: %w", err).Error()}
	}
	b := make([]byte, 65536)
	oob := make([]byte, 65536)
	n, oobn, _, _, err := unix.Recvmsg(parentSock, b, oob, 0)
	if err != nil {
		return results{Err: fmt.Errorf("receiving message: %w", err).Error()}
	}
	if err := unix.Shutdown(parentSock, unix.SHUT_RD); err != nil {
		return results{Err: fmt.Errorf("finishing socket: %w", err).Error()}
	}
	if n > len(b) {
		return results{Err: fmt.Errorf("too much regular data: %d > %d", n, len(b)).Error()}
	}
	if oobn > len(oob) {
		return results{Err: fmt.Errorf("too much OOB data: %d > %d", oobn, len(oob)).Error()}
	}
	scms, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return results{Err: fmt.Errorf("parsing control message: %w", err).Error()}
	}
	var receivedFds []int
	for i := range scms {
		fds, err := unix.ParseUnixRights(&scms[i])
		if err != nil {
			return results{Err: fmt.Errorf("parsing rights message %d: %w", i, err).Error()}
		}
		receivedFds = append(receivedFds, fds...)
	}
	decoder := json.NewDecoder(bytes.NewReader(b[:n]))
	var result results
	if err := decoder.Decode(&result); err != nil {
		return results{Err: fmt.Errorf("decoding results: %w", err).Error()}
	}
	j := 0
	for i := range result.Open {
		if result.Open[i].Err == "" {
			if j >= len(receivedFds) {
				for _, fd := range receivedFds {
					unix.Close(fd)
				}
				return results{Err: fmt.Errorf("didn't receive enough FDs").Error()}
			}
			result.Open[i].Fd = uintptr(receivedFds[j])
			j++
		}
	}
	return result
}

func inChrootMain() {
	var theseRequests requests
	var theseResults results
	sockFd := 3
	sock := os.NewFile(uintptr(sockFd), "socket connection to parent process")
	defer sock.Close()
	encoder := json.NewEncoder(sock)
	decoder := json.NewDecoder(sock)
	if err := decoder.Decode(&theseRequests); err != nil {
		if err := encoder.Encode(results{Err: fmt.Errorf("decoding request: %w", err).Error()}); err != nil {
			os.Exit(1)
		}
	}
	if theseRequests.Root != "" {
		if err := os.Chdir(theseRequests.Root); err != nil {
			if err := encoder.Encode(results{Err: fmt.Errorf("changing to %q: %w", theseRequests.Root, err).Error()}); err != nil {
				os.Exit(1)
			}
			os.Exit(1)
		}
		if err := unix.Chroot(theseRequests.Root); err != nil {
			if err := encoder.Encode(results{Err: fmt.Errorf("chrooting to %q: %w", theseRequests.Root, err).Error()}); err != nil {
				os.Exit(1)
			}
			os.Exit(1)
		}
		if err := os.Chdir("/"); err != nil {
			if err := encoder.Encode(results{Err: fmt.Errorf("changing to new root: %w", err).Error()}); err != nil {
				os.Exit(1)
			}
			os.Exit(1)
		}
	}
	if theseRequests.Wd != "" {
		if err := os.Chdir(theseRequests.Wd); err != nil {
			if err := encoder.Encode(results{Err: fmt.Errorf("changing to %q in chroot: %w", theseRequests.Wd, err).Error()}); err != nil {
				os.Exit(1)
			}
			os.Exit(1)
		}
	}
	var fds []int
	for _, request := range theseRequests.Open {
		fd, err := unix.Open(request.Path, request.Mode, request.Perms)
		thisResult := result{Fd: uintptr(fd)}
		if err == nil {
			fds = append(fds, fd)
		} else {
			var errno syscall.Errno
			thisResult.Err = err.Error()
			if errors.As(err, &errno) {
				thisResult.Errno = errno
			}
		}
		theseResults.Open = append(theseResults.Open, thisResult)
	}
	rights := unix.UnixRights(fds...)
	inband, err := json.Marshal(&theseResults)
	if err != nil {
		if err := encoder.Encode(results{Err: fmt.Errorf("sending response: %w", err).Error()}); err != nil {
			os.Exit(1)
		}
		os.Exit(1)
	}
	if err := unix.Sendmsg(sockFd, inband, rights, nil, 0); err != nil {
		if err := encoder.Encode(results{Err: fmt.Errorf("sending response: %w", err).Error()}); err != nil {
			os.Exit(1)
		}
		os.Exit(1)
	}
	os.Exit(0)
}
