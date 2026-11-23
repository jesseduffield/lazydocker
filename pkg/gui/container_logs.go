package gui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func (gui *Gui) renderContainerLogsToMain(container *commands.Container) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			gui.renderContainerLogsToMainAux(container, ctx, notifyStopped)
		},
		Duration: time.Millisecond * 200,
		// TODO: see why this isn't working (when switching from Top tab to Logs tab in the services panel, the tops tab's content isn't removed)
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Autoscroll: true,
	})
}

func (gui *Gui) renderContainerLogsToMainAux(container *commands.Container, ctx context.Context, notifyStopped chan struct{}) {
	gui.clearMainView()
	defer func() {
		notifyStopped <- struct{}{}
	}()

	mainView := gui.Views.Main

	if err := gui.writeContainerLogs(container, ctx, mainView); err != nil {
		gui.Log.Error(err)
	}

	// if we are here because the task has been stopped, we should return
	// if we are here then the container must have exited, meaning we should wait until it's back again before
	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := container.Inspect()
			if err != nil {
				// if we get an error, then the container has probably been removed so we'll get out of here
				gui.Log.Error(err)
				return
			}
			if result.State.Running {
				return
			}
		}
	}
}

func (gui *Gui) renderLogsToStdout(container *commands.Container) {
	stop := make(chan os.Signal, 1)
	defer signal.Stop(stop)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		signal.Notify(stop, os.Interrupt)
		<-stop
		cancel()
	}()

	if err := gui.g.Suspend(); err != nil {
		gui.Log.Error(err)
		return
	}

	defer func() {
		if err := gui.g.Resume(); err != nil {
			gui.Log.Error(err)
		}
	}()

	if err := gui.writeContainerLogs(container, ctx, os.Stdout); err != nil {
		gui.Log.Error(err)
		return
	}

	gui.promptToReturn()
}

func (gui *Gui) promptToReturn() {
	if !gui.Config.UserConfig.Gui.ReturnImmediately {
		fmt.Fprintf(os.Stdout, "\n\n%s", utils.ColoredString(gui.Tr.PressEnterToReturn, color.FgGreen))

		// wait for enter press
		if _, err := fmt.Scanln(); err != nil {
			gui.Log.Error(err)
		}
	}
}

func (gui *Gui) writeContainerLogs(ctr *commands.Container, ctx context.Context, writer io.Writer) error {
	readCloser, err := gui.DockerCommand.Client.ContainerLogs(ctx, ctr.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: gui.Config.UserConfig.Logs.Timestamps,
		Since:      gui.Config.UserConfig.Logs.Since,
		Tail:       gui.Config.UserConfig.Logs.Tail,
		Follow:     true,
	})
	if err != nil {
		gui.Log.Error(err)
		return err
	}
	defer readCloser.Close()

	if !ctr.DetailsLoaded() {
		// loop until the details load or context is cancelled, using timer
		ticker := time.NewTicker(time.Millisecond * 100)
		defer ticker.Stop()
	outer:
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if ctr.DetailsLoaded() {
					break outer
				}
			}
		}
	}

	stdoutWriter := NewLogWriter(writer, false)
	stderrWriter := NewLogWriter(writer, true)
	defer stdoutWriter.Close()
	defer stderrWriter.Close()

	if ctr.Details.Config.Tty {
		_, err = io.Copy(stdoutWriter, readCloser)
		if err != nil {
			return err
		}
	} else {
		_, err = stdcopy.StdCopy(stdoutWriter, stderrWriter, readCloser)
		if err != nil {
			return err
		}
	}

	return nil
}

type LogWriter struct {
	writer     io.Writer
	lineBuffer []byte
	isStderr   bool
}

func NewLogWriter(writer io.Writer, isStderr bool) *LogWriter {
	return &LogWriter{
		writer:     writer,
		lineBuffer: make([]byte, 0),
		isStderr:   isStderr,
	}
}

func (lw *LogWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	lw.lineBuffer = append(lw.lineBuffer, p...)

	for {
		idx := bytes.IndexByte(lw.lineBuffer, '\n')
		if idx == -1 {
			break
		}

		line := string(lw.lineBuffer[:idx])
		lw.lineBuffer = lw.lineBuffer[idx+1:]

		colorised := utils.ColoriseLog(line)
		if lw.isStderr {
			colorised = "\x1b[31;1m" + colorised + "\x1b[0m"
		}

		_, err = fmt.Fprintln(lw.writer, colorised)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (lw *LogWriter) Close() error {
	if len(lw.lineBuffer) > 0 {
		line := string(lw.lineBuffer)
		colorised := utils.ColoriseLog(line)
		if lw.isStderr {
			colorised = "\x1b[31;1m" + colorised + "\x1b[0m"
		}
		_, err := fmt.Fprint(lw.writer, colorised)
		return err
	}
	return nil
}

func (gui *Gui) handleContainerViewLogsExternal(g *gocui.Gui, v *gocui.View) error {
	ctr, err := gui.Panels.Containers.GetSelectedItem()
	if err != nil {
		return nil
	}
	return gui.handleViewLogsExternal(ctr)
}

func (gui *Gui) handleMainViewLogsExternal(g *gocui.Gui, v *gocui.View) error {
	currentSideViewName := gui.currentSideViewName()
	if currentSideViewName == "containers" {
		return gui.handleContainerViewLogsExternal(g, v)
	} else if currentSideViewName == "services" {
		return gui.handleServiceViewLogsExternal(g, v)
	}
	return nil
}

func (gui *Gui) handleViewLogsExternal(container *commands.Container) error {
	pager := gui.Config.UserConfig.Logs.Pager
	if pager == "" {
		return gui.createErrorPanel("No external pager configured. Please set 'logs.pager' in config.yml")
	}

	tmpFile, err := os.CreateTemp("", "lazydocker-*.log")
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	stop := make(chan os.Signal, 1)
	defer signal.Stop(stop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		signal.Notify(stop, os.Interrupt)
		<-stop
		cancel()
	}()

	// Start writing container logs to the temporary file in background
	go func() {
		_ = gui.writeContainerLogs(container, ctx, tmpFile)
	}()

	// Wait briefly for some logs to be written so the pager has content on start
	time.Sleep(200 * time.Millisecond)

	if err := gui.g.Suspend(); err != nil {
		gui.Log.Error(err)
		return err
	}

	defer func() {
		if err := gui.g.Resume(); err != nil {
			gui.Log.Error(err)
		}
	}()

	var cmdStr string
	if strings.Contains(pager, "{{filename}}") {
		cmdStr = strings.ReplaceAll(pager, "{{filename}}", tmpPath)
	} else {
		cmdStr = pager + " " + tmpPath
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	_ = cmd.Run()
	return nil
}
