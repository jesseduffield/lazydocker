//go:build freebsd && cgo

package system

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// #include <unistd.h>
// #include <sys/vmmeter.h>
// #include <sys/sysctl.h>
// #include <vm/vm_param.h>
import "C"

func getMemInfo() (int64, int64, error) {
	data, err := unix.SysctlRaw("vm.vmtotal")
	if err != nil {
		return -1, -1, fmt.Errorf("can't get kernel info: %w", err)
	}
	if len(data) != C.sizeof_struct_vmtotal {
		return -1, -1, fmt.Errorf("unexpected vmtotal size %d", len(data))
	}

	total := (*C.struct_vmtotal)(unsafe.Pointer(&data[0]))

	pagesize := int64(C.sysconf(C._SC_PAGESIZE))
	npages := int64(C.sysconf(C._SC_PHYS_PAGES))
	return pagesize * npages, pagesize * int64(total.t_free), nil
}

func getSwapInfo() (int64, int64, error) {
	var (
		total int64 = 0
		used  int64 = 0
	)
	swapCount, err := unix.SysctlUint32("vm.nswapdev")
	if err != nil {
		return -1, -1, fmt.Errorf("reading vm.nswapdev: %w", err)
	}
	for i := 0; i < int(swapCount); i++ {
		data, err := unix.SysctlRaw("vm.swap_info", i)
		if err != nil {
			return -1, -1, fmt.Errorf("reading vm.swap_info.%d: %w", i, err)
		}
		if len(data) != C.sizeof_struct_xswdev {
			return -1, -1, fmt.Errorf("unexpected swap_info size %d", len(data))
		}
		xsw := (*C.struct_xswdev)(unsafe.Pointer(&data[0]))
		total += int64(xsw.xsw_nblks)
		used += int64(xsw.xsw_used)
	}
	pagesize := int64(C.sysconf(C._SC_PAGESIZE))
	return pagesize * total, pagesize * (total - used), nil
}

// ReadMemInfo retrieves memory statistics of the host system and returns a
//
//	MemInfo type.
func ReadMemInfo() (*MemInfo, error) {
	MemTotal, MemFree, err := getMemInfo()
	if err != nil {
		return nil, fmt.Errorf("getting memory totals %w", err)
	}
	SwapTotal, SwapFree, err := getSwapInfo()
	if err != nil {
		return nil, fmt.Errorf("getting swap totals %w", err)
	}

	if MemTotal < 0 || MemFree < 0 || SwapTotal < 0 || SwapFree < 0 {
		return nil, errors.New("getting system memory info")
	}

	meminfo := &MemInfo{}
	// Total memory is total physical memory less than memory locked by kernel
	meminfo.MemTotal = MemTotal
	meminfo.MemFree = MemFree
	meminfo.SwapTotal = SwapTotal
	meminfo.SwapFree = SwapFree

	return meminfo, nil
}
