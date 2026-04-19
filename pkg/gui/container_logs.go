package gui

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/fatih/color"
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

	var filterString string
	var hasFilter bool

	if gui.State.LogsFilter.active {
		filterString = gui.State.LogsFilter.needle
		hasFilter = filterString != ""
	}

	if ctr.Details.Config.Tty {
		if hasFilter {
			return gui.filterTTYLogs(readCloser, writer, filterString, ctx)
		}
		_, err = io.Copy(writer, readCloser)
		if err != nil {
			return err
		}
	} else {
		if hasFilter {
			return gui.filterNonTTYLogs(readCloser, writer, filterString, ctx)
		}
		_, err = stdcopy.StdCopy(writer, writer, readCloser)
		if err != nil {
			return err
		}
	}

	return nil
}

// filterTTYLogs filters TTY logs line by line based on the filter string
func (gui *Gui) filterTTYLogs(reader io.Reader, writer io.Writer, filter string, ctx context.Context) error {
	return streamFilterLines(reader, writer, filter, ctx)
}

// filterNonTTYLogs filters non-TTY logs (stdout/stderr) line by line
func (gui *Gui) filterNonTTYLogs(reader io.Reader, writer io.Writer, filter string, ctx context.Context) error {
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		defer pipeWriter.Close()

		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				pipeWriter.CloseWithError(ctx.Err())
			case <-done:
			}
		}()

		_, err := stdcopy.StdCopy(pipeWriter, pipeWriter, reader)
		close(done)

		if err != nil {
			pipeWriter.CloseWithError(err)
		}
	}()

	defer pipeReader.Close()
	return streamFilterLines(pipeReader, writer, filter, ctx)
}

func streamFilterLines(reader io.Reader, writer io.Writer, filter string, ctx context.Context) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4*1024), 10*1024*1024)

	// Pre-convert filter to bytes for faster comparison when filter is ASCII
	filterBytes := []byte(filter)
	isASCIIFilter := len(filterBytes) == len(filter)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Bytes()

		var shouldWrite bool
		if isASCIIFilter {
			shouldWrite = bytes.Contains(line, filterBytes)
		} else {
			// For non-ASCII filters, we need string conversion for proper UTF-8 handling
			shouldWrite = strings.Contains(string(line), filter)
		}

		if shouldWrite {
			if _, err := writer.Write(line); err != nil {
				return err
			}
			if _, err := writer.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}
