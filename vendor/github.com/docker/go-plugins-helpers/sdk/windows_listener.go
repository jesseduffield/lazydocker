//go:build windows

package sdk

import (
	"net"
	"os"
	"syscall"
	"unsafe"

	"github.com/Microsoft/go-winio"
)

// Named pipes use Windows Security Descriptor Definition Language to define ACL. Following are
// some useful definitions.
const (
	// AllowEveryone grants full access permissions for everyone.
	AllowEveryone = "S:(ML;;NW;;;LW)D:(A;;0x12019f;;;WD)"

	// AllowServiceSystemAdmin grants full access permissions for Service, System, Administrator group and account.
	AllowServiceSystemAdmin = "D:(A;ID;FA;;;SY)(A;ID;FA;;;BA)(A;ID;FA;;;LA)(A;ID;FA;;;LS)"
)

func newWindowsListener(address, pluginName, daemonRoot string, pipeConfig *WindowsPipeConfig) (net.Listener, string, error) {
	listener, err := winio.ListenPipe(address, &winio.PipeConfig{
		SecurityDescriptor: pipeConfig.SecurityDescriptor,
		InputBufferSize:    pipeConfig.InBufferSize,
		OutputBufferSize:   pipeConfig.OutBufferSize,
	})
	if err != nil {
		return nil, "", err
	}

	addr := listener.Addr().String()

	specDir, err := createPluginSpecDirWindows(pluginName, addr, daemonRoot)
	if err != nil {
		return nil, "", err
	}

	spec, err := writeSpecFile(pluginName, addr, specDir, protoNamedPipe)
	if err != nil {
		return nil, "", err
	}
	return listener, spec, nil
}

func windowsCreateDirectoryWithACL(name string) error {
	sa := syscall.SecurityAttributes{Length: 0}
	const sddl = "D:P(A;OICI;GA;;;BA)(A;OICI;GA;;;SY)"
	sd, err := winio.SddlToSecurityDescriptor(sddl)
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: name, Err: err}
	}
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.InheritHandle = 1
	sa.SecurityDescriptor = uintptr(unsafe.Pointer(&sd[0]))

	namep, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: name, Err: err}
	}

	e := syscall.CreateDirectory(namep, &sa)
	if e != nil {
		return &os.PathError{Op: "mkdir", Path: name, Err: e}
	}
	return nil
}
