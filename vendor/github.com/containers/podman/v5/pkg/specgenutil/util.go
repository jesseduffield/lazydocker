package specgenutil

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	storageTypes "go.podman.io/storage/types"
)

// ReadPodIDFile reads the specified file and returns its content (i.e., first
// line).
func ReadPodIDFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading pod ID file: %w", err)
	}
	return strings.Split(string(content), "\n")[0], nil
}

// ReadPodIDFiles reads the specified files and returns their content (i.e.,
// first line).
func ReadPodIDFiles(files []string) ([]string, error) {
	ids := []string{}
	for _, file := range files {
		id, err := ReadPodIDFile(file)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// CreateExpose parses user-provided exposed port definitions and converts them
// into SpecGen format.
// TODO: The SpecGen format should really handle ranges more sanely - we could
// be massively inflating what is sent over the wire with a large range.
func CreateExpose(expose []string) (map[uint16]string, error) {
	toReturn := make(map[uint16]string)

	for _, e := range expose {
		// Check for protocol
		proto := "tcp"
		splitProto := strings.Split(e, "/")
		if len(splitProto) > 2 {
			return nil, errors.New("invalid expose format - protocol can only be specified once")
		} else if len(splitProto) == 2 {
			proto = splitProto[1]
		}

		// Check for a range
		start, len, err := parseAndValidateRange(splitProto[0])
		if err != nil {
			return nil, err
		}

		var index uint16
		for index = range len {
			portNum := start + index
			protocols, ok := toReturn[portNum]
			if !ok {
				toReturn[portNum] = proto
			} else {
				newProto := strings.Join(append(strings.Split(protocols, ","), strings.Split(proto, ",")...), ",")
				toReturn[portNum] = newProto
			}
		}
	}

	return toReturn, nil
}

// CreatePortBindings iterates ports mappings into SpecGen format.
func CreatePortBindings(ports []string) ([]types.PortMapping, error) {
	// --publish is formatted as follows:
	// [[hostip:]hostport[-endPort]:]containerport[-endPort][/protocol]
	toReturn := make([]types.PortMapping, 0, len(ports))

	for _, p := range ports {
		var (
			ctrPort                 string
			proto, hostIP, hostPort *string
		)

		splitProto := strings.Split(p, "/")
		switch len(splitProto) {
		case 1:
			// No protocol was provided
		case 2:
			proto = &(splitProto[1])
		default:
			return nil, errors.New("invalid port format - protocol can only be specified once")
		}

		remainder := splitProto[0]
		haveV6 := false

		// Check for an IPv6 address in brackets
		splitV6 := strings.Split(remainder, "]")
		switch len(splitV6) {
		case 1:
			// Do nothing, proceed as before
		case 2:
			// We potentially have an IPv6 address
			haveV6 = true
			if !strings.HasPrefix(splitV6[0], "[") {
				return nil, errors.New("invalid port format - IPv6 addresses must be enclosed by []")
			}
			if !strings.HasPrefix(splitV6[1], ":") {
				return nil, errors.New("invalid port format - IPv6 address must be followed by a colon (':')")
			}
			ipNoPrefix := strings.TrimPrefix(splitV6[0], "[")
			hostIP = &ipNoPrefix
			remainder = strings.TrimPrefix(splitV6[1], ":")
		default:
			return nil, errors.New("invalid port format - at most one IPv6 address can be specified in a --publish")
		}

		splitPort := strings.Split(remainder, ":")
		switch len(splitPort) {
		case 1:
			if haveV6 {
				return nil, errors.New("invalid port format - must provide host and destination port if specifying an IP")
			}
			ctrPort = splitPort[0]
		case 2:
			hostPort = &(splitPort[0])
			ctrPort = splitPort[1]
		case 3:
			if haveV6 {
				return nil, errors.New("invalid port format - when v6 address specified, must be [ipv6]:hostPort:ctrPort")
			}
			hostIP = &(splitPort[0])
			hostPort = &(splitPort[1])
			ctrPort = splitPort[2]
		default:
			return nil, errors.New("invalid port format - format is [[hostIP:]hostPort:]containerPort")
		}

		newPort, err := parseSplitPort(hostIP, hostPort, ctrPort, proto)
		if err != nil {
			return nil, err
		}

		toReturn = append(toReturn, newPort)
	}

	return toReturn, nil
}

// parseSplitPort parses individual components of the --publish flag to produce
// a single port mapping in SpecGen format.
func parseSplitPort(hostIP, hostPort *string, ctrPort string, protocol *string) (types.PortMapping, error) {
	newPort := types.PortMapping{}
	if ctrPort == "" {
		return newPort, errors.New("must provide a non-empty container port to publish")
	}
	ctrStart, ctrLen, err := parseAndValidateRange(ctrPort)
	if err != nil {
		return newPort, fmt.Errorf("parsing container port: %w", err)
	}
	newPort.ContainerPort = ctrStart
	newPort.Range = ctrLen

	if protocol != nil {
		if *protocol == "" {
			return newPort, errors.New("must provide a non-empty protocol to publish")
		}
		newPort.Protocol = *protocol
	}
	if hostIP != nil {
		if *hostIP == "" {
			return newPort, errors.New("must provide a non-empty container host IP to publish")
		} else if *hostIP != "0.0.0.0" {
			// If hostIP is 0.0.0.0, leave it unset - CNI treats
			// 0.0.0.0 and empty differently, Docker does not.
			testIP := net.ParseIP(*hostIP)
			if testIP == nil {
				return newPort, fmt.Errorf("cannot parse %q as an IP address", *hostIP)
			}
			newPort.HostIP = testIP.String()
		}
	}
	if hostPort != nil {
		if *hostPort == "" {
			// Set 0 as a placeholder. The server side of Specgen
			// will find a random, open, unused port to use.
			newPort.HostPort = 0
		} else {
			hostStart, hostLen, err := parseAndValidateRange(*hostPort)
			if err != nil {
				return newPort, fmt.Errorf("parsing host port: %w", err)
			}
			if hostLen != ctrLen {
				return newPort, fmt.Errorf("host and container port ranges have different lengths: %d vs %d", hostLen, ctrLen)
			}
			newPort.HostPort = hostStart
		}
	}

	hport := newPort.HostPort
	logrus.Debugf("Adding port mapping from %d to %d length %d protocol %q", hport, newPort.ContainerPort, newPort.Range, newPort.Protocol)

	return newPort, nil
}

// Parse and validate a port range.
// Returns start port, length of range, error.
func parseAndValidateRange(portRange string) (uint16, uint16, error) {
	splitRange := strings.Split(portRange, "-")
	if len(splitRange) > 2 {
		return 0, 0, errors.New("invalid port format - port ranges are formatted as startPort-stopPort")
	}

	if splitRange[0] == "" {
		return 0, 0, errors.New("port numbers cannot be negative")
	}

	startPort, err := parseAndValidatePort(splitRange[0])
	if err != nil {
		return 0, 0, err
	}

	var rangeLen uint16 = 1
	if len(splitRange) == 2 {
		if splitRange[1] == "" {
			return 0, 0, errors.New("must provide ending number for port range")
		}
		endPort, err := parseAndValidatePort(splitRange[1])
		if err != nil {
			return 0, 0, err
		}
		if endPort <= startPort {
			return 0, 0, fmt.Errorf("the end port of a range must be higher than the start port - %d is not higher than %d", endPort, startPort)
		}
		// Our range is the total number of ports
		// involved, so we need to add 1 (8080:8081 is
		// 2 ports, for example, not 1)
		rangeLen = endPort - startPort + 1
	}

	return startPort, rangeLen, nil
}

// Turn a single string into a valid U16 port.
func parseAndValidatePort(port string) (uint16, error) {
	num, err := strconv.Atoi(port)
	if err != nil {
		return 0, fmt.Errorf("invalid port number: %w", err)
	}
	if num < 1 || num > 65535 {
		return 0, fmt.Errorf("port numbers must be between 1 and 65535 (inclusive), got %d", num)
	}
	return uint16(num), nil
}

func CreateExitCommandArgs(storageConfig storageTypes.StoreOptions, config *config.Config, syslog, rm, rmi, exec bool) ([]string, error) {
	// We need a cleanup process for containers in the current model.
	// But we can't assume that the caller is Podman - it could be another
	// user of the API.
	// As such, provide a way to specify a path to Podman, so we can
	// still invoke a cleanup process.

	podmanPath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	command := []string{podmanPath,
		"--root", storageConfig.GraphRoot,
		"--runroot", storageConfig.RunRoot,
		"--log-level", logrus.GetLevel().String(),
		"--cgroup-manager", config.Engine.CgroupManager,
		"--tmpdir", config.Engine.TmpDir,
		"--network-config-dir", config.Network.NetworkConfigDir,
		"--network-backend", config.Network.NetworkBackend,
		"--volumepath", config.Engine.VolumePath,
		"--db-backend", config.Engine.DBBackend,
		fmt.Sprintf("--transient-store=%t", storageConfig.TransientStore),
	}
	for _, dir := range config.Engine.HooksDir.Get() {
		command = append(command, []string{"--hooks-dir", dir}...)
	}
	if storageConfig.ImageStore != "" {
		command = append(command, []string{"--imagestore", storageConfig.ImageStore}...)
	}
	if config.Engine.OCIRuntime != "" {
		command = append(command, []string{"--runtime", config.Engine.OCIRuntime}...)
	}
	if storageConfig.GraphDriverName != "" {
		command = append(command, []string{"--storage-driver", storageConfig.GraphDriverName}...)
	}
	for _, opt := range storageConfig.GraphDriverOptions {
		command = append(command, []string{"--storage-opt", opt}...)
	}
	if config.Engine.EventsLogger != "" {
		command = append(command, []string{"--events-backend", config.Engine.EventsLogger}...)
	}

	if syslog {
		command = append(command, "--syslog")
	}

	// Make sure that loaded containers.conf modules are passed down to the
	// callback as well.
	for _, module := range config.LoadedModules() {
		command = append(command, "--module", module)
	}

	// --stopped-only is used to ensure we only cleanup stopped containers and do not race
	// against other processes that did a cleanup() + init() again before we had the chance to run
	command = append(command, []string{"container", "cleanup", "--stopped-only"}...)

	if rm {
		command = append(command, "--rm")
	}

	if rmi {
		command = append(command, "--rmi")
	}

	// This has to be absolutely last, to ensure that the exec session ID
	// will be added after it by Libpod.
	if exec {
		command = append(command, "--exec")
	}

	return command, nil
}
