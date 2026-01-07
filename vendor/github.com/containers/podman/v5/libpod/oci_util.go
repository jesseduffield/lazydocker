//go:build !remote

package libpod

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
)

// Timeout before declaring that runtime has failed to kill a given
// container
const killContainerTimeout = 5 * time.Second

// ociError is used to parse the OCI runtime JSON log.  It is not part of the
// OCI runtime specifications, it follows what runc does
type ociError struct {
	Level string `json:"level,omitempty"`
	Time  string `json:"time,omitempty"`
	Msg   string `json:"msg,omitempty"`
}

// Bind ports to keep them closed on the host
func bindPorts(ports []types.PortMapping) ([]*os.File, error) {
	var files []*os.File
	sctpWarning := true
	for _, port := range ports {
		isV6 := net.ParseIP(port.HostIP).To4() == nil
		if port.HostIP == "" {
			isV6 = false
		}
		protocols := strings.SplitSeq(port.Protocol, ",")
		for protocol := range protocols {
			for i := uint16(0); i < port.Range; i++ {
				f, err := bindPort(protocol, port.HostIP, port.HostPort+i, isV6, &sctpWarning)
				if err != nil {
					// close all open ports in case of early error so we do not
					// rely garbage  collector to close them
					for _, f := range files {
						f.Close()
					}
					return nil, err
				}
				if f != nil {
					files = append(files, f)
				}
			}
		}
	}
	return files, nil
}

func bindPort(protocol, hostIP string, port uint16, isV6 bool, sctpWarning *bool) (*os.File, error) {
	var file *os.File
	switch protocol {
	case "udp":
		var (
			addr *net.UDPAddr
			err  error
		)
		if isV6 {
			addr, err = net.ResolveUDPAddr("udp6", fmt.Sprintf("[%s]:%d", hostIP, port))
		} else {
			addr, err = net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", hostIP, port))
		}
		if err != nil {
			return nil, fmt.Errorf("cannot resolve the UDP address: %w", err)
		}

		proto := "udp4"
		if isV6 {
			proto = "udp6"
		}
		server, err := net.ListenUDP(proto, addr)
		if err != nil {
			return nil, fmt.Errorf("cannot listen on the UDP port: %w", err)
		}
		file, err = server.File()
		if err != nil {
			return nil, fmt.Errorf("cannot get file for UDP socket: %w", err)
		}
		// close the listener
		// note that this does not affect the fd, see the godoc for server.File()
		err = server.Close()
		if err != nil {
			logrus.Warnf("Failed to close connection: %v", err)
		}

	case "tcp":
		var (
			addr *net.TCPAddr
			err  error
		)
		if isV6 {
			addr, err = net.ResolveTCPAddr("tcp6", fmt.Sprintf("[%s]:%d", hostIP, port))
		} else {
			addr, err = net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", hostIP, port))
		}
		if err != nil {
			return nil, fmt.Errorf("cannot resolve the TCP address: %w", err)
		}

		proto := "tcp4"
		if isV6 {
			proto = "tcp6"
		}
		server, err := net.ListenTCP(proto, addr)
		if err != nil {
			return nil, fmt.Errorf("cannot listen on the TCP port: %w", err)
		}
		file, err = server.File()
		if err != nil {
			return nil, fmt.Errorf("cannot get file for TCP socket: %w", err)
		}
		// close the listener
		// note that this does not affect the fd, see the godoc for server.File()
		err = server.Close()
		if err != nil {
			logrus.Warnf("Failed to close connection: %v", err)
		}

	case "sctp":
		if *sctpWarning {
			logrus.Info("Port reservation for SCTP is not supported")
			*sctpWarning = false
		}
	default:
		return nil, fmt.Errorf("unknown protocol %s", protocol)
	}
	return file, nil
}

func getOCIRuntimeError(name, runtimeMsg string) error {
	includeFullOutput := logrus.GetLevel() == logrus.DebugLevel

	if match := regexp.MustCompile("(?i).*permission denied.*|.*operation not permitted.*").FindString(runtimeMsg); match != "" {
		errStr := match
		if includeFullOutput {
			errStr = runtimeMsg
		}
		return fmt.Errorf("%s: %s: %w", name, strings.Trim(errStr, "\n"), define.ErrOCIRuntimePermissionDenied)
	}
	if match := regexp.MustCompile("(?i).*executable file not found in.*|.*no such file or directory.*|.*open executable.*").FindString(runtimeMsg); match != "" {
		errStr := match
		if includeFullOutput {
			errStr = runtimeMsg
		}
		return fmt.Errorf("%s: %s: %w", name, strings.Trim(errStr, "\n"), define.ErrOCIRuntimeNotFound)
	}
	if match := regexp.MustCompile("`/proc/[a-z0-9-].+/attr.*`").FindString(runtimeMsg); match != "" {
		errStr := match
		if includeFullOutput {
			errStr = runtimeMsg
		}
		if strings.HasSuffix(match, "/exec`") {
			return fmt.Errorf("%s: %s: %w", name, strings.Trim(errStr, "\n"), define.ErrSetSecurityAttribute)
		} else if strings.HasSuffix(match, "/current`") {
			return fmt.Errorf("%s: %s: %w", name, strings.Trim(errStr, "\n"), define.ErrGetSecurityAttribute)
		}
		return fmt.Errorf("%s: %s: %w", name, strings.Trim(errStr, "\n"), define.ErrSecurityAttribute)
	}
	return fmt.Errorf("%s: %s: %w", name, strings.Trim(runtimeMsg, "\n"), define.ErrOCIRuntime)
}
