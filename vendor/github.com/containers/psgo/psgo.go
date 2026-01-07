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

// Package psgo is a ps (1) AIX-format compatible golang library extended with
// various descriptors useful for displaying container-related data.
//
// The idea behind the library is to provide an easy to use way of extracting
// process-related data, just as ps (1) does. The problem when using ps (1) is
// that the ps format strings split columns with whitespaces, making the output
// nearly impossible to parse. It also adds some jitter as we have to fork and
// execute ps either in the container or filter the output afterwards, further
// limiting applicability.
//
// Please visit https://github.com/containers/psgo for further details about
// supported format descriptors and to see some usage examples.
package psgo

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/containers/psgo/internal/capabilities"
	"github.com/containers/psgo/internal/dev"
	"github.com/containers/psgo/internal/proc"
	"github.com/containers/psgo/internal/process"
	"go.podman.io/storage/pkg/idtools"
	"golang.org/x/sys/unix"
)

// JoinNamespaceOpts specifies different options for joining the specified namespaces.
type JoinNamespaceOpts struct {
	// UIDMap specifies a mapping for UIDs in the container.  If specified
	// huser will perform the reverse mapping.
	UIDMap []idtools.IDMap
	// GIDMap specifies a mapping for GIDs in the container.  If specified
	// hgroup will perform the reverse mapping.
	GIDMap []idtools.IDMap

	// FillMappings specified whether UIDMap and GIDMap must be initialized
	// with the current user namespace.
	FillMappings bool
}

type psContext struct {
	// Processes in the container.
	containersProcesses []*process.Process
	// Processes on the host.  Used to map those to the ones running in the container.
	hostProcesses []*process.Process
	// tty and pty devices.
	ttys *[]dev.TTY
	// Various options
	opts *JoinNamespaceOpts
}

// processFunc is used to map a given aixFormatDescriptor to a corresponding
// function extracting the desired data from a process.
type processFunc func(*process.Process, *psContext) (string, error)

// aixFormatDescriptor as mentioned in the ps(1) manpage.  A given descriptor
// can either be specified via its code (e.g., "%C") or its normal representation
// (e.g., "pcpu") and will be printed under its corresponding header (e.g, "%CPU").
type aixFormatDescriptor struct {
	// code descriptor in the short form (e.g., "%C").
	code string
	// normal descriptor in the long form (e.g., "pcpu").
	normal string
	// header of the descriptor (e.g., "%CPU").
	header string
	// onHost controls if data of the corresponding host processes will be
	// extracted as well.
	onHost bool
	// procFN points to the corresponding method to extract the desired data.
	procFn processFunc
}

// findID converts the specified id to the host mapping
func findID(idStr string, mapping []idtools.IDMap, lookupFunc func(uid string) (string, error), overflowFile string) (string, error) {
	if len(mapping) == 0 {
		return idStr, nil
	}

	id, err := strconv.ParseInt(idStr, 10, 0)
	if err != nil {
		return "", fmt.Errorf("cannot parse ID: %w", err)
	}
	for _, m := range mapping {
		if int(id) >= m.ContainerID && int(id) < m.ContainerID+m.Size {
			user := fmt.Sprintf("%d", m.HostID+(int(id)-m.ContainerID))

			return lookupFunc(user)
		}
	}

	// User not found, read the overflow
	overflow, err := os.ReadFile(overflowFile)
	if err != nil {
		return "", err
	}
	return string(overflow), nil
}

// translateDescriptors parses the descriptors and returns a correspodning slice of
// aixFormatDescriptors.  Descriptors can be specified in the normal and in the
// code form (if supported).  If the descriptors slice is empty, the
// `DefaultDescriptors` is used.
func translateDescriptors(descriptors []string) ([]aixFormatDescriptor, error) {
	if len(descriptors) == 0 {
		descriptors = DefaultDescriptors
	}

	formatDescriptors := []aixFormatDescriptor{}
	for _, d := range descriptors {
		d = strings.TrimSpace(d)
		found := false
		for _, aix := range aixFormatDescriptors {
			if d == aix.code || d == aix.normal {
				formatDescriptors = append(formatDescriptors, aix)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("'%s': %w", d, ErrUnknownDescriptor)
		}
	}

	return formatDescriptors, nil
}

var (
	// DefaultDescriptors is the `ps -ef` compatible default format.
	DefaultDescriptors = []string{"user", "pid", "ppid", "pcpu", "etime", "tty", "time", "args"}

	// ErrUnknownDescriptor is returned when an unknown descriptor is parsed.
	ErrUnknownDescriptor = errors.New("unknown descriptor")

	aixFormatDescriptors = []aixFormatDescriptor{
		{
			code:   "%C",
			normal: "pcpu",
			header: "%CPU",
			procFn: processPCPU,
		},
		{
			code:   "%G",
			normal: "group",
			header: "GROUP",
			procFn: processGROUP,
		},
		{
			normal: "groups",
			header: "GROUPS",
			procFn: processGROUPS,
		},
		{
			code:   "%P",
			normal: "ppid",
			header: "PPID",
			procFn: processPPID,
		},
		{
			code:   "%U",
			normal: "user",
			header: "USER",
			procFn: processUSER,
		},
		{
			normal: "uid",
			header: "UID",
			procFn: processUID,
		},
		{
			code:   "%a",
			normal: "args",
			header: "COMMAND",
			procFn: processARGS,
		},
		{
			code:   "%c",
			normal: "comm",
			header: "COMMAND",
			procFn: processCOMM,
		},
		{
			code:   "%g",
			normal: "rgroup",
			header: "RGROUP",
			procFn: processRGROUP,
		},
		{
			code:   "%n",
			normal: "nice",
			header: "NI",
			procFn: processNICE,
		},
		{
			code:   "%p",
			normal: "pid",
			header: "PID",
			procFn: processPID,
		},
		{
			code:   "%r",
			normal: "pgid",
			header: "PGID",
			procFn: processPGID,
		},
		{
			code:   "%t",
			normal: "etime",
			header: "ELAPSED",
			procFn: processETIME,
		},
		{
			code:   "%u",
			normal: "ruser",
			header: "RUSER",
			procFn: processRUSER,
		},
		{
			code:   "%x",
			normal: "time",
			header: "TIME",
			procFn: processTIME,
		},
		{
			code:   "%y",
			normal: "tty",
			header: "TTY",
			procFn: processTTY,
		},
		{
			code:   "%z",
			normal: "vsz",
			header: "VSZ",
			procFn: processVSZ,
		},
		{
			normal: "capamb",
			header: "AMBIENT CAPS",
			procFn: processCAPAMB,
		},
		{
			normal: "capinh",
			header: "INHERITED CAPS",
			procFn: processCAPINH,
		},
		{
			normal: "capprm",
			header: "PERMITTED CAPS",
			procFn: processCAPPRM,
		},
		{
			normal: "capeff",
			header: "EFFECTIVE CAPS",
			procFn: processCAPEFF,
		},
		{
			normal: "capbnd",
			header: "BOUNDING CAPS",
			procFn: processCAPBND,
		},
		{
			normal: "seccomp",
			header: "SECCOMP",
			procFn: processSECCOMP,
		},
		{
			normal: "label",
			header: "LABEL",
			procFn: processLABEL,
		},
		{
			normal: "hpid",
			header: "HPID",
			onHost: true,
			procFn: processHPID,
		},
		{
			normal: "huser",
			header: "HUSER",
			onHost: true,
			procFn: processHUSER,
		},
		{
			normal: "huid",
			header: "HUID",
			onHost: true,
			procFn: processHUID,
		},
		{
			normal: "hgroup",
			header: "HGROUP",
			onHost: true,
			procFn: processHGROUP,
		},
		{
			normal: "hgroups",
			header: "HGROUPS",
			onHost: true,
			procFn: processHGROUPS,
		},
		{
			normal: "rss",
			header: "RSS",
			procFn: processRSS,
		},
		{
			normal: "state",
			header: "STATE",
			procFn: processState,
		},
		{
			normal: "stime",
			header: "STIME",
			procFn: processStartTime,
		},
	}
)

// ListDescriptors returns a string slice of all supported AIX format
// descriptors in the normal form.
func ListDescriptors() (list []string) {
	for _, d := range aixFormatDescriptors {
		list = append(list, d.normal)
	}
	sort.Strings(list)
	return
}

// JoinNamespaceAndProcessInfo has the same semantics as ProcessInfo but joins
// the mount namespace of the specified pid before extracting data from `/proc`.
func JoinNamespaceAndProcessInfo(pid string, descriptors []string) ([][]string, error) {
	return JoinNamespaceAndProcessInfoWithOptions(pid, descriptors, &JoinNamespaceOpts{})
}

func contextFromOptions(options *JoinNamespaceOpts) (*psContext, error) {
	ctx := new(psContext)
	ctx.opts = options
	if ctx.opts != nil && ctx.opts.FillMappings {
		uidMappings, err := proc.ReadMappings("/proc/self/uid_map")
		if err != nil {
			return nil, err
		}

		gidMappings, err := proc.ReadMappings("/proc/self/gid_map")
		if err != nil {
			return nil, err
		}
		ctx.opts.UIDMap = uidMappings
		ctx.opts.GIDMap = gidMappings

		ctx.opts.FillMappings = false
	}
	return ctx, nil
}

// JoinNamespaceAndProcessInfoWithOptions has the same semantics as ProcessInfo but joins
// the mount namespace of the specified pid before extracting data from `/proc`.
func JoinNamespaceAndProcessInfoWithOptions(pid string, descriptors []string, options *JoinNamespaceOpts) ([][]string, error) {
	var (
		data    [][]string
		dataErr error
		wg      sync.WaitGroup
	)

	aixDescriptors, err := translateDescriptors(descriptors)
	if err != nil {
		return nil, err
	}

	ctx, err := contextFromOptions(options)
	if err != nil {
		return nil, err
	}

	// extract data from host processes only on-demand / when at least one
	// of the specified descriptors requires host data
	for _, d := range aixDescriptors {
		if d.onHost {
			ctx.hostProcesses, err = hostProcesses(pid)
			if err != nil {
				return nil, err
			}
			break
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		runtime.LockOSThread()

		// extract user namespaces prior to joining the mount namespace
		currentUserNs, err := proc.ParseUserNamespace("self")
		if err != nil {
			dataErr = fmt.Errorf("error determining user namespace: %w", err)
			return
		}

		pidUserNs, err := proc.ParseUserNamespace(pid)
		if err != nil {
			dataErr = fmt.Errorf("error determining user namespace of PID %s: %w", pid, err)
		}

		// join the mount namespace of pid
		fd, err := os.Open(fmt.Sprintf("/proc/%s/ns/mnt", pid))
		if err != nil {
			dataErr = err
			return
		}
		defer fd.Close()

		// create a new mountns on the current thread
		if err = unix.Unshare(unix.CLONE_NEWNS); err != nil {
			dataErr = err
			return
		}
		if err := unix.Setns(int(fd.Fd()), unix.CLONE_NEWNS); err != nil {
			dataErr = err
			return
		}

		// extract all pids mentioned in pid's mount namespace
		pids, err := proc.GetPIDs()
		if err != nil {
			dataErr = err
			return
		}

		// join the user NS if the pid's user NS is different
		// to the caller's user NS.
		joinUserNS := currentUserNs != pidUserNs

		ctx.containersProcesses, err = process.FromPIDs(pids, joinUserNS)
		if err != nil {
			dataErr = err
			return
		}

		data, dataErr = processDescriptors(aixDescriptors, ctx)
	}()
	wg.Wait()

	return data, dataErr
}

// JoinNamespaceAndProcessInfoByPidsWithOptions has similar semantics to
// JoinNamespaceAndProcessInfo and avoids duplicate entries by joining a giving
// PID namespace only once.
func JoinNamespaceAndProcessInfoByPidsWithOptions(pids []string, descriptors []string, options *JoinNamespaceOpts) ([][]string, error) {
	// Extracting data from processes that share the same PID namespace
	// would yield duplicate results.  Avoid that by extracting data only
	// from the first process in `pids` from a given PID namespace.
	// `nsMap` is used for quick lookups if a given PID namespace is
	// already covered, `pidList` is used to preserve the order which is
	// not guaranteed by nondeterministic maps in golang.
	nsMap := make(map[string]bool)
	pidList := []string{}
	for _, pid := range pids {
		ns, err := proc.ParsePIDNamespace(pid)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
				// catch race conditions
				continue
			}
			return nil, fmt.Errorf("error extracting PID namespace: %w", err)
		}
		if _, exists := nsMap[ns]; !exists {
			nsMap[ns] = true
			pidList = append(pidList, pid)
		}
	}

	data := [][]string{}
	for i, pid := range pidList {
		pidData, err := JoinNamespaceAndProcessInfoWithOptions(pid, descriptors, options)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
			// catch race conditions
			continue
		}
		if err != nil {
			return nil, err
		}
		if i == 0 {
			data = append(data, pidData[0])
		}
		data = append(data, pidData[1:]...)
	}

	return data, nil
}

// JoinNamespaceAndProcessInfoByPids has similar semantics to
// JoinNamespaceAndProcessInfo and avoids duplicate entries by joining a giving
// PID namespace only once.
func JoinNamespaceAndProcessInfoByPids(pids []string, descriptors []string) ([][]string, error) {
	return JoinNamespaceAndProcessInfoByPidsWithOptions(pids, descriptors, &JoinNamespaceOpts{})
}

// ProcessInfo returns the process information of all processes in the current
// mount namespace. The input format must be a comma-separated list of
// supported AIX format descriptors.  If the input string is empty, the
// `DefaultDescriptors` is used.
// The return value is an array of tab-separated strings, to easily use the
// output for column-based formatting (e.g., with the `text/tabwriter` package).
func ProcessInfo(descriptors []string) ([][]string, error) {
	pids, err := proc.GetPIDs()
	if err != nil {
		return nil, err
	}

	return ProcessInfoByPids(pids, descriptors)
}

// ProcessInfoByPids is like ProcessInfo, but the process information returned
// is limited to a list of user specified PIDs.
func ProcessInfoByPids(pids []string, descriptors []string) ([][]string, error) {
	aixDescriptors, err := translateDescriptors(descriptors)
	if err != nil {
		return nil, err
	}

	ctx, err := contextFromOptions(nil)
	if err != nil {
		return nil, err
	}
	ctx.containersProcesses, err = process.FromPIDs(pids, false)
	if err != nil {
		return nil, err
	}

	return processDescriptors(aixDescriptors, ctx)
}

// hostProcesses returns all processes running in the current namespace.
func hostProcesses(pid string) ([]*process.Process, error) {
	// get processes
	pids, err := proc.GetPIDsFromCgroup(pid)
	if err != nil {
		return nil, err
	}

	processes, err := process.FromPIDs(pids, false)
	if err != nil {
		return nil, err
	}

	// set the additional host data
	for _, p := range processes {
		if err := p.SetHostData(); err != nil {
			return nil, err
		}
	}

	return processes, nil
}

// processDescriptors calls each `procFn` of all formatDescriptors on each
// process and returns an array of tab-separated strings.
func processDescriptors(formatDescriptors []aixFormatDescriptor, ctx *psContext) ([][]string, error) {
	data := [][]string{}
	// create header
	header := []string{}
	for _, desc := range formatDescriptors {
		header = append(header, desc.header)
	}
	data = append(data, header)

	// dispatch all descriptor functions on each process
	for _, proc := range ctx.containersProcesses {
		pData := []string{}
		for _, desc := range formatDescriptors {
			dataStr, err := desc.procFn(proc, ctx)
			if err != nil {
				return nil, err
			}
			pData = append(pData, dataStr)
		}
		data = append(data, pData)
	}

	return data, nil
}

// findHostProcess returns the corresponding process from `hostProcesses` or
// nil if non is found.
func findHostProcess(p *process.Process, ctx *psContext) *process.Process {
	for _, hp := range ctx.hostProcesses {
		// We expect the host process to be in another namespace, so
		// /proc/$pid/status.NSpid must have at least two entries.
		if len(hp.Status.NSpid) < 2 {
			continue
		}
		// The process' PID must match the one in the NS of the host
		// process and both must share the same pid NS.
		if p.Pid == hp.Status.NSpid[1] && p.PidNS == hp.PidNS {
			return hp
		}
	}
	return nil
}

// processGROUP returns the effective group ID of the process.  This will be
// the textual group ID, if it can be obtained, or a decimal representation
// otherwise.
func processGROUP(p *process.Process, ctx *psContext) (string, error) {
	return process.LookupGID(p.Status.Gids[1])
}

// processGROUPS returns the supplementary groups of the process separated by
// comma. This will be the textual group ID, if it can be obtained, or a
// decimal representation otherwise.
func processGROUPS(p *process.Process, ctx *psContext) (string, error) {
	var err error
	groups := make([]string, len(p.Status.Groups))
	for i, g := range p.Status.Groups {
		groups[i], err = process.LookupGID(g)
		if err != nil {
			return "", err
		}
	}
	return strings.Join(groups, ","), nil
}

// processRGROUP returns the real group ID of the process.  This will be
// the textual group ID, if it can be obtained, or a decimal representation
// otherwise.
func processRGROUP(p *process.Process, ctx *psContext) (string, error) {
	return process.LookupGID(p.Status.Gids[0])
}

// processPPID returns the parent process ID of process p.
func processPPID(p *process.Process, ctx *psContext) (string, error) {
	return p.Status.PPid, nil
}

// processUSER returns the effective user name of the process.  This will be
// the textual user ID, if it can be obtained, or a decimal representation
// otherwise.
func processUSER(p *process.Process, ctx *psContext) (string, error) {
	return process.LookupUID(p.Status.Uids[1])
}

// processUID returns the effective UID of the process as the decimal representation.
func processUID(p *process.Process, ctx *psContext) (string, error) {
	return p.Status.Uids[1], nil
}

// processRUSER returns the effective user name of the process.  This will be
// the textual user ID, if it can be obtained, or a decimal representation
// otherwise.
func processRUSER(p *process.Process, ctx *psContext) (string, error) {
	return process.LookupUID(p.Status.Uids[0])
}

// processName returns the name of process p in the format "[$name]".
func processName(p *process.Process, ctx *psContext) (string, error) {
	return fmt.Sprintf("[%s]", p.Status.Name), nil
}

// processARGS returns the command of p with all its arguments.
func processARGS(p *process.Process, ctx *psContext) (string, error) {
	// ps (1) returns "[$name]" if command/args are empty
	if p.CmdLine[0] == "" {
		return processName(p, ctx)
	}
	return strings.Join(p.CmdLine, " "), nil
}

// processCOMM returns the command name (i.e., executable name) of process p.
func processCOMM(p *process.Process, ctx *psContext) (string, error) {
	return p.Stat.Comm, nil
}

// processNICE returns the nice value of process p.
func processNICE(p *process.Process, ctx *psContext) (string, error) {
	return p.Stat.Nice, nil
}

// processPID returns the process ID of process p.
func processPID(p *process.Process, ctx *psContext) (string, error) {
	return p.Pid, nil
}

// processPGID returns the process group ID of process p.
func processPGID(p *process.Process, ctx *psContext) (string, error) {
	return p.Stat.Pgrp, nil
}

// processPCPU returns how many percent of the CPU time process p uses as
// a three digit float as string.
func processPCPU(p *process.Process, ctx *psContext) (string, error) {
	elapsed, err := p.ElapsedTime()
	if err != nil {
		return "", err
	}
	cpu, err := p.CPUTime()
	if err != nil {
		return "", err
	}
	pcpu := 100 * cpu.Seconds() / elapsed.Seconds()

	return strconv.FormatFloat(pcpu, 'f', 3, 64), nil
}

// processETIME returns the elapsed time since the process was started.
func processETIME(p *process.Process, ctx *psContext) (string, error) {
	elapsed, err := p.ElapsedTime()
	if err != nil {
		return "", nil
	}
	return fmt.Sprintf("%v", elapsed), nil
}

// processTIME returns the cumulative CPU time of process p.
func processTIME(p *process.Process, ctx *psContext) (string, error) {
	cpu, err := p.CPUTime()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", cpu), nil
}

// processStartTime returns the start time of process p.
func processStartTime(p *process.Process, ctx *psContext) (string, error) {
	sTime, err := p.StartTime()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", sTime), nil
}

// processTTY returns the controlling tty (terminal) of process p.
func processTTY(p *process.Process, ctx *psContext) (string, error) {
	ttyNr, err := strconv.ParseUint(p.Stat.TtyNr, 10, 64)
	if err != nil {
		return "", nil
	}

	tty, err := dev.FindTTY(ttyNr, ctx.ttys)
	if err != nil {
		return "", nil
	}

	ttyS := "?"
	if tty != nil {
		ttyS = strings.TrimPrefix(tty.Path, "/dev/")
	}
	return ttyS, nil
}

// processVSZ returns the virtual memory size of process p in KiB (1024-byte
// units).
func processVSZ(p *process.Process, ctx *psContext) (string, error) {
	vmsize, err := strconv.Atoi(p.Stat.Vsize)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", vmsize/1024), nil
}

// parseCAP parses cap (a string bit mask) and returns the associated set of
// capabilities.  If all capabilities are set, "full" is returned.  If no
// capability is enabled, "none" is returned.
func parseCAP(cap string) (string, error) {
	mask, err := strconv.ParseUint(cap, 16, 64)
	if err != nil {
		return "", err
	}
	if mask == capabilities.FullCAPs {
		return "full", nil
	}
	caps := capabilities.TranslateMask(mask)
	if len(caps) == 0 {
		return "none", nil
	}
	sort.Strings(caps)
	return strings.Join(caps, ","), nil
}

// processCAPAMB returns the set of ambient capabilities associated with
// process p.  If all capabilities are set, "full" is returned.  If no
// capability is enabled, "none" is returned.
func processCAPAMB(p *process.Process, ctx *psContext) (string, error) {
	return parseCAP(p.Status.CapAmb)
}

// processCAPINH returns the set of inheritable capabilities associated with
// process p.  If all capabilities are set, "full" is returned.  If no
// capability is enabled, "none" is returned.
func processCAPINH(p *process.Process, ctx *psContext) (string, error) {
	return parseCAP(p.Status.CapInh)
}

// processCAPPRM returns the set of permitted capabilities associated with
// process p.  If all capabilities are set, "full" is returned.  If no
// capability is enabled, "none" is returned.
func processCAPPRM(p *process.Process, ctx *psContext) (string, error) {
	return parseCAP(p.Status.CapPrm)
}

// processCAPEFF returns the set of effective capabilities associated with
// process p.  If all capabilities are set, "full" is returned.  If no
// capability is enabled, "none" is returned.
func processCAPEFF(p *process.Process, ctx *psContext) (string, error) {
	return parseCAP(p.Status.CapEff)
}

// processCAPBND returns the set of bounding capabilities associated with
// process p.  If all capabilities are set, "full" is returned.  If no
// capability is enabled, "none" is returned.
func processCAPBND(p *process.Process, ctx *psContext) (string, error) {
	return parseCAP(p.Status.CapBnd)
}

// processSECCOMP returns the seccomp mode of the process (i.e., disabled,
// strict or filter) or "?" if /proc/$pid/status.seccomp has a unknown value.
func processSECCOMP(p *process.Process, ctx *psContext) (string, error) {
	switch p.Status.Seccomp {
	case "0":
		return "disabled", nil
	case "1":
		return "strict", nil
	case "2":
		return "filter", nil
	default:
		return "?", nil
	}
}

// processLABEL returns the process label of process p or "?" if the system
// doesn't support labeling.
func processLABEL(p *process.Process, ctx *psContext) (string, error) {
	return p.Label, nil
}

// processHPID returns the PID of the corresponding host process of the
// (container) or "?" if no corresponding process could be found.
func processHPID(p *process.Process, ctx *psContext) (string, error) {
	if hp := findHostProcess(p, ctx); hp != nil {
		return hp.Pid, nil
	}
	return "?", nil
}

// processHUSER returns the effective user ID of the corresponding host process
// of the (container) or "?" if no corresponding process could be found.
func processHUSER(p *process.Process, ctx *psContext) (string, error) {
	if hp := findHostProcess(p, ctx); hp != nil {
		if ctx.opts != nil && len(ctx.opts.UIDMap) > 0 {
			return findID(hp.Status.Uids[1], ctx.opts.UIDMap, process.LookupUID, "/proc/sys/fs/overflowuid")
		}
		return hp.Huser, nil
	}
	return "?", nil
}

// processHUID returns the effective UID of the corresponding host process
// of the (container) as the decimal representation or "?" if no corresponding
// process could be found.
func processHUID(p *process.Process, ctx *psContext) (string, error) {
	if hp := findHostProcess(p, ctx); hp != nil {
		if ctx.opts != nil && len(ctx.opts.UIDMap) > 0 {
			// Return uid without searching its textual representation.
			lookupFunc := func(uid string) (string, error) {
				return uid, nil
			}
			return findID(hp.Status.Uids[1], ctx.opts.UIDMap, lookupFunc, "/proc/sys/fs/overflowuid")
		}
		return hp.Status.Uids[1], nil
	}
	return "?", nil
}

// processHGROUP returns the effective group ID of the corresponding host
// process of the (container) or "?" if no corresponding process could be
// found.
func processHGROUP(p *process.Process, ctx *psContext) (string, error) {
	if hp := findHostProcess(p, ctx); hp != nil {
		if ctx.opts != nil && len(ctx.opts.GIDMap) > 0 {
			return findID(hp.Status.Gids[1], ctx.opts.GIDMap, process.LookupGID, "/proc/sys/fs/overflowgid")
		}
		return hp.Hgroup, nil
	}
	return "?", nil
}

// processHGROUPS returns the supplementary groups of the corresponding host
// process of the (container) or "?" if no corresponding process could be
// found.
func processHGROUPS(p *process.Process, ctx *psContext) (string, error) {
	if hp := findHostProcess(p, ctx); hp != nil {
		groups := hp.Status.Groups
		if ctx.opts != nil && len(ctx.opts.GIDMap) > 0 {
			var err error
			for i, g := range groups {
				groups[i], err = findID(g, ctx.opts.GIDMap, process.LookupGID, "/proc/sys/fs/overflowgid")
				if err != nil {
					return "", err
				}
			}
		}
		return strings.Join(groups, ","), nil
	}
	return "?", nil
}

// processRSS returns the resident set size of process p in KiB (1024-byte
// units).
func processRSS(p *process.Process, ctx *psContext) (string, error) {
	if p.Status.VMRSS == "" {
		// probably a kernel thread
		return "0", nil
	}
	return p.Status.VMRSS, nil
}

// processState returns the process state of process p.
func processState(p *process.Process, ctx *psContext) (string, error) {
	return p.Status.State, nil
}
