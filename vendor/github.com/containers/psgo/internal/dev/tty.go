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

package dev

import (
	"os"
	"strings"
	"syscall"
)

// TTY represents a tty including its minor and major device number and the
// path to the tty.
type TTY struct {
	// Minor device number.
	Minor uint64
	// Major device number.
	Major uint64
	// Path to the tty device.
	Path string
}

// FindTTY return the corresponding TTY to the ttyNr or nil of non could be
// found.
func FindTTY(ttyNr uint64, devices *[]TTY) (*TTY, error) {
	// (man 5 proc) The minor device number is contained in the combination
	// of bits 31 to 20 and 7 to 0; the major device number is in bits 15
	// to 8.
	maj := (ttyNr >> 8) & 0xFF
	min := (ttyNr & 0xFF) | ((ttyNr >> 20) & 0xFFF)

	if devices == nil {
		devs, err := TTYs()
		if err != nil {
			return nil, err
		}
		devices = devs
	}

	for _, t := range *devices {
		if t.Minor == min && t.Major == maj {
			return &t, nil
		}
	}

	return nil, nil
}

// majDevNum returns the major device number of rdev (see stat_t.Rdev).
func majDevNum(rdev uint64) uint64 {
	return (rdev >> 8) & 0xfff
}

// minDevNum returns the minor device number of rdev (see stat_t.Rdev).
func minDevNum(rdev uint64) uint64 {
	return (rdev & 0xff) | ((rdev >> 12) & 0xfff00)
}

// TTYs parses /dev for tty and pts devices.
func TTYs() (*[]TTY, error) {
	devDir, err := os.Open("/dev/")
	if err != nil {
		return nil, err
	}
	defer devDir.Close()

	devices := []string{}
	devTTYs, err := devDir.Readdirnames(0)
	if err != nil {
		return nil, err
	}
	for _, d := range devTTYs {
		if !strings.HasPrefix(d, "tty") {
			continue
		}
		devices = append(devices, "/dev/"+d)
	}

	devPTSDir, err := os.Open("/dev/pts/")
	if err != nil {
		return nil, err
	}
	defer devPTSDir.Close()

	devPTSs, err := devPTSDir.Readdirnames(0)
	if err != nil {
		return nil, err
	}
	for _, d := range devPTSs {
		devices = append(devices, "/dev/pts/"+d)
	}

	ttys := []TTY{}
	for _, dev := range devices {
		fi, err := os.Stat(dev)
		if err != nil {
			if os.IsNotExist(err) {
				// catch race conditions
				continue
			}
			return nil, err
		}
		s := fi.Sys().(*syscall.Stat_t)
		t := TTY{
			// Rdev is type uint32 on mips arch so we have to cast to uint64
			Minor: minDevNum(uint64(s.Rdev)),
			Major: majDevNum(uint64(s.Rdev)),
			Path:  dev,
		}
		ttys = append(ttys, t)
	}

	return &ttys, nil
}
