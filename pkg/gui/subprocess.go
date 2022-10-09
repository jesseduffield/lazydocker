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
				// preparing the state for when we return
				gui.pushView(gui.currentViewName())
				// giving goEvery goroutines time to finish
				gui.State.SessionIndex++

				if err := gui.runCommand(); err != nil {
					return err
				}

				// pop here so we don't stack up view names
				gui.popView()
				// ensuring we render e.g. the logs of the currently selected item upon return
				gui.State.Panels.Main.ObjectKey = ""
			} else {
				return err
			}
		}
	}
	return nil
}

func (gui *Gui) runCommand() error {
	gui.SubProcess.Stdout = os.Stdout
	gui.SubProcess.Stderr = os.Stdout
	gui.SubProcess.Stdin = os.Stdin

	stop := make(chan os.Signal, 1)
	defer signal.Stop(stop)

	go func() {
		signal.Notify(stop, os.Interrupt)
		<-stop

		if err := gui.OSCommand.Kill(gui.SubProcess); err != nil {
			gui.Log.Error(err)
		}
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

	gui.promptToReturn()

	return nil
}
