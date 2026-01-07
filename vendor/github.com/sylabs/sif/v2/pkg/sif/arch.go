// Copyright (c) 2021-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

var (
	hdrArchUnknown  archType = [...]byte{'0', '0', '\x00'}
	hdrArch386      archType = [...]byte{'0', '1', '\x00'}
	hdrArchAMD64    archType = [...]byte{'0', '2', '\x00'}
	hdrArchARM      archType = [...]byte{'0', '3', '\x00'}
	hdrArchARM64    archType = [...]byte{'0', '4', '\x00'}
	hdrArchPPC64    archType = [...]byte{'0', '5', '\x00'}
	hdrArchPPC64le  archType = [...]byte{'0', '6', '\x00'}
	hdrArchMIPS     archType = [...]byte{'0', '7', '\x00'}
	hdrArchMIPSle   archType = [...]byte{'0', '8', '\x00'}
	hdrArchMIPS64   archType = [...]byte{'0', '9', '\x00'}
	hdrArchMIPS64le archType = [...]byte{'1', '0', '\x00'}
	hdrArchS390x    archType = [...]byte{'1', '1', '\x00'}
	hdrArchRISCV64  archType = [...]byte{'1', '2', '\x00'}
)

type archType [3]byte

// getSIFArch returns the archType corresponding to go runtime arch.
func getSIFArch(arch string) archType {
	archMap := map[string]archType{
		"386":      hdrArch386,
		"amd64":    hdrArchAMD64,
		"arm":      hdrArchARM,
		"arm64":    hdrArchARM64,
		"ppc64":    hdrArchPPC64,
		"ppc64le":  hdrArchPPC64le,
		"mips":     hdrArchMIPS,
		"mipsle":   hdrArchMIPSle,
		"mips64":   hdrArchMIPS64,
		"mips64le": hdrArchMIPS64le,
		"s390x":    hdrArchS390x,
		"riscv64":  hdrArchRISCV64,
	}

	t, ok := archMap[arch]
	if !ok {
		return hdrArchUnknown
	}
	return t
}

// GoArch returns the go runtime arch corresponding to t.
func (t archType) GoArch() string {
	archMap := map[archType]string{
		hdrArch386:      "386",
		hdrArchAMD64:    "amd64",
		hdrArchARM:      "arm",
		hdrArchARM64:    "arm64",
		hdrArchPPC64:    "ppc64",
		hdrArchPPC64le:  "ppc64le",
		hdrArchMIPS:     "mips",
		hdrArchMIPSle:   "mipsle",
		hdrArchMIPS64:   "mips64",
		hdrArchMIPS64le: "mips64le",
		hdrArchS390x:    "s390x",
		hdrArchRISCV64:  "riscv64",
	}

	arch, ok := archMap[t]
	if !ok {
		arch = "unknown"
	}
	return arch
}
