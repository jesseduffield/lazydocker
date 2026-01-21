package gui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func (gui *Gui) runSubprocess(cmd *exec.Cmd) error {
	return gui.runSubprocessWithMessage(cmd, "")
}

func (gui *Gui) runSubprocessWithMessage(cmd *exec.Cmd, msg string) error {
	gui.Mutexes.SubprocessMutex.Lock()
	defer gui.Mutexes.SubprocessMutex.Unlock()

	if err := gui.g.Suspend(); err != nil {
		return gui.createErrorPanel(err.Error())
	}

	gui.PauseBackgroundThreads = true

	err := gui.runCommand(cmd, msg)

	if err := gui.g.Resume(); err != nil {
		return gui.createErrorPanel(err.Error())
	}

	gui.PauseBackgroundThreads = false

	return err
}

func (gui *Gui) runCommandSilently(cmd *exec.Cmd) error {
	stop := make(chan os.Signal, 1)
	defer signal.Stop(stop)

	go func() {
		signal.Notify(stop, os.Interrupt)
		<-stop

		if err := gui.OSCommand.Kill(cmd); err != nil {
			gui.Log.Error(err)
		}
	}()

	return cmd.Run()
}

func (gui *Gui) runCommand(cmd *exec.Cmd, msg string) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	cmd.Stdin = os.Stdin

	stop := make(chan os.Signal, 1)
	defer signal.Stop(stop)

	go func() {
		signal.Notify(stop, os.Interrupt)
		<-stop

		if err := gui.OSCommand.Kill(cmd); err != nil {
			gui.Log.Error(err)
		}
	}()

	fmt.Fprintf(os.Stdout, "\n%s\n\n", utils.ColoredString("+ "+strings.Join(cmd.Args, " "), color.FgBlue))
	if msg != "" {
		fmt.Fprintf(os.Stdout, "\n%s\n\n", utils.ColoredString(msg, color.FgGreen))
	}
	err := cmd.Run()
	if err != nil {
		gui.Log.Error(err)
	}

	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	gui.promptToReturn()

	return err
}
