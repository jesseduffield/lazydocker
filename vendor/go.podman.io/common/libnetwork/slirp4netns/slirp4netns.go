//go:build linux

package slirp4netns

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/rootlessport"
	"go.podman.io/common/pkg/servicereaper"
	"go.podman.io/common/pkg/util"
)

type slirpFeatures struct {
	HasDisableHostLoopback bool
	HasMTU                 bool
	HasEnableSandbox       bool
	HasEnableSeccomp       bool
	HasCIDR                bool
	HasOutboundAddr        bool
	HasIPv6                bool
}

type slirp4netnsCmdArg struct {
	Proto     string `json:"proto,omitempty"`
	HostAddr  string `json:"host_addr"`
	HostPort  uint16 `json:"host_port"`
	GuestAddr string `json:"guest_addr"`
	GuestPort uint16 `json:"guest_port"`
}

type slirp4netnsCmd struct {
	Execute string            `json:"execute"`
	Args    slirp4netnsCmdArg `json:"arguments"`
}

type networkOptions struct {
	cidr                string
	disableHostLoopback bool
	enableIPv6          bool
	isSlirpHostForward  bool
	noPivotRoot         bool
	mtu                 int
	outboundAddr        string
	outboundAddr6       string
}

type SetupOptions struct {
	// Config used to get slip4netns path and other default options
	Config *config.Config
	// ContainerID is the ID of the container
	ContainerID string
	// Netns path to the netns
	Netns string
	// Ports the should be forwarded
	Ports []types.PortMapping
	// ExtraOptions for slirp4netns that were set on the cli
	ExtraOptions []string
	// Slirp4netnsExitPipeR pipe used to exit the slirp4netns process.
	// This is must be the reading end, the writer must be kept open until you want the
	// process to exit. For podman, conmon will hold the pipe open.
	// It can be set to nil in which case we do not use the pipe exit and the caller
	// must use the returned pid to kill the process after it is done.
	Slirp4netnsExitPipeR *os.File
	// RootlessPortSyncPipe pipe used to exit the rootlessport process.
	// Same as Slirp4netnsExitPipeR, except this is only used when ports are given.
	RootlessPortExitPipeR *os.File
	// Pdeathsig is the signal which is send to slirp4netns process if the calling thread
	// exits. The caller is responsible for locking the thread with runtime.LockOSThread().
	Pdeathsig syscall.Signal
}

type logrusDebugWriter struct {
	prefix string
}

func (w *logrusDebugWriter) Write(p []byte) (int, error) {
	logrus.Debugf("%s%s", w.prefix, string(p))
	return len(p), nil
}

func checkSlirpFlags(path string) (*slirpFeatures, error) {
	cmd := exec.Command(path, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("slirp4netns %q: %w", out, err)
	}
	return &slirpFeatures{
		HasDisableHostLoopback: strings.Contains(string(out), "--disable-host-loopback"),
		HasMTU:                 strings.Contains(string(out), "--mtu"),
		HasEnableSandbox:       strings.Contains(string(out), "--enable-sandbox"),
		HasEnableSeccomp:       strings.Contains(string(out), "--enable-seccomp"),
		HasCIDR:                strings.Contains(string(out), "--cidr"),
		HasOutboundAddr:        strings.Contains(string(out), "--outbound-addr"),
		HasIPv6:                strings.Contains(string(out), "--enable-ipv6"),
	}, nil
}

func parseNetworkOptions(config *config.Config, extraOptions []string) (*networkOptions, error) {
	options := make([]string, 0, len(config.Engine.NetworkCmdOptions.Get())+len(extraOptions))
	options = append(options, config.Engine.NetworkCmdOptions.Get()...)
	options = append(options, extraOptions...)
	opts := &networkOptions{
		// overwrite defaults
		disableHostLoopback: true,
		mtu:                 defaultMTU,
		noPivotRoot:         config.Engine.NoPivotRoot,
		enableIPv6:          true,
	}
	for _, o := range options {
		option, value, ok := strings.Cut(o, "=")
		if !ok {
			return nil, fmt.Errorf("unknown option for slirp4netns: %q", o)
		}
		switch option {
		case "cidr":
			ipv4, _, err := net.ParseCIDR(value)
			if err != nil || ipv4.To4() == nil {
				return nil, fmt.Errorf("invalid cidr %q", value)
			}
			opts.cidr = value
		case "port_handler":
			switch value {
			case "slirp4netns":
				opts.isSlirpHostForward = true
			case "rootlesskit":
				opts.isSlirpHostForward = false
			default:
				return nil, fmt.Errorf("unknown port_handler for slirp4netns: %q", value)
			}
		case "allow_host_loopback":
			switch value {
			case "true":
				opts.disableHostLoopback = false
			case "false":
				opts.disableHostLoopback = true
			default:
				return nil, fmt.Errorf("invalid value of allow_host_loopback for slirp4netns: %q", value)
			}
		case "enable_ipv6":
			switch value {
			case "true":
				opts.enableIPv6 = true
			case "false":
				opts.enableIPv6 = false
			default:
				return nil, fmt.Errorf("invalid value of enable_ipv6 for slirp4netns: %q", value)
			}
		case "outbound_addr":
			ipv4 := net.ParseIP(value)
			if ipv4 == nil || ipv4.To4() == nil {
				_, err := net.InterfaceByName(value)
				if err != nil {
					return nil, fmt.Errorf("invalid outbound_addr %q", value)
				}
			}
			opts.outboundAddr = value
		case "outbound_addr6":
			ipv6 := net.ParseIP(value)
			if ipv6 == nil || ipv6.To4() != nil {
				_, err := net.InterfaceByName(value)
				if err != nil {
					return nil, fmt.Errorf("invalid outbound_addr6: %q", value)
				}
			}
			opts.outboundAddr6 = value
		case "mtu":
			var err error
			opts.mtu, err = strconv.Atoi(value)
			if opts.mtu < 68 || err != nil {
				return nil, fmt.Errorf("invalid mtu %q", value)
			}
		default:
			return nil, fmt.Errorf("unknown option for slirp4netns: %q", o)
		}
	}
	return opts, nil
}

func createBasicSlirpCmdArgs(options *networkOptions, features *slirpFeatures) ([]string, error) {
	cmdArgs := []string{}
	if options.disableHostLoopback && features.HasDisableHostLoopback {
		cmdArgs = append(cmdArgs, "--disable-host-loopback")
	}
	if options.mtu > -1 && features.HasMTU {
		cmdArgs = append(cmdArgs, "--mtu="+strconv.Itoa(options.mtu))
	}
	if !options.noPivotRoot && features.HasEnableSandbox {
		cmdArgs = append(cmdArgs, "--enable-sandbox")
	}
	if features.HasEnableSeccomp {
		cmdArgs = append(cmdArgs, "--enable-seccomp")
	}

	if options.cidr != "" {
		if !features.HasCIDR {
			return nil, errors.New("cidr not supported")
		}
		cmdArgs = append(cmdArgs, "--cidr="+options.cidr)
	}

	if options.enableIPv6 {
		if !features.HasIPv6 {
			return nil, errors.New("enable_ipv6 not supported")
		}
		cmdArgs = append(cmdArgs, "--enable-ipv6")
	}

	if options.outboundAddr != "" {
		if !features.HasOutboundAddr {
			return nil, errors.New("outbound_addr not supported")
		}
		cmdArgs = append(cmdArgs, "--outbound-addr="+options.outboundAddr)
	}

	if options.outboundAddr6 != "" {
		if !features.HasOutboundAddr || !features.HasIPv6 {
			return nil, errors.New("outbound_addr6 not supported")
		}
		if !options.enableIPv6 {
			return nil, errors.New("enable_ipv6=true is required for outbound_addr6")
		}
		cmdArgs = append(cmdArgs, "--outbound-addr6="+options.outboundAddr6)
	}

	return cmdArgs, nil
}

// Setup can be called in rootful as well as in rootless.
// Spawns the slirp4netns process and setup port forwarding if ports are given.
func Setup(opts *SetupOptions) (*SetupResult, error) {
	path := opts.Config.Engine.NetworkCmdPath
	if path == "" {
		var err error
		path, err = opts.Config.FindHelperBinary(BinaryName, true)
		if err != nil {
			return nil, fmt.Errorf("could not find slirp4netns, the network namespace can't be configured: %w", err)
		}
	}

	syncR, syncW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open pipe: %w", err)
	}
	defer closeQuiet(syncR)
	defer closeQuiet(syncW)

	havePortMapping := len(opts.Ports) > 0
	logPath := filepath.Join(opts.Config.Engine.TmpDir, fmt.Sprintf("slirp4netns-%s.log", opts.ContainerID))

	netOptions, err := parseNetworkOptions(opts.Config, opts.ExtraOptions)
	if err != nil {
		return nil, err
	}
	slirpFeatures, err := checkSlirpFlags(path)
	if err != nil {
		return nil, fmt.Errorf("checking slirp4netns binary %s: %q: %w", path, err, err)
	}
	cmdArgs, err := createBasicSlirpCmdArgs(netOptions, slirpFeatures)
	if err != nil {
		return nil, err
	}

	// the slirp4netns arguments being passed are described as follows:
	// from the slirp4netns documentation: https://github.com/rootless-containers/slirp4netns
	// -c, --configure Brings up the tap interface
	// -e, --exit-fd=FD specify the FD for terminating slirp4netns
	// -r, --ready-fd=FD specify the FD to write to when the initialization steps are finished
	cmdArgs = append(cmdArgs, "-c", "-r", "3")
	if opts.Slirp4netnsExitPipeR != nil {
		cmdArgs = append(cmdArgs, "-e", "4")
	}

	var apiSocket string
	if havePortMapping && netOptions.isSlirpHostForward {
		apiSocket = filepath.Join(opts.Config.Engine.TmpDir, opts.ContainerID+".net")
		cmdArgs = append(cmdArgs, "--api-socket", apiSocket)
	}

	cmdArgs = append(cmdArgs, "--netns-type=path", opts.Netns, "tap0")

	cmd := exec.Command(path, cmdArgs...)
	logrus.Debugf("slirp4netns command: %s", strings.Join(cmd.Args, " "))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: opts.Pdeathsig,
	}

	// workaround for https://github.com/rootless-containers/slirp4netns/pull/153
	if !netOptions.noPivotRoot && slirpFeatures.HasEnableSandbox {
		cmd.SysProcAttr.Cloneflags = syscall.CLONE_NEWNS
		cmd.SysProcAttr.Unshareflags = syscall.CLONE_NEWNS
	}

	// Leak one end of the pipe in slirp4netns, the other will be sent to conmon
	cmd.ExtraFiles = append(cmd.ExtraFiles, syncW)
	if opts.Slirp4netnsExitPipeR != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, opts.Slirp4netnsExitPipeR)
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open slirp4netns log file %s: %w", logPath, err)
	}
	defer logFile.Close()
	// Unlink immediately the file so we won't need to worry about cleaning it up later.
	// It is still accessible through the open fd logFile.
	if err := os.Remove(logPath); err != nil {
		return nil, fmt.Errorf("delete file %s: %w", logPath, err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	var slirpReadyWg, netnsReadyWg *sync.WaitGroup
	if netOptions.enableIPv6 {
		// use two wait groups to make sure we set the sysctl before
		// starting slirp and reset it only after slirp is ready
		slirpReadyWg = &sync.WaitGroup{}
		netnsReadyWg = &sync.WaitGroup{}
		slirpReadyWg.Add(1)
		netnsReadyWg.Add(1)

		go func() {
			err := ns.WithNetNSPath(opts.Netns, func(_ ns.NetNS) error {
				// Duplicate Address Detection slows the ipv6 setup down for 1-2 seconds.
				// Since slirp4netns is run in its own namespace and not directly routed
				// we can skip this to make the ipv6 address immediately available.
				// We change the default to make sure the slirp tap interface gets the
				// correct value assigned so DAD is disabled for it
				// Also make sure to change this value back to the original after slirp4netns
				// is ready in case users rely on this sysctl.
				orgValue, err := os.ReadFile(ipv6ConfDefaultAcceptDadSysctl)
				if err != nil {
					netnsReadyWg.Done()
					// on ipv6 disabled systems the sysctl does not exist
					// so we should not error
					if errors.Is(err, os.ErrNotExist) {
						return nil
					}
					return err
				}
				err = os.WriteFile(ipv6ConfDefaultAcceptDadSysctl, []byte("0"), 0o644)
				netnsReadyWg.Done()
				if err != nil {
					return err
				}

				// wait until slirp4nets is ready before resetting this value
				slirpReadyWg.Wait()
				return os.WriteFile(ipv6ConfDefaultAcceptDadSysctl, orgValue, 0o644)
			})
			if err != nil {
				logrus.Warnf("failed to set net.ipv6.conf.default.accept_dad sysctl: %v", err)
			}
		}()

		// wait until we set the sysctl
		netnsReadyWg.Wait()
	}

	if err := cmd.Start(); err != nil {
		if netOptions.enableIPv6 {
			slirpReadyWg.Done()
		}
		return nil, fmt.Errorf("failed to start slirp4netns process: %w", err)
	}
	defer func() {
		servicereaper.AddPID(cmd.Process.Pid)
		if err := cmd.Process.Release(); err != nil {
			logrus.Errorf("Unable to release command process: %q", err)
		}
	}()

	err = waitForSync(syncR, cmd, logFile, 1*time.Second)
	if netOptions.enableIPv6 {
		slirpReadyWg.Done()
	}
	if err != nil {
		return nil, err
	}

	// Set a default slirp subnet. Parsing a string with the net helper is easier than building the struct myself
	_, slirpSubnet, _ := net.ParseCIDR(defaultSubnet)

	// Set slirp4netnsSubnet addresses now that we are pretty sure the command executed
	if netOptions.cidr != "" {
		ipv4, ipv4network, err := net.ParseCIDR(netOptions.cidr)
		if err != nil || ipv4.To4() == nil {
			return nil, fmt.Errorf("invalid cidr %q", netOptions.cidr)
		}
		slirpSubnet = ipv4network
	}

	if havePortMapping {
		if netOptions.isSlirpHostForward {
			err = setupRootlessPortMappingViaSlirp(opts.Ports, cmd, apiSocket)
		} else {
			err = SetupRootlessPortMappingViaRLK(opts, slirpSubnet, nil)
		}
		if err != nil {
			return nil, err
		}
	}

	return &SetupResult{
		Pid:    cmd.Process.Pid,
		Subnet: slirpSubnet,
		IPv6:   netOptions.enableIPv6,
	}, nil
}

// GetIP returns the slirp ipv4 address based on subnet. If subnet is null use default subnet.
// Reference: https://github.com/rootless-containers/slirp4netns/blob/master/slirp4netns.1.md#description
func GetIP(subnet *net.IPNet) (*net.IP, error) {
	_, slirpSubnet, _ := net.ParseCIDR(defaultSubnet)
	if subnet != nil {
		slirpSubnet = subnet
	}
	expectedIP, err := addToIP(slirpSubnet, uint32(100))
	if err != nil {
		return nil, fmt.Errorf("calculating expected ip for slirp4netns: %w", err)
	}
	return expectedIP, nil
}

// GetGateway returns the slirp gateway ipv4 address based on subnet.
// Reference: https://github.com/rootless-containers/slirp4netns/blob/master/slirp4netns.1.md#description
func GetGateway(subnet *net.IPNet) (*net.IP, error) {
	_, slirpSubnet, _ := net.ParseCIDR(defaultSubnet)
	if subnet != nil {
		slirpSubnet = subnet
	}
	expectedGatewayIP, err := addToIP(slirpSubnet, uint32(2))
	if err != nil {
		return nil, fmt.Errorf("calculating expected gateway ip for slirp4netns: %w", err)
	}
	return expectedGatewayIP, nil
}

// GetDNS returns slirp DNS ipv4 address based on subnet.
// Reference: https://github.com/rootless-containers/slirp4netns/blob/master/slirp4netns.1.md#description
func GetDNS(subnet *net.IPNet) (*net.IP, error) {
	_, slirpSubnet, _ := net.ParseCIDR(defaultSubnet)
	if subnet != nil {
		slirpSubnet = subnet
	}
	expectedDNSIP, err := addToIP(slirpSubnet, uint32(3))
	if err != nil {
		return nil, fmt.Errorf("calculating expected dns ip for slirp4netns: %w", err)
	}
	return expectedDNSIP, nil
}

// Helper function to calculate slirp ip address offsets
// Adapted from: https://github.com/signalsciences/ipv4/blob/master/int.go#L12-L24
func addToIP(subnet *net.IPNet, offset uint32) (*net.IP, error) {
	// I have no idea why I have to do this, but if I don't ip is 0
	ipFixed := subnet.IP.To4()

	ipInteger := uint32(ipFixed[3]) | uint32(ipFixed[2])<<8 | uint32(ipFixed[1])<<16 | uint32(ipFixed[0])<<24
	ipNewRaw := ipInteger + offset
	// Avoid overflows
	if ipNewRaw < ipInteger {
		return nil, fmt.Errorf("integer overflow while calculating ip address offset, %s + %d", ipFixed, offset)
	}
	ipNew := net.IPv4(byte(ipNewRaw>>24), byte(ipNewRaw>>16&0xFF), byte(ipNewRaw>>8)&0xFF, byte(ipNewRaw&0xFF))
	if !subnet.Contains(ipNew) {
		return nil, fmt.Errorf("calculated ip address %s is not within given subnet %s", ipNew.String(), subnet.String())
	}
	return &ipNew, nil
}

func waitForSync(syncR *os.File, cmd *exec.Cmd, logFile io.ReadSeeker, timeout time.Duration) error {
	prog := filepath.Base(cmd.Path)
	if len(cmd.Args) > 0 {
		prog = cmd.Args[0]
	}
	b := make([]byte, 16)
	for {
		if err := syncR.SetDeadline(time.Now().Add(timeout)); err != nil {
			return fmt.Errorf("setting %s pipe timeout: %w", prog, err)
		}
		// FIXME: return err as soon as proc exits, without waiting for timeout
		_, err := syncR.Read(b)
		if err == nil {
			break
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// Check if the process is still running.
			var status syscall.WaitStatus
			pid, err := syscall.Wait4(cmd.Process.Pid, &status, syscall.WNOHANG, nil)
			if err != nil {
				return fmt.Errorf("failed to read %s process status: %w", prog, err)
			}
			if pid != cmd.Process.Pid {
				continue
			}
			if status.Exited() {
				// Seek at the beginning of the file and read all its content
				if _, err := logFile.Seek(0, 0); err != nil {
					logrus.Errorf("Could not seek log file: %q", err)
				}
				logContent, err := io.ReadAll(logFile)
				if err != nil {
					return fmt.Errorf("%s failed: %w", prog, err)
				}
				return fmt.Errorf("%s failed: %q", prog, logContent)
			}
			if status.Signaled() {
				return fmt.Errorf("%s killed by signal", prog)
			}
			continue
		}
		return fmt.Errorf("failed to read from %s sync pipe: %w", prog, err)
	}
	return nil
}

func SetupRootlessPortMappingViaRLK(opts *SetupOptions, slirpSubnet *net.IPNet, netStatus map[string]types.StatusBlock) error {
	syncR, syncW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to open pipe: %w", err)
	}
	defer closeQuiet(syncR)
	defer closeQuiet(syncW)

	logPath := filepath.Join(opts.Config.Engine.TmpDir, fmt.Sprintf("rootlessport-%s.log", opts.ContainerID))
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to open rootlessport log file %s: %w", logPath, err)
	}
	defer logFile.Close()
	// Unlink immediately the file so we won't need to worry about cleaning it up later.
	// It is still accessible through the open fd logFile.
	if err := os.Remove(logPath); err != nil {
		return fmt.Errorf("delete file %s: %w", logPath, err)
	}

	childIP := GetRootlessPortChildIP(slirpSubnet, netStatus)
	cfg := rootlessport.Config{
		Mappings:    opts.Ports,
		NetNSPath:   opts.Netns,
		ExitFD:      3,
		ReadyFD:     4,
		TmpDir:      opts.Config.Engine.TmpDir,
		ChildIP:     childIP,
		ContainerID: opts.ContainerID,
		RootlessCNI: netStatus != nil,
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	cfgR := bytes.NewReader(cfgJSON)
	var stdout bytes.Buffer
	path, err := opts.Config.FindHelperBinary(rootlessport.BinaryName, false)
	if err != nil {
		return err
	}
	cmd := exec.Command(path)
	cmd.Args = []string{rootlessport.BinaryName}

	// Leak one end of the pipe in rootlessport process, the other will be sent to conmon
	cmd.ExtraFiles = append(cmd.ExtraFiles, opts.RootlessPortExitPipeR, syncW)
	cmd.Stdin = cfgR
	// stdout is for human-readable error, stderr is for debug log
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(logFile, &logrusDebugWriter{"rootlessport: "})
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rootlessport process: %w", err)
	}
	defer func() {
		servicereaper.AddPID(cmd.Process.Pid)
		if err := cmd.Process.Release(); err != nil {
			logrus.Errorf("Unable to release rootlessport process: %q", err)
		}
	}()
	if err := waitForSync(syncR, cmd, logFile, 3*time.Second); err != nil {
		stdoutStr := stdout.String()
		if stdoutStr != "" {
			// err contains full debug log and too verbose, so return stdoutStr
			logrus.Debug(err)
			return errors.New("rootlessport " + strings.TrimSuffix(stdoutStr, "\n"))
		}
		return err
	}
	logrus.Debug("rootlessport is ready")
	return nil
}

func setupRootlessPortMappingViaSlirp(ports []types.PortMapping, cmd *exec.Cmd, apiSocket string) (err error) {
	const pidWaitTimeout = 60 * time.Second
	chWait := make(chan error)
	go func() {
		interval := 25 * time.Millisecond
		for i := time.Duration(0); i < pidWaitTimeout; i += interval {
			// Check if the process is still running.
			var status syscall.WaitStatus
			pid, err := syscall.Wait4(cmd.Process.Pid, &status, syscall.WNOHANG, nil)
			if err != nil {
				break
			}
			if pid != cmd.Process.Pid {
				continue
			}
			if status.Exited() || status.Signaled() {
				chWait <- fmt.Errorf("slirp4netns exited with status %d", status.ExitStatus())
			}
			time.Sleep(interval)
		}
	}()
	defer close(chWait)

	// wait that API socket file appears before trying to use it.
	if _, err := util.WaitForFile(apiSocket, chWait, pidWaitTimeout); err != nil {
		return fmt.Errorf("waiting for slirp4nets to create the api socket file %s: %w", apiSocket, err)
	}

	// for each port we want to add we need to open a connection to the slirp4netns control socket
	// and send the add_hostfwd command.
	for _, port := range ports {
		for protocol := range strings.SplitSeq(port.Protocol, ",") {
			hostIP := port.HostIP
			if hostIP == "" {
				hostIP = "0.0.0.0"
			}
			for i := range port.Range {
				if err := openSlirp4netnsPort(apiSocket, protocol, hostIP, port.HostPort+i, port.ContainerPort+i); err != nil {
					return err
				}
			}
		}
	}
	logrus.Debug("slirp4netns port-forwarding setup via add_hostfwd is ready")
	return nil
}

// openSlirp4netnsPort sends the slirp4netns pai quey to the given socket.
func openSlirp4netnsPort(apiSocket, proto, hostip string, hostport, guestport uint16) error {
	conn, err := net.Dial("unix", apiSocket)
	if err != nil {
		return fmt.Errorf("cannot open connection to %s: %w", apiSocket, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logrus.Errorf("Unable to close slirp4netns connection: %q", err)
		}
	}()
	apiCmd := slirp4netnsCmd{
		Execute: "add_hostfwd",
		Args: slirp4netnsCmdArg{
			Proto:     proto,
			HostAddr:  hostip,
			HostPort:  hostport,
			GuestPort: guestport,
		},
	}
	// create the JSON payload and send it.  Mark the end of request shutting down writes
	// to the socket, as requested by slirp4netns.
	data, err := json.Marshal(&apiCmd)
	if err != nil {
		return fmt.Errorf("cannot marshal JSON for slirp4netns: %w", err)
	}
	if _, err := fmt.Fprintf(conn, "%s\n", data); err != nil {
		return fmt.Errorf("cannot write to control socket %s: %w", apiSocket, err)
	}
	//nolint:errcheck // This cast should never fail, if it does we get a interface
	// conversion panic and a stack trace on how we ended up here which is more
	// valuable than returning a human friendly error test as we don't know how it
	// happened.
	if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
		return fmt.Errorf("cannot shutdown the socket %s: %w", apiSocket, err)
	}
	buf := make([]byte, 2048)
	readLength, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("cannot read from control socket %s: %w", apiSocket, err)
	}
	// if there is no 'error' key in the received JSON data, then the operation was
	// successful.
	var y map[string]any
	if err := json.Unmarshal(buf[0:readLength], &y); err != nil {
		return fmt.Errorf("parsing error status from slirp4netns: %w", err)
	}
	if e, found := y["error"]; found {
		return fmt.Errorf("from slirp4netns while setting up port redirection: %v", e)
	}
	return nil
}

func GetRootlessPortChildIP(slirpSubnet *net.IPNet, netStatus map[string]types.StatusBlock) string {
	if slirpSubnet != nil {
		slirp4netnsIP, err := GetIP(slirpSubnet)
		if err != nil {
			return ""
		}
		return slirp4netnsIP.String()
	}

	var ipv6 net.IP
	for _, status := range netStatus {
		for _, netInt := range status.Interfaces {
			for _, netAddress := range netInt.Subnets {
				ipv4 := netAddress.IPNet.IP.To4()
				if ipv4 != nil {
					return ipv4.String()
				}
				ipv6 = netAddress.IPNet.IP
			}
		}
	}
	if ipv6 != nil {
		return ipv6.String()
	}
	return ""
}

// closeQuiet closes a file and logs any error. Should only be used within
// a defer.
func closeQuiet(f *os.File) {
	if err := f.Close(); err != nil {
		logrus.Errorf("Unable to close file %s: %q", f.Name(), err)
	}
}
