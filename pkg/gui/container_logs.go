package gui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/christophe-duc/lazypodman/pkg/commands"
	"github.com/christophe-duc/lazypodman/pkg/tasks"
	"github.com/christophe-duc/lazypodman/pkg/utils"
	"github.com/fatih/color"
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
	// Build podman logs command
	args := []string{"logs", "--follow"}

	if gui.Config.UserConfig.Logs.Timestamps {
		args = append(args, "--timestamps")
	}
	if gui.Config.UserConfig.Logs.Since != "" {
		args = append(args, "--since", gui.Config.UserConfig.Logs.Since)
	}
	if gui.Config.UserConfig.Logs.Tail != "" {
		args = append(args, "--tail", gui.Config.UserConfig.Logs.Tail)
	}
	args = append(args, ctr.ID)

	cmd := ctr.OSCommand.NewCmd("podman", args...)
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Start(); err != nil {
		gui.Log.Error(err)
		return err
	}

	// Wait for context cancellation or command completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if err := ctr.OSCommand.Kill(cmd); err != nil {
			gui.Log.Warn(err)
		}
		return nil
	case err := <-done:
		return err
	}
}

// Pod logs rendering

func (gui *Gui) renderPodLogsToMain(pod *commands.Pod) tasks.TaskFunc {
	return gui.NewTickerTask(TickerTaskOpts{
		Func: func(ctx context.Context, notifyStopped chan struct{}) {
			gui.renderPodLogsToMainAux(pod, ctx, notifyStopped)
		},
		Duration:   time.Millisecond * 200,
		Before:     func(ctx context.Context) { gui.clearMainView() },
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Autoscroll: true,
	})
}

func (gui *Gui) renderPodLogsToMainAux(pod *commands.Pod, ctx context.Context, notifyStopped chan struct{}) {
	gui.clearMainView()
	defer func() {
		notifyStopped <- struct{}{}
	}()

	mainView := gui.Views.Main

	if err := gui.writePodLogs(pod, ctx, mainView); err != nil {
		gui.Log.Error(err)
	}

	// Wait for context cancellation
	<-ctx.Done()
}

func (gui *Gui) writePodLogs(pod *commands.Pod, ctx context.Context, writer io.Writer) error {
	// Build podman pod logs command
	// Note: --color is used to distinguish output from different containers in the pod
	args := []string{"pod", "logs", "--follow"}

	if gui.Config.UserConfig.Logs.Timestamps {
		args = append(args, "--timestamps")
	}
	if gui.Config.UserConfig.Logs.Since != "" {
		args = append(args, "--since", gui.Config.UserConfig.Logs.Since)
	}
	if gui.Config.UserConfig.Logs.Tail != "" {
		args = append(args, "--tail", gui.Config.UserConfig.Logs.Tail)
	}
	args = append(args, pod.Name)

	cmd := pod.OSCommand.NewCmd("podman", args...)
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Start(); err != nil {
		gui.Log.Error(err)
		return err
	}

	// Wait for context cancellation or command completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if err := pod.OSCommand.Kill(cmd); err != nil {
			gui.Log.Warn(err)
		}
		return nil
	case err := <-done:
		return err
	}
}
