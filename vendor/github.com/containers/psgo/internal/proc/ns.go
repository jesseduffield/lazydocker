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
	"io"
	"os"

	"go.podman.io/storage/pkg/idtools"
)

// ParsePIDNamespace returns the content of /proc/$pid/ns/pid.
func ParsePIDNamespace(pid string) (string, error) {
	pidNS, err := os.Readlink(fmt.Sprintf("/proc/%s/ns/pid", pid))
	if err != nil {
		return "", err
	}
	return pidNS, nil
}

// ParseUserNamespace returns the content of /proc/$pid/ns/user.
func ParseUserNamespace(pid string) (string, error) {
	userNS, err := os.Readlink(fmt.Sprintf("/proc/%s/ns/user", pid))
	if err != nil {
		return "", err
	}
	return userNS, nil
}

// ReadMappings reads the user namespace mappings at the specified path
func ReadMappings(path string) ([]idtools.IDMap, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var mappings []idtools.IDMap

	buf := bufio.NewReader(file)
	for {
		line, _, err := buf.ReadLine()
		if err != nil {
			if err == io.EOF { //nolint:errorlint // False positive, see https://github.com/polyfloyd/go-errorlint/pull/12
				return mappings, nil
			}
			return nil, fmt.Errorf("cannot read line from %s: %w", path, err)
		}
		if line == nil {
			return mappings, nil
		}

		var containerID, hostID, size int
		if _, err := fmt.Sscanf(string(line), "%d %d %d", &containerID, &hostID, &size); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", string(line), err)
		}
		mappings = append(mappings, idtools.IDMap{ContainerID: containerID, HostID: hostID, Size: size})
	}
}
