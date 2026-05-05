package commands

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

const clipboardTimeout = 1500 * time.Millisecond

// CopyToClipboard copies the given string to the system clipboard.
func (c *OSCommand) CopyToClipboard(s string) error {
	if s == "" {
		return fmt.Errorf("nothing to copy")
	}

	switch c.Platform.os {
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "pbcopy")
		return runCmdWithInput(cmd, s)
	case "linux":
		if p, err := exec.LookPath("wl-copy"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, p)
			return runCmdWithInput(cmd, s)
		}
		if p, err := exec.LookPath("xclip"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, p, "-selection", "clipboard")
			return runCmdWithInput(cmd, s)
		}
		if p, err := exec.LookPath("xsel"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, p, "--clipboard", "--input")
			return runCmdWithInput(cmd, s)
		}
		return fmt.Errorf("no clipboard utility found: try installing wl-clipboard (wl-copy) or xclip/xsel")
	case "windows":
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Set-Clipboard")
		return runCmdWithInput(cmd, s)
	default:
		return fmt.Errorf("unsupported platform: %s", c.Platform.os)
	}
}

func runCmdWithInput(cmd *exec.Cmd, s string) error {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}

	if _, err := io.WriteString(stdin, s); err != nil {
		_ = cmd.Process.Kill()
		_ = stdin.Close()
		return err
	}

	if err := stdin.Close(); err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	return cmd.Wait()
}
