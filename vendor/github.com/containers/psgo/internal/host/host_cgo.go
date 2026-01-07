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

/*
#include <unistd.h>
*/
import "C"

var (
	// cache host queries to redundant calculations
	clockTicks *int64
	bootTime   *int64
)

// ClockTicks returns sysconf(SC_CLK_TCK).
func ClockTicks() (int64, error) {
	if clockTicks == nil {
		ticks := int64(C.sysconf(C._SC_CLK_TCK))
		clockTicks = &ticks
	}
	return *clockTicks, nil
}
