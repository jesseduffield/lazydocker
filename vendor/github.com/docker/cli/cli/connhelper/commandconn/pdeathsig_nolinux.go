//go:build !linux
// +build !linux

package commandconn

import (
	"os/exec"
)

func setPdeathsig(cmd *exec.Cmd) {
}
