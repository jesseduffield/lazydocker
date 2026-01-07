// SPDX-License-Identifier: BSD-3-Clause
//go:build windows

package cpu

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows"

	"github.com/shirou/gopsutil/v4/internal/common"
)

var (
	procGetNativeSystemInfo              = common.Modkernel32.NewProc("GetNativeSystemInfo")
	procGetLogicalProcessorInformationEx = common.Modkernel32.NewProc("GetLogicalProcessorInformationEx")
	procGetSystemFirmwareTable           = common.Modkernel32.NewProc("GetSystemFirmwareTable")
	procCallNtPowerInformation           = common.ModPowrProf.NewProc("CallNtPowerInformation")
)

type win32_Processor struct { //nolint:revive //FIXME
	Family                    uint16
	Manufacturer              string
	Name                      string
	NumberOfLogicalProcessors uint32
	NumberOfCores             uint32
	ProcessorID               *string
	Stepping                  *string
	MaxClockSpeed             uint32
}

// SYSTEM_PROCESSOR_PERFORMANCE_INFORMATION
// defined in windows api doc with the following
// https://docs.microsoft.com/en-us/windows/desktop/api/winternl/nf-winternl-ntquerysysteminformation#system_processor_performance_information
// additional fields documented here
// https://www.geoffchappell.com/studies/windows/km/ntoskrnl/api/ex/sysinfo/processor_performance.htm
type win32_SystemProcessorPerformanceInformation struct { //nolint:revive //FIXME
	IdleTime       int64  // idle time in 100ns (this is not a filetime).
	KernelTime     int64  // kernel time in 100ns.  kernel time includes idle time. (this is not a filetime).
	UserTime       int64  // usertime in 100ns (this is not a filetime).
	DpcTime        int64  // dpc time in 100ns (this is not a filetime).
	InterruptTime  int64  // interrupt time in 100ns
	InterruptCount uint64 // ULONG needs to be uint64
}

// https://learn.microsoft.com/en-us/windows/win32/power/processor-power-information-str
type processorPowerInformation struct {
	number           uint32 // http://download.microsoft.com/download/a/d/f/adf1347d-08dc-41a4-9084-623b1194d4b2/MoreThan64proc.docx
	maxMhz           uint32
	currentMhz       uint32
	mhzLimit         uint32
	maxIdleState     uint32
	currentIdleState uint32
}

const (
	ClocksPerSec = 10000000.0

	// systemProcessorPerformanceInformationClass information class to query with NTQuerySystemInformation
	// https://processhacker.sourceforge.io/doc/ntexapi_8h.html#ad5d815b48e8f4da1ef2eb7a2f18a54e0
	win32_SystemProcessorPerformanceInformationClass = 8 //nolint:revive //FIXME

	// size of systemProcessorPerformanceInfoSize in memory
	win32_SystemProcessorPerformanceInfoSize = uint32(unsafe.Sizeof(win32_SystemProcessorPerformanceInformation{})) //nolint:revive //FIXME

	firmwareTableProviderSignatureRSMB = 0x52534d42 // "RSMB"  https://gitlab.winehq.org/dreamer/wine/-/blame/wine-7.0-rc6/dlls/ntdll/unix/system.c#L230
	smBiosHeaderSize                   = 8          // SMBIOS header size
	smbiosEndOfTable                   = 127        // Minimum length for processor structure
	smbiosTypeProcessor                = 4          // SMBIOS Type 4: Processor Information
	smbiosProcessorMinLength           = 0x18       // Minimum length for processor structure
)

type relationship uint32

// https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-getlogicalprocessorinformationex
const (
	relationProcessorCore    = relationship(0)
	relationProcessorPackage = relationship(3)
)

const (
	kAffinitySize = unsafe.Sizeof(int(0))
	// https://learn.microsoft.com/en-us/windows-hardware/drivers/kernel/interrupt-affinity-and-priority
	maxLogicalProcessorsPerGroup = uint32(unsafe.Sizeof(kAffinitySize * 8))
	// https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/wdm/ne-wdm-power_information_level
	processorInformation = 11
)

// Times returns times stat per cpu and combined for all CPUs
func Times(percpu bool) ([]TimesStat, error) {
	return TimesWithContext(context.Background(), percpu)
}

func TimesWithContext(_ context.Context, percpu bool) ([]TimesStat, error) {
	if percpu {
		return perCPUTimes()
	}

	var ret []TimesStat
	var lpIdleTime common.FILETIME
	var lpKernelTime common.FILETIME
	var lpUserTime common.FILETIME
	// GetSystemTimes returns 0 for error, in which case we check err,
	// see https://pkg.go.dev/golang.org/x/sys/windows#LazyProc.Call
	r, _, err := common.ProcGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&lpIdleTime)),
		uintptr(unsafe.Pointer(&lpKernelTime)),
		uintptr(unsafe.Pointer(&lpUserTime)))
	if r == 0 {
		return nil, err
	}

	LOT := float64(0.0000001)
	HIT := (LOT * 4294967296.0)
	idle := ((HIT * float64(lpIdleTime.DwHighDateTime)) + (LOT * float64(lpIdleTime.DwLowDateTime)))
	user := ((HIT * float64(lpUserTime.DwHighDateTime)) + (LOT * float64(lpUserTime.DwLowDateTime)))
	kernel := ((HIT * float64(lpKernelTime.DwHighDateTime)) + (LOT * float64(lpKernelTime.DwLowDateTime)))
	system := (kernel - idle)

	ret = append(ret, TimesStat{
		CPU:    "cpu-total",
		Idle:   float64(idle),
		User:   float64(user),
		System: float64(system),
	})
	return ret, nil
}

func Info() ([]InfoStat, error) {
	return InfoWithContext(context.Background())
}

// this function iterates over each set bit in the package affinity mask, each bit represent a logical processor in a group (assuming you are iteriang over a package mask)
// the function is used also to compute the global logical processor number
// https://learn.microsoft.com/en-us/windows/win32/procthread/processor-groups
// see https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-group_affinity
// and https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-processor_relationship
// and https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-system_logical_processor_information_ex
func forEachSetBit64(mask uint64, fn func(bit int)) {
	m := mask
	for m != 0 {
		b := bits.TrailingZeros64(m)
		fn(b)
		m &= m - 1
	}
}

func getProcessorPowerInformation(ctx context.Context) ([]processorPowerInformation, error) {
	numLP, countErr := CountsWithContext(ctx, true)
	if countErr != nil {
		return nil, fmt.Errorf("failed to get logical processor count: %w", countErr)
	}
	if numLP <= 0 {
		return nil, fmt.Errorf("invalid logical processor count: %d", numLP)
	}

	ppiSize := uintptr(numLP) * unsafe.Sizeof(processorPowerInformation{})
	buf := make([]byte, ppiSize)
	ppi, _, err := procCallNtPowerInformation.Call(
		uintptr(processorInformation),
		0,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(ppiSize),
	)
	if ppi != 0 {
		return nil, fmt.Errorf("CallNtPowerInformation failed with code %d: %w", ppi, err)
	}
	ppis := unsafe.Slice((*processorPowerInformation)(unsafe.Pointer(&buf[0])), numLP)
	return ppis, nil
}

func InfoWithContext(ctx context.Context) ([]InfoStat, error) {
	var ret []InfoStat
	var dst []win32_Processor
	q := wmi.CreateQuery(&dst, "")
	if err := common.WMIQueryWithContext(ctx, q, &dst); err != nil {
		return ret, err
	}

	var procID string
	for i, l := range dst {
		procID = ""
		if l.ProcessorID != nil {
			procID = *l.ProcessorID
		}

		cpu := InfoStat{
			CPU:        int32(i),
			Family:     strconv.FormatUint(uint64(l.Family), 10),
			VendorID:   l.Manufacturer,
			ModelName:  l.Name,
			Cores:      int32(l.NumberOfLogicalProcessors), // TO BE REMOVED, set by getSystemLogicalProcessorInformationEx
			PhysicalID: procID,
			Mhz:        float64(l.MaxClockSpeed),
			Flags:      []string{},
		}
		ret = append(ret, cpu)
	}

	processorPackages, err := getSystemLogicalProcessorInformationEx(relationProcessorPackage)
	if err != nil {
		// return an error whem wmi will be removed
		// return ret, fmt.Errorf("failed to get processor package information: %w", err)
		return ret, nil
	}

	if len(processorPackages) != len(ret) {
		// this should never happen, but it's kept for safety until wmi is removed
		return ret, nil
	}

	ppis, powerInformationErr := getProcessorPowerInformation(ctx)
	if powerInformationErr != nil {
		// return an error whem wmi will be removed
		// return ret, fmt.Errorf("failed to get processor power information: %w", err)
		return ret, nil
	}

	family, processorId, smBIOSErr := getSMBIOSProcessorInfo()
	if smBIOSErr != nil {
		// return an error whem wmi will be removed
		// return ret, smBIOSErr
		return ret, nil
	}

	for i, pkg := range processorPackages {
		logicalCount := 0
		maxMhz := 0
		for _, ga := range pkg.processor.groupMask {
			g := int(ga.group)
			forEachSetBit64(uint64(ga.mask), func(bit int) {
				// the global logical processor label
				globalLpl := g*int(maxLogicalProcessorsPerGroup) + bit
				if globalLpl >= 0 && globalLpl < len(ppis) {
					logicalCount++
					m := int(ppis[globalLpl].maxMhz)
					if m > maxMhz {
						maxMhz = m
					}
				}
			})
		}
		ret[i].Mhz = float64(maxMhz)
		ret[i].Cores = int32(logicalCount)
		ret[i].Family = strconv.FormatUint(uint64(family), 10)
		ret[i].PhysicalID = processorId
	}

	return ret, nil
}

// perCPUTimes returns times stat per cpu, per core and overall for all CPUs
func perCPUTimes() ([]TimesStat, error) {
	var ret []TimesStat
	stats, err := perfInfo()
	if err != nil {
		return nil, err
	}
	for core, v := range stats {
		c := TimesStat{
			CPU:    fmt.Sprintf("cpu%d", core),
			User:   float64(v.UserTime) / ClocksPerSec,
			System: float64(v.KernelTime-v.IdleTime) / ClocksPerSec,
			Idle:   float64(v.IdleTime) / ClocksPerSec,
			Irq:    float64(v.InterruptTime) / ClocksPerSec,
		}
		ret = append(ret, c)
	}
	return ret, nil
}

// makes call to Windows API function to retrieve performance information for each core
func perfInfo() ([]win32_SystemProcessorPerformanceInformation, error) {
	// Make maxResults large for safety.
	// We can't invoke the api call with a results array that's too small.
	// If we have more than 2056 cores on a single host, then it's probably the future.
	maxBuffer := 2056
	// buffer for results from the windows proc
	resultBuffer := make([]win32_SystemProcessorPerformanceInformation, maxBuffer)
	// size of the buffer in memory
	bufferSize := uintptr(win32_SystemProcessorPerformanceInfoSize) * uintptr(maxBuffer)
	// size of the returned response
	var retSize uint32

	// Invoke windows api proc.
	// The returned err from the windows dll proc will always be non-nil even when successful.
	// See https://godoc.org/golang.org/x/sys/windows#LazyProc.Call for more information
	retCode, _, err := common.ProcNtQuerySystemInformation.Call(
		win32_SystemProcessorPerformanceInformationClass, // System Information Class -> SystemProcessorPerformanceInformation
		uintptr(unsafe.Pointer(&resultBuffer[0])),        // pointer to first element in result buffer
		bufferSize,                        // size of the buffer in memory
		uintptr(unsafe.Pointer(&retSize)), // pointer to the size of the returned results the windows proc will set this
	)

	// check return code for errors
	if retCode != 0 {
		return nil, fmt.Errorf("call to NtQuerySystemInformation returned %d. err: %s", retCode, err.Error())
	}

	// calculate the number of returned elements based on the returned size
	numReturnedElements := retSize / win32_SystemProcessorPerformanceInfoSize

	// trim results to the number of returned elements
	resultBuffer = resultBuffer[:numReturnedElements]

	return resultBuffer, nil
}

// SystemInfo is an equivalent representation of SYSTEM_INFO in the Windows API.
// https://msdn.microsoft.com/en-us/library/ms724958%28VS.85%29.aspx?f=255&MSPPError=-2147217396
// https://github.com/elastic/go-windows/blob/bb1581babc04d5cb29a2bfa7a9ac6781c730c8dd/kernel32.go#L43
type systemInfo struct {
	wProcessorArchitecture      uint16
	wReserved                   uint16
	dwPageSize                  uint32
	lpMinimumApplicationAddress uintptr
	lpMaximumApplicationAddress uintptr
	dwActiveProcessorMask       uintptr
	dwNumberOfProcessors        uint32
	dwProcessorType             uint32
	dwAllocationGranularity     uint32
	wProcessorLevel             uint16
	wProcessorRevision          uint16
}

type groupAffinity struct {
	mask     uintptr // https://learn.microsoft.com/en-us/windows-hardware/drivers/kernel/interrupt-affinity-and-priority#about-kaffinity
	group    uint16
	reserved [3]uint16
}

// https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-processor_relationship
type processorRelationship struct {
	flags          byte
	efficientClass byte
	reserved       [20]byte
	groupCount     uint16
	groupMask      [1]groupAffinity
}

// https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-system_logical_processor_information_ex
type systemLogicalProcessorInformationEx struct {
	relationship uint32
	size         uint32
	processor    processorRelationship
}

// getSMBIOSProcessorInfo reads the SMBIOS Type 4 (Processor Information) structure and returns the Processor Family and ProcessorId fields.
// If not found, returns 0 and an empty string.
func getSMBIOSProcessorInfo() (family uint8, processorId string, err error) {
	// https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-getsystemfirmwaretable
	size, _, err := procGetSystemFirmwareTable.Call(
		uintptr(firmwareTableProviderSignatureRSMB),
		0,
		0,
		0,
	)
	if size == 0 {
		return 0, "", fmt.Errorf("failed to get SMBIOS table size: %w", err)
	}
	buf := make([]byte, size)
	ret, _, err := procGetSystemFirmwareTable.Call(
		uintptr(firmwareTableProviderSignatureRSMB),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(size),
	)
	if ret == 0 {
		return 0, "", fmt.Errorf("failed to read SMBIOS table: %w", err)
	}
	// https://wiki.osdev.org/System_Management_BIOS
	i := smBiosHeaderSize // skip SMBIOS header (first 8 bytes)
	maxIterations := len(buf) * 2
	iterations := 0
	for i < len(buf) && iterations < maxIterations {
		iterations++
		if i+4 > len(buf) {
			break
		}
		typ := buf[i]
		length := buf[i+1]
		if typ == smbiosEndOfTable {
			break
		}
		if typ == smbiosTypeProcessor && length >= smbiosProcessorMinLength && i+int(length) <= len(buf) {
			// Ensure we have enough bytes for procIdBytes
			if i+16 > len(buf) {
				break
			}
			// Get the processor family from byte at offset 6
			family = buf[i+6]
			// Extract processor ID bytes (8 bytes total) from offsets 8-15
			procIdBytes := buf[i+8 : i+16]
			// Convert first 4 bytes to 32-bit EAX register value (little endian)
			eax := uint32(procIdBytes[0]) | uint32(procIdBytes[1])<<8 | uint32(procIdBytes[2])<<16 | uint32(procIdBytes[3])<<24
			// Convert last 4 bytes to 32-bit EDX register value (little endian)
			edx := uint32(procIdBytes[4]) | uint32(procIdBytes[5])<<8 | uint32(procIdBytes[6])<<16 | uint32(procIdBytes[7])<<24
			// Format processor ID as 16 character hex string (EDX+EAX)
			procId := fmt.Sprintf("%08X%08X", edx, eax)
			return family, procId, nil
		}
		// skip to next structure
		j := i + int(length)
		innerIterations := 0
		maxInner := len(buf) // failsafe for inner loop
		for j+1 < len(buf) && innerIterations < maxInner {
			innerIterations++
			if buf[j] == 0 && buf[j+1] == 0 {
				j += 2
				break
			}
			j++
		}
		if innerIterations >= maxInner {
			break // malformed buffer, avoid infinite loop
		}
		i = j
	}
	return 0, "", fmt.Errorf("SMBIOS processor information not found: %w", syscall.ERROR_NOT_FOUND)
}

func getSystemLogicalProcessorInformationEx(relationship relationship) ([]systemLogicalProcessorInformationEx, error) {
	var length uint32
	// First call to determine the required buffer size
	_, _, err := procGetLogicalProcessorInformationEx.Call(uintptr(relationship), 0, uintptr(unsafe.Pointer(&length)))
	if err != nil && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, fmt.Errorf("failed to get buffer size: %w", err)
	}

	// Allocate the buffer
	buffer := make([]byte, length)

	// Second call to retrieve the processor information
	_, _, err = procGetLogicalProcessorInformationEx.Call(uintptr(relationship), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
	if err != nil && !errors.Is(err, windows.NTE_OP_OK) {
		return nil, fmt.Errorf("failed to get logical processor information: %w", err)
	}

	// Convert the byte slice into a slice of systemLogicalProcessorInformationEx structs
	offset := uintptr(0)
	var infos []systemLogicalProcessorInformationEx
	for offset < uintptr(length) {
		info := (*systemLogicalProcessorInformationEx)(unsafe.Pointer(uintptr(unsafe.Pointer(&buffer[0])) + offset))
		infos = append(infos, *info)
		offset += uintptr(info.size)
	}

	return infos, nil
}

func getPhysicalCoreCount() (int, error) {
	infos, err := getSystemLogicalProcessorInformationEx(relationProcessorCore)
	return len(infos), err
}

func CountsWithContext(_ context.Context, logical bool) (int, error) {
	if logical {
		// Get logical processor count https://github.com/giampaolo/psutil/blob/d01a9eaa35a8aadf6c519839e987a49d8be2d891/psutil/_psutil_windows.c#L97
		ret := windows.GetActiveProcessorCount(windows.ALL_PROCESSOR_GROUPS)
		if ret != 0 {
			return int(ret), nil
		}

		var sInfo systemInfo
		_, _, err := procGetNativeSystemInfo.Call(uintptr(unsafe.Pointer(&sInfo)))
		if sInfo.dwNumberOfProcessors == 0 {
			return 0, err
		}
		return int(sInfo.dwNumberOfProcessors), nil
	}

	// Get physical core count https://github.com/giampaolo/psutil/blob/d01a9eaa35a8aadf6c519839e987a49d8be2d891/psutil/_psutil_windows.c#L499
	return getPhysicalCoreCount()
}
