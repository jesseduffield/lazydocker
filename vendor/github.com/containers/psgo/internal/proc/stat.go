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
	"errors"
	"fmt"
	"os"
	"strings"
)

// Stat is a direct translation of a `/proc/[pid]/stat` file as described in
// the proc(5) manpage. Please note that it is not a full translation as not
// all fields are in the scope of this library and higher indices are
// Kernel-version dependent.
type Stat struct {
	// (1) The process ID
	Pid string
	// (2) The filename of the executable, in parentheses. This is visible
	// whether or not the executable is swapped out.
	Comm string
	// (3) The process state (e.g., running, sleeping, zombie, dead).
	// Refer to proc(5) for further details.
	State string
	// (4) The PID of the parent of this process.
	Ppid string
	// (5) The process group ID of the process.
	Pgrp string
	// (6) The session ID of the process.
	Session string
	// (7) The controlling terminal of the process. (The minor device
	// number is contained in the combination of bits 31 to 20 and 7 to 0;
	// the major device number is in bits 15 to 8.)
	TtyNr string
	// (8) The ID of the foreground process group of the controlling
	// terminal of the process.
	Tpgid string
	// (9) The kernel flags word of the process. For bit meanings, see the
	// PF_* defines in the Linux kernel source file
	// include/linux/sched.h. Details depend on the kernel version.
	Flags string
	// (10) The number of minor faults the process has made which have not
	// required loading a memory page from disk.
	Minflt string
	// (11) The number of minor faults that the process's waited-for
	// children have made.
	Cminflt string
	// (12) The number of major faults the process has made which have
	// required loading a memory page from disk.
	Majflt string
	// (13) The number of major faults that the process's waited-for
	// children have made.
	Cmajflt string
	// (14) Amount of time that this process has been scheduled in user
	// mode, measured in clock ticks (divide by
	// sysconf(_SC_CLK_TCK)). This includes guest time, guest_time
	// (time spent running a virtual CPU, see below), so that applications
	// that are not aware of the guest time field do not lose that time
	// from their calculations.
	Utime string
	// (15) Amount of time that this process has been scheduled in kernel
	// mode, measured in clock ticks (divide by sysconf(_SC_CLK_TCK)).
	Stime string
	// (16) Amount of time that this process's waited-for children have
	// been scheduled in user mode, measured in clock ticks (divide by
	// sysconf(_SC_CLK_TCK)). (See also times(2).) This includes guest
	// time, cguest_time (time spent running a virtual CPU, see below).
	Cutime string
	// (17) Amount of time that this process's waited-for children have
	// been scheduled in kernel mode, measured in clock ticks (divide by
	// sysconf(_SC_CLK_TCK)).
	Cstime string
	// (18) (Explanation for Linux 2.6+) For processes running a real-time
	// scheduling policy (policy below; see sched_setscheduler(2)), this is
	// the negated scheduling pri- ority, minus one; that is, a number
	// in the range -2 to -100, corresponding to real-time priorities 1 to
	// 99. For processes running under a non-real-time scheduling
	// policy, this is the raw nice value (setpriority(2)) as represented
	// in the kernel. The kernel stores nice values as numbers in the
	// range 0 (high) to 39 (low), corresponding to the user-visible nice
	// range of -20 to 19.
	Priority string
	// (19) The nice value (see setpriority(2)), a value in the range 19
	// (low priority) to -20 (high priority).
	Nice string
	// (20) Number of threads in this process (since Linux 2.6). Before
	// kernel 2.6, this field was hard coded to 0 as a placeholder for an
	// earlier removed field.
	NumThreads string
	// (21) The time in jiffies before the next SIGALRM is sent to the
	// process due to an interval timer. Since kernel 2.6.17, this
	// field is no longer maintained, and is hard coded as 0.
	Itrealvalue string
	// (22) The time the process started after system boot. In kernels
	// before Linux 2.6, this value was expressed in jiffies. Since
	// Linux 2.6, the value is expressed in clock ticks (divide by
	// sysconf(_SC_CLK_TCK)).
	Starttime string
	// (23) Virtual memory size in bytes.
	Vsize string
}

// readStat is used for mocking in unit tests.
var readStat = func(path string) (string, error) {
	rawData, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(rawData), nil
}

// ParseStat parses the /proc/$pid/stat file and returns a Stat.
func ParseStat(pid string) (*Stat, error) {
	data, err := readStat(fmt.Sprintf("/proc/%s/stat", pid))
	if err != nil {
		return nil, err
	}

	firstParen := strings.IndexByte(data, '(')
	lastParen := strings.LastIndexByte(data, ')')
	if firstParen == -1 || lastParen == -1 {
		return nil, errors.New("invalid format in stat")
	}
	pidstr := data[0 : firstParen-1]
	comm := data[firstParen+1 : lastParen]
	rest := strings.Fields(data[lastParen+1:])
	fields := append([]string{pidstr, comm}, rest...)

	fieldAt := func(i int) string {
		return fields[i-1]
	}

	return &Stat{
		Pid:         fieldAt(1),
		Comm:        fieldAt(2),
		State:       fieldAt(3),
		Ppid:        fieldAt(4),
		Pgrp:        fieldAt(5),
		Session:     fieldAt(6),
		TtyNr:       fieldAt(7),
		Tpgid:       fieldAt(8),
		Flags:       fieldAt(9),
		Minflt:      fieldAt(10),
		Cminflt:     fieldAt(11),
		Majflt:      fieldAt(12),
		Cmajflt:     fieldAt(13),
		Utime:       fieldAt(14),
		Stime:       fieldAt(15),
		Cutime:      fieldAt(16),
		Cstime:      fieldAt(17),
		Priority:    fieldAt(18),
		Nice:        fieldAt(19),
		NumThreads:  fieldAt(20),
		Itrealvalue: fieldAt(21),
		Starttime:   fieldAt(22),
		Vsize:       fieldAt(23),
	}, nil
}
