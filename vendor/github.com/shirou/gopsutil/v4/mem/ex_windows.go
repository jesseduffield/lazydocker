// SPDX-License-Identifier: BSD-3-Clause
//go:build windows

package mem

import (
	"unsafe"
)

// ExVirtualMemory represents Windows specific information
// https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/ns-sysinfoapi-memorystatusex
// https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-performance_information
type ExVirtualMemory struct {
	CommitLimit   uint64 `json:"commitLimit"`
	CommitTotal   uint64 `json:"commitTotal"`
	VirtualTotal  uint64 `json:"virtualTotal"`
	VirtualAvail  uint64 `json:"virtualAvail"`
	PhysTotal     uint64 `json:"physTotal"`
	PhysAvail     uint64 `json:"physAvail"`
	PageFileTotal uint64 `json:"pageFileTotal"`
	PageFileAvail uint64 `json:"pageFileAvail"`
}

type ExWindows struct{}

func NewExWindows() *ExWindows {
	return &ExWindows{}
}

func (*ExWindows) VirtualMemory() (*ExVirtualMemory, error) {
	var memInfo memoryStatusEx
	memInfo.cbSize = uint32(unsafe.Sizeof(memInfo))
	// If mem == 0 since this is an error according to GlobalMemoryStatusEx documentation
	// In that case, use err which is constructed from GetLastError(),
	// see https://pkg.go.dev/golang.org/x/sys/windows#LazyProc.Call
	mem, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memInfo)))
	if mem == 0 {
		return nil, err
	}

	var perfInfo performanceInformation
	perfInfo.cb = uint32(unsafe.Sizeof(perfInfo))
	// Analogous to above: perf == 0 is an error according to the GetPerformanceInfo documentation,
	// use err in that case
	perf, _, err := procGetPerformanceInfo.Call(uintptr(unsafe.Pointer(&perfInfo)), uintptr(perfInfo.cb))
	if perf == 0 {
		return nil, err
	}

	ret := &ExVirtualMemory{
		CommitLimit:   perfInfo.commitLimit * perfInfo.pageSize,
		CommitTotal:   perfInfo.commitTotal * perfInfo.pageSize,
		VirtualTotal:  memInfo.ullTotalVirtual,
		VirtualAvail:  memInfo.ullAvailVirtual,
		PhysTotal:     memInfo.ullTotalPhys,
		PhysAvail:     memInfo.ullAvailPhys,
		PageFileTotal: memInfo.ullTotalPageFile,
		PageFileAvail: memInfo.ullAvailPageFile,
	}

	return ret, nil
}
