// Copyright 2018 psgo authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proc

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.podman.io/storage/pkg/idtools"
)

// Status is a direct translation of a `/proc/[pid]/status`, which provides much
// of the information in /proc/[pid]/stat and /proc/[pid]/statm in a format
// that's easier for humans to parse.
type Status struct {
	// Name: Command run by this process.
	Name string
	// Umask: Process umask, expressed in octal with a leading  zero;  see
	// umask(2). (Since Linux 4.7.)
	Umask string
	// State:  Current  state  of the process.  One of "R (running)", "S
	// (sleeping)", "D (disk sleep)", "T (stopped)", "T (tracing stop)", "Z
	// (zombie)", or "X (dead)".
	State string
	// Tgid: Thread group ID (i.e., Process ID).
	Tgid string
	// Ngid: NUMA group ID (0 if none; since Linux 3.13).
	Ngid string
	// Pid: Thread ID (see gettid(2)).
	Pid string
	// PPid: PID of parent process.
	PPid string
	// TracerPid: PID of process tracing this process (0 if not being traced).
	TracerPid string
	// Uids: Real, effective, saved set, and filesystem.
	Uids []string
	// Gids: Real, effective, saved set, and filesystem.
	Gids []string
	// FDSize: Number of file descriptor slots currently allocated.
	FdSize string
	// Groups: Supplementary group list.
	Groups []string
	// NStgid : Thread group ID (i.e., PID) in each of the PID namespaces
	// of which [pid] is  a member.   The  leftmost  entry shows the value
	// with respect to the PID namespace of the reading process, followed
	// by the value in successively nested inner namespaces.  (Since Linux
	// 4.1.)
	NStgid string
	// NSpid:  Thread ID in each of the PID namespaces of which [pid] is a
	// member.  The fields are ordered as for NStgid.  (Since Linux 4.1.)
	NSpid []string
	// NSpgid: Process group ID in each of the PID namespaces of which
	// [pid] is a member.  The fields are ordered as for NStgid.  (Since
	// Linux 4.1.)
	NSpgid string
	// NSsid:  descendant  namespace session ID hierarchy Session ID in
	// each of the PID names- paces of which [pid] is a member.  The fields
	// are ordered as for NStgid.  (Since  Linux 4.1.)
	NSsid string
	// VMPeak: Peak virtual memory size.
	VMPeak string
	// VMSize: Virtual memory size.
	VMSize string
	// VMLck: Locked memory size (see mlock(3)).
	VMLCK string
	// VMPin:  Pinned  memory  size  (since  Linux  3.2).  These are pages
	// that can't be moved because something needs to directly access
	// physical memory.
	VMPin string
	// VMHWM: Peak resident set size ("high water mark").
	VMHWM string
	// VMRSS: Resident set size.  Note that the value here is the sum of
	// RssAnon, RssFile, and RssShmem.
	VMRSS string
	// RssAnon: Size of resident anonymous memory.  (since Linux 4.5).
	RssAnon string
	// RssFile: Size of resident file mappings.  (since Linux 4.5).
	RssFile string
	// RssShmem:  Size  of  resident  shared memory (includes System V
	// shared memory, mappings from tmpfs(5), and shared anonymous
	// mappings).  (since Linux 4.5).
	RssShmem string
	// VMData: Size of data segment.
	VMData string
	// VMStk: Size of stack segment.
	VMStk string
	// VMExe: Size of text segment.
	VMExe string
	// VMLib: Shared library code size.
	VMLib string
	// VMPTE: Page table entries size (since Linux 2.6.10).
	VMPTE string
	// VMPMD: Size of second-level page tables (since Linux 4.0).
	VMPMD string
	// VMSwap: Swapped-out virtual memory size by anonymous private pages;
	// shmem swap usage is not included (since Linux 2.6.34).
	VMSwap string
	// HugetlbPages: Size of hugetlb memory portions.  (since Linux 4.4).
	HugetlbPages string
	// Threads: Number of threads in process containing this thread.
	Threads string
	// SigQ: This field contains two slash-separated numbers that relate to
	// queued signals for the real user ID of this process.  The first of
	// these is the number of currently queued signals  for  this  real
	// user ID, and the second is the resource limit on the number of
	// queued signals for this process (see the  description  of
	// RLIMIT_SIGPENDING  in  getr- limit(2)).
	SigQ string
	// SigPnd:  Number  of signals pending for thread and for (see pthreads(7)).
	SigPnd string
	// ShdPnd:  Number  of signals pending for process as a whole (see
	// signal(7)).
	ShdPnd string
	//  SigBlk: Mask indicating signals being  blocked (see signal(7)).
	SigBlk string
	//  SigIgn: Mask indicating signals being ignored (see signal(7)).
	SigIgn string
	//  SigCgt: Mask indicating signals being  blocked caught (see signal(7)).
	SigCgt string
	// CapInh:  Mask of capabilities enabled in inheritable sets (see
	// capabilities(7)).
	CapInh string
	// CapPrm:  Mask of capabilities enabled in permitted sets (see
	// capabilities(7)).
	CapPrm string
	// CapEff:  Mask of capabilities enabled in effective sets (see
	// capabilities(7)).
	CapEff string
	// CapBnd: Capability Bounding set (since Linux 2.6.26, see
	// capabilities(7)).
	CapBnd string
	// CapAmb: Ambient capability set (since Linux 4.3, see capabilities(7)).
	CapAmb string
	// NoNewPrivs: Value of the no_new_privs bit (since Linux 4.10, see
	// prctl(2)).
	NoNewPrivs string
	// Seccomp: Seccomp mode of the process (since Linux 3.8, see
	// seccomp(2)).  0  means  SEC- COMP_MODE_DISABLED;  1  means
	// SECCOMP_MODE_STRICT;  2 means SECCOMP_MODE_FILTER.  This field is
	// provided only if the kernel was built with the CONFIG_SECCOMP kernel
	// configu- ration option enabled.
	Seccomp string
	// SeccompFilters: Amount of filters attached to the process.
	// (since Linux 5.9)
	SeccompFilters string
	// Cpus_allowed:  Mask  of  CPUs  on  which  this process may run
	// (since Linux 2.6.24, see cpuset(7)).
	CpusAllowed string
	// Cpus_allowed_list: Same as previous, but in "list  format"  (since
	// Linux  2.6.26,  see cpuset(7)).
	CpusAllowedList string
	// Mems_allowed:  Mask  of  memory  nodes allowed to this process
	// (since Linux 2.6.24, see cpuset(7)).
	MemsAllowed string
	// Mems_allowed_list: Same as previous, but in "list  format"  (since
	// Linux  2.6.26,  see cpuset(7)).
	MemsAllowedList string
	// voluntaryCtxtSwitches:  Number of voluntary context switches
	// (since Linux 2.6.23).
	VoluntaryCtxtSwitches string
	// nonvoluntaryCtxtSwitches:  Number of involuntary context switches
	// (since Linux 2.6.23).
	NonvoluntaryCtxtSwitches string
}

// readStatus returns the content of /proc/pid/status as a string slice.
func readStatus(pid string) ([]string, error) {
	path := fmt.Sprintf("/proc/%s/status", pid)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	lines := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, nil
}

// mapField maps a single string-typed ID field given the set of mappings. If
// no mapping exists, the overflow uid/gid is used.
func mapStatusField(field *string, mapping []idtools.IDMap, overflow string) {
	hostId, err := strconv.Atoi(*field)
	if err != nil {
		*field = overflow
		return
	}
	contId, err := idtools.RawToContainer(hostId, mapping)
	if err != nil {
		*field = overflow
		return
	}
	*field = strconv.Itoa(contId)
}

var (
	overflowOnce sync.Once
	overflowUid  = "65534"
	overflowGid  = "65534"
)

func overflowIds() (string, string) {
	overflowOnce.Do(func() {
		if uid, err := os.ReadFile("/proc/sys/kernel/overflowuid"); err == nil {
			overflowUid = strings.TrimSpace(string(uid))
		}
		if gid, err := os.ReadFile("/proc/sys/kernel/overflowgid"); err == nil {
			overflowGid = strings.TrimSpace(string(gid))
		}
	})
	return overflowUid, overflowGid
}

// mapStatus takes a Status struct and remaps all of the relevant fields to
// match the user namespace of the target process.
func mapStatus(pid string, status *Status) (*Status, error) {
	uidMap, err := ReadMappings(fmt.Sprintf("/proc/%s/uid_map", pid))
	if err != nil {
		return nil, err
	}
	gidMap, err := ReadMappings(fmt.Sprintf("/proc/%s/gid_map", pid))
	if err != nil {
		return nil, err
	}
	overflowUid, overflowGid := overflowIds()
	for i := range status.Uids {
		mapStatusField(&status.Uids[i], uidMap, overflowUid)
	}
	for i := range status.Gids {
		mapStatusField(&status.Gids[i], gidMap, overflowGid)
	}
	for i := range status.Groups {
		mapStatusField(&status.Groups[i], gidMap, overflowGid)
	}
	return status, nil
}

// ParseStatus parses the /proc/$pid/status file and returns a *Status.
func ParseStatus(pid string, mapUserNS bool) (*Status, error) {
	lines, err := readStatus(pid)
	if err != nil {
		return nil, err
	}
	status, err := parseStatus(pid, lines)
	if err != nil {
		return nil, err
	}
	if mapUserNS {
		status, err = mapStatus(pid, status)
		if err != nil {
			return nil, err
		}
	}
	return status, nil
}

// parseStatus extracts data from lines and returns a *Status.
func parseStatus(pid string, lines []string) (*Status, error) {
	s := Status{}
	errUnexpectedInput := fmt.Errorf("unexpected input from /proc/%s/status", pid)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "Name:":
			s.Name = fields[1]
		case "Umask:":
			s.Umask = fields[1]
		case "State:":
			s.State = fields[1]
		case "Tgid:":
			s.Tgid = fields[1]
		case "Ngid:":
			s.Ngid = fields[1]
		case "Pid:":
			s.Pid = fields[1]
		case "PPid:":
			s.PPid = fields[1]
		case "TracerPid:":
			s.TracerPid = fields[1]
		case "Uid:":
			if len(fields) != 5 {
				return nil, fmt.Errorf(line+": %w", errUnexpectedInput)
			}
			s.Uids = []string{fields[1], fields[2], fields[3], fields[4]}
		case "Gid:":
			if len(fields) != 5 {
				return nil, fmt.Errorf(line+": %w", errUnexpectedInput)
			}
			s.Gids = []string{fields[1], fields[2], fields[3], fields[4]}
		case "FDSize:":
			s.FdSize = fields[1]
		case "Groups:":
			s.Groups = fields[1:]
		case "NStgid:":
			s.NStgid = fields[1]
		case "NSpid:":
			s.NSpid = fields[1:]
		case "NSpgid:":
			s.NSpgid = fields[1]
		case "NSsid:":
			s.NSsid = fields[1]
		case "VmPeak:":
			s.VMPeak = fields[1]
		case "VmSize:":
			s.VMSize = fields[1]
		case "VmLck:":
			s.VMLCK = fields[1]
		case "VmPin:":
			s.VMPin = fields[1]
		case "VmHWM:":
			s.VMHWM = fields[1]
		case "VmRSS:":
			s.VMRSS = fields[1]
		case "RssAnon:":
			s.RssAnon = fields[1]
		case "RssFile:":
			s.RssFile = fields[1]
		case "RssShmem:":
			s.RssShmem = fields[1]
		case "VmData:":
			s.VMData = fields[1]
		case "VmStk:":
			s.VMStk = fields[1]
		case "VmExe:":
			s.VMExe = fields[1]
		case "VmLib:":
			s.VMLib = fields[1]
		case "VmPTE:":
			s.VMPTE = fields[1]
		case "VmPMD:":
			s.VMPMD = fields[1]
		case "VmSwap:":
			s.VMSwap = fields[1]
		case "HugetlbPages:":
			s.HugetlbPages = fields[1]
		case "Threads:":
			s.Threads = fields[1]
		case "SigQ:":
			s.SigQ = fields[1]
		case "SigPnd:":
			s.SigPnd = fields[1]
		case "ShdPnd:":
			s.ShdPnd = fields[1]
		case "SigBlk:":
			s.SigBlk = fields[1]
		case "SigIgn:":
			s.SigIgn = fields[1]
		case "SigCgt:":
			s.SigCgt = fields[1]
		case "CapInh:":
			s.CapInh = fields[1]
		case "CapPrm:":
			s.CapPrm = fields[1]
		case "CapEff:":
			s.CapEff = fields[1]
		case "CapBnd:":
			s.CapBnd = fields[1]
		case "CapAmb:":
			s.CapAmb = fields[1]
		case "NoNewPrivs:":
			s.NoNewPrivs = fields[1]
		case "Seccomp:":
			s.Seccomp = fields[1]
		case "Seccomp_filters:":
			s.SeccompFilters = fields[1]
		case "Cpus_allowed:":
			s.CpusAllowed = fields[1]
		case "Cpus_allowed_list:":
			s.CpusAllowedList = fields[1]
		case "Mems_allowed:":
			s.MemsAllowed = fields[1]
		case "Mems_allowed_list:":
			s.MemsAllowedList = fields[1]
		case "voluntary_ctxt_switches:":
			s.VoluntaryCtxtSwitches = fields[1]
		case "nonvoluntary_ctxt_switches:":
			s.NonvoluntaryCtxtSwitches = fields[1]
		}
	}

	return &s, nil
}
