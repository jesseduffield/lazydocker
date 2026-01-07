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

	"golang.org/x/sys/unix"
)

// ParseAttrCurrent returns the contents of /proc/$pid/attr/current of "?" if
// labeling is not supported on the host.
func ParseAttrCurrent(pid string) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%s/attr/current", pid))
	if err != nil {
		_, err = os.Stat(fmt.Sprintf("/proc/%s", pid))
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
			// PID doesn't exist
			return "", err
		}
		// PID exists but labeling seems to be unsupported
		return "?", nil
	}
	return strings.Trim(string(data), "\n"), nil
}
