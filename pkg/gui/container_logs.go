package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
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

	if gui.Config.UserConfig.Logs.PipeThrough != "" {
		return gui.pipeContainerLogs(ctr, ctx, writer, readCloser)
	}

	return copyContainerLogs(ctr, writer, readCloser)
}

// copyContainerLogs writes the container's raw log stream to dst, merging stdout and stderr
func copyContainerLogs(ctr *commands.Container, dst io.Writer, src io.Reader) error {
	if ctr.Details.Config.Tty {
		_, err := io.Copy(dst, src)
		return err
	}

	_, err := stdcopy.StdCopy(dst, dst, src)
	return err
}

// pipeContainerLogs runs the user's logs.pipeThrough command as a child process, feeds it the container's logs and
// writes its output to dst instead. It waits for the command to exit so the process is reaped and all its output has
// been written before returning
func (gui *Gui) pipeContainerLogs(ctr *commands.Container, ctx context.Context, dst io.Writer, src io.Reader) error {
	cmd := gui.OSCommand.RunCustomCommandContext(ctx, gui.Config.UserConfig.Logs.PipeThrough)
	cmd.Stdout = dst
	cmd.Stderr = dst

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	err = copyContainerLogs(ctr, stdin, src)

	// stdin must be closed before waiting, so the command sees EOF, flushes and exits
	return errors.Join(err, stdin.Close(), cmd.Wait())
}
