//go:build !linux

package commandconn

import (
	"os/exec"
)

func setPdeathsig(*exec.Cmd) {}
