package commands

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func getPlatform() *Platform {
	return &Platform{
		os:                   "windows",
		shell:                "cmd",
		shellArg:             "/c",
		escapedQuote:         `\"`,
		fallbackEscapedQuote: "\\'",
	}
}

// Kill kills a process. If the process has Setpgid == true, then we have anticipated that it might spawn its own child processes, so we've given it a process group ID (PGID) equal to its process id (PID) and given its child processes will inherit the PGID, we can kill that group, rather than killing the process itself.
func (c *OSCommand) Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		// somebody got to it before we were able to, poor bastard
		return nil
	}

	pids := Getppids(uint32(cmd.Process.Pid))
	for _, pid := range pids {
		pro, err := os.FindProcess(int(pid))
		if err != nil {
			continue
		}

		pro.Kill()
	}

	return nil
}

// PrepareForChildren sets Setpgid to true on the cmd, so that when we run it as a sideproject, we can kill its group rather than the process itself. This is because some commands, like `docker-compose logs` spawn multiple children processes, and killing the parent process isn't sufficient for killing those child processes. We set the group id here, and then in subprocess.go we check if the group id is set and if so, we kill the whole group rather than just the one process.
func (c *OSCommand) PrepareForChildren(cmd *exec.Cmd) {}

const (
	MAX_PATH           = 260
	TH32CS_SNAPPROCESS = 0x00000002
)

type ProcessInfo struct {
	Name string
	Pid  uint32
	PPid uint32
}

type PROCESSENTRY32 struct {
	DwSize              uint32
	CntUsage            uint32
	Th32ProcessID       uint32
	Th32DefaultHeapID   uintptr
	Th32ModuleID        uint32
	CntThreads          uint32
	Th32ParentProcessID uint32
	PcPriClassBase      int32
	DwFlags             uint32
	SzExeFile           [MAX_PATH]uint16
}

type HANDLE uintptr

var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32First           = modkernel32.NewProc("Process32FirstW")
	procProcess32Next            = modkernel32.NewProc("Process32NextW")
	procCloseHandle              = modkernel32.NewProc("CloseHandle")
)

func Getppids(pid uint32) []uint32 {
	infos, err := GetProcs()
	if err != nil {
		return []uint32{pid}
	}
	var pids []uint32 = make([]uint32, 0, len(infos))
	var index int = 0
	pids = append(pids, pid)

	var length int = len(pids)
	for index < length {
		for _, info := range infos {
			if info.PPid == pids[index] {
				pids = append(pids, info.Pid)
			}
		}
		index += 1
		length = len(pids)
	}
	return pids
}

func GetProcs() (procs []ProcessInfo, err error) {
	snap := createToolhelp32Snapshot(TH32CS_SNAPPROCESS, uint32(0))
	if snap == 0 {
		err = syscall.GetLastError()
		return
	}
	defer closeHandle(snap)
	var pe32 PROCESSENTRY32
	pe32.DwSize = uint32(unsafe.Sizeof(pe32))
	if process32First(snap, &pe32) == false {
		err = syscall.GetLastError()
		return
	}
	procs = append(procs, ProcessInfo{syscall.UTF16ToString(pe32.SzExeFile[:260]), pe32.Th32ProcessID, pe32.Th32ParentProcessID})
	for process32Next(snap, &pe32) {
		procs = append(procs, ProcessInfo{syscall.UTF16ToString(pe32.SzExeFile[:260]), pe32.Th32ProcessID, pe32.Th32ParentProcessID})
	}
	return
}

func createToolhelp32Snapshot(flags, processId uint32) HANDLE {
	ret, _, _ := procCreateToolhelp32Snapshot.Call(uintptr(flags), uintptr(processId))
	if ret <= 0 {
		return HANDLE(0)
	}
	return HANDLE(ret)
}

func process32First(snapshot HANDLE, pe *PROCESSENTRY32) bool {
	ret, _, _ := procProcess32First.Call(uintptr(snapshot), uintptr(unsafe.Pointer(pe)))
	return ret != 0
}

func process32Next(snapshot HANDLE, pe *PROCESSENTRY32) bool {
	ret, _, _ := procProcess32Next.Call(uintptr(snapshot), uintptr(unsafe.Pointer(pe)))
	return ret != 0
}

func closeHandle(object HANDLE) bool {
	ret, _, _ := procCloseHandle.Call(uintptr(object))
	return ret != 0
}
