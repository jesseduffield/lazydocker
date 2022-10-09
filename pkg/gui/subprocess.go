package gui

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func (gui *Gui) runSubprocess(cmd *exec.Cmd) error {
	gui.Mutexes.SubprocessMutex.Lock()
	defer gui.Mutexes.SubprocessMutex.Unlock()

	if err := gui.g.Suspend(); err != nil {
		return gui.createErrorPanel(gui.g, err.Error())
	}

	gui.PauseBackgroundThreads = true

	cmdErr := gui.runSubprocess(cmd)

	if err := gui.g.Resume(); err != nil {
		return gui.createErrorPanel(gui.g, err.Error())
	}

	gui.PauseBackgroundThreads = false

	if cmdErr != nil {
		return gui.createErrorPanel(gui.g, cmdErr.Error())
	}

	return nil
}

func (gui *Gui) runCommand(cmd *exec.Cmd) error {
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

	if err := cmd.Run(); err != nil {
		// not handling the error explicitly because usually we're going to see it
		// in the output anyway
		gui.Log.Error(err)
	}

	cmd.Stdin = nil
	cmd.Stdout = ioutil.Discard
	cmd.Stderr = ioutil.Discard

	gui.promptToReturn()

	return nil
}
