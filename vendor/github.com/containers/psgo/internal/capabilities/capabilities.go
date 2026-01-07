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

// Package capabilities provides a mapping from common kernel bit masks to the
// alphanumerical representation of kernel capabilities.  See capabilities(7)
// for additional information.
package capabilities

var (
	// capabilities are a mapping from a numerical value to the textual
	// representation of a given capability.  A map allows to easily check
	// if a given value is included or not.
	//
	// NOTE: this map must be maintained and kept in sync with the
	//       ./include/uapi/linux/capability.h kernel header.
	capabilities = map[uint]string{
		0:  "CHOWN",
		1:  "DAC_OVERRIDE",
		2:  "DAC_READ_SEARCH",
		3:  "FOWNER",
		4:  "FSETID",
		5:  "KILL",
		6:  "SETGID",
		7:  "SETUID",
		8:  "SETPCAP",
		9:  "LINUX_IMMUTABLE",
		10: "NET_BIND_SERVICE",
		11: "NET_BROADCAST",
		12: "NET_ADMIN",
		13: "NET_RAW",
		14: "IPC_LOCK",
		15: "IPC_OWNER",
		16: "SYS_MODULE",
		17: "SYS_RAWIO",
		18: "SYS_CHROOT",
		19: "SYS_PTRACE",
		20: "SYS_PACCT",
		21: "SYS_ADMIN",
		22: "SYS_BOOT",
		23: "SYS_NICE",
		24: "SYS_RESOURCE",
		25: "SYS_TIME",
		26: "SYS_TTY_CONFIG",
		27: "MKNOD",
		28: "LEASE",
		29: "AUDIT_WRITE",
		30: "AUDIT_CONTROL",
		31: "SETFCAP",
		32: "MAC_OVERRIDE",
		33: "MAC_ADMIN",
		34: "SYSLOG",
		35: "WAKE_ALARM",
		36: "BLOCK_SUSPEND",
		37: "AUDIT_READ",
		38: "PERFMON",
		39: "BPF",
		40: "CHECKPOINT_RESTORE",
	}

	// FullCAPs represents the value of a bitmask with a full capability
	// set.
	FullCAPs = uint64(0x1FFFFFFFFFF)
)

// TranslateMask iterates over mask and returns a slice of corresponding
// capabilities.  If a bit is out of range of known capabilities, it is set as
// "unknown" to catch potential regressions when new capabilities are added to
// the kernel.
func TranslateMask(mask uint64) []string {
	caps := []string{}
	for i := uint(0); i < 64; i++ {
		if (mask>>i)&0x1 == 1 {
			c, known := capabilities[i]
			if !known {
				c = "unknown"
			}
			caps = append(caps, c)
		}
	}
	return caps
}
