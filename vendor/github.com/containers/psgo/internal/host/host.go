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

// Package host extracts data from the host, such as the system's boot time or
// the tick rate of the system clock.
package host

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// BootTime parses /proc/uptime returns the boot time in seconds since the
// Epoch, 1970-01-01 00:00:00 +0000 (UTC).
func BootTime() (int64, error) {
	if bootTime != nil {
		return *bootTime, nil
	}

	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, err
	}

	btimeStr := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "btime" {
			btimeStr = fields[1]
		}
	}

	if len(btimeStr) == 0 {
		return 0, fmt.Errorf("couldn't extract boot time from /proc/stat")
	}

	btimeSec, err := strconv.ParseInt(btimeStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing boot time from /proc/stat: %w", err)
	}
	bootTime = &btimeSec
	return btimeSec, nil
}
