package gui

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// RunWithSubprocesses loops, instantiating a new gocui.Gui with each iteration
// if the error returned from a run is a ErrSubProcess, it runs the subprocess
// otherwise it handles the error, possibly by quitting the application
func (gui *Gui) RunWithSubprocesses() error {
	for {
		if err := gui.Run(); err != nil {
			if err == gocui.ErrQuit {
				break
			} else if err == gui.Errors.ErrSubProcess {
				if err := gui.runCommand(); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}
	return nil
}

func (gui *Gui) runCommand() error {
	gui.State.PreviousView = gui.currentViewName()

	gui.SubProcess.Stdout = os.Stdout
	gui.SubProcess.Stderr = os.Stdout
	gui.SubProcess.Stdin = os.Stdin

	c := make(chan os.Signal, 1)

	go func() {
		signal.Notify(c, os.Interrupt)
		<-c
		signal.Stop(c)

		gui.SubProcess.Process.Kill()
	}()

	fmt.Fprintf(os.Stdout, "\n%s\n\n", utils.ColoredString("+ "+strings.Join(gui.SubProcess.Args, " "), color.FgBlue))

	if err := gui.SubProcess.Run(); err != nil {
		// not handling the error explicitly because usually we're going to see it
		// in the output anyway
		gui.Log.Error(err)
	}

	gui.SubProcess.Stdin = nil
	gui.SubProcess.Stdout = ioutil.Discard
	gui.SubProcess.Stderr = ioutil.Discard
	gui.SubProcess = nil

	signal.Stop(c)

	fmt.Fprintf(os.Stdout, "\n%s", utils.ColoredString(gui.Tr.PressEnterToReturn, color.FgGreen))

	fmt.Scanln() // wait for enter press

	return nil
}
