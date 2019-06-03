package gui

import (
	"bufio"
	"fmt"
	"io"
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
	gui.SubProcess.Stdout = os.Stdout
	gui.SubProcess.Stderr = os.Stdout
	// gui.SubProcess.Stdin = os.Stdin

	reader := bufio.NewReader(os.Stdin)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	go func() {
		<-c
		gui.SubProcess.Process.Kill()
	}()

	w, err := gui.SubProcess.StdinPipe()
	if err != nil {
		return err
	}

	go func() {
		for {
			input, err := reader.ReadByte()
			gui.Log.Warn("byte received")
			gui.Log.Warn(input)
			if input == 3 {
				gui.SubProcess.Process.Kill()
				break
			}
			if err != nil && err == io.EOF {
				gui.SubProcess.Process.Kill()
				break
			}
			w.Write([]byte{input})
		}
	}()

	fmt.Fprintf(os.Stdout, "\n%s\n\n", utils.ColoredString("+ "+strings.Join(gui.SubProcess.Args, " "), color.FgBlue))

	if err := gui.SubProcess.Run(); err != nil {
		// not handling the error explicitly because usually we're going to see it
		// in the output anyway
		gui.Log.Error(err)
	}

	gui.SubProcess.Stdout = ioutil.Discard
	gui.SubProcess.Stderr = ioutil.Discard
	gui.SubProcess.Stdin = nil
	gui.SubProcess = nil

	// fmt.Fprintf(os.Stdout, "\n%s", utils.ColoredString(gui.Tr.SLocalize("pressEnterToReturn"), color.FgGreen))

	// fmt.Scanln() // wait for enter press

	return nil
}
