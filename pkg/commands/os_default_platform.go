// +build !windows

package commands

import (
	"os/exec"
	"runtime"
	"syscall"
)

func getPlatform() *Platform {
	return &Platform{
		os:                   runtime.GOOS,
		shell:                "bash",
		shellArg:             "-c",
		escapedQuote:         "'",
		openCommand:          "open {{filename}}",
		openLinkCommand:      "open {{link}}",
		fallbackEscapedQuote: "\"",
	}
}

// Kill kills a process. If the process has Setpgid == true, then we have anticipated that it might spawn its own child processes, so we've given it a process group ID (PGID) equal to its process id (PID) and given its child processes will inherit the PGID, we can kill that group, rather than killing the process itself.
func (c *OSCommand) Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		// somebody got to it before we were able to, poor bastard
		return nil
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		// minus sign means we're talking about a PGID as opposed to a PID
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	return cmd.Process.Kill()
}

// PrepareForChildren sets Setpgid to true on the cmd, so that when we run it as a sideproject, we can kill its group rather than the process itself. This is because some commands, like `docker-compose logs` spawn multiple children processes, and killing the parent process isn't sufficient for killing those child processes. We set the group id here, and then in subprocess.go we check if the group id is set and if so, we kill the whole group rather than just the one process.
func (c *OSCommand) PrepareForChildren(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
