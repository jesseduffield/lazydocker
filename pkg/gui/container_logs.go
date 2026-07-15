package gui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
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

// containerLogsTailPresets are the values we cycle through when the user asks to
// change how many lines of logs are displayed. An empty string means 'show all logs'.
var containerLogsTailPresets = []string{"", "50", "100", "200", "500"}

func containerLogsTailLabel(tail string) string {
	if tail == "" {
		return "all"
	}

	return tail
}

// cycleContainerLogsTail moves to the next tail preset (wrapping back to the start)
// and forces the logs tab to re-render with the new value.
func (gui *Gui) cycleContainerLogsTail(g *gocui.Gui, v *gocui.View) error {
	currentIdx := 0
	for i, preset := range containerLogsTailPresets {
		if preset == gui.State.ContainerLogsTail {
			currentIdx = i
			break
		}
	}

	gui.State.ContainerLogsTail = containerLogsTailPresets[(currentIdx+1)%len(containerLogsTailPresets)]

	return gui.Panels.Containers.HandleSelect()
}

// tailLimitingWriter wraps a writer and re-renders it on every line so that it only
// ever displays the last maxLines lines, keeping a true sliding window rather than
// just applying `--tail` to the initial snapshot and then growing forever as new
// lines are followed in.
type tailLimitingWriter struct {
	view     *gocui.View
	maxLines int
	lines    []string
	partial  string
}

func newTailLimitingWriter(view *gocui.View, maxLines int) *tailLimitingWriter {
	return &tailLimitingWriter{view: view, maxLines: maxLines}
}

func (w *tailLimitingWriter) Write(p []byte) (int, error) {
	w.partial += string(p)
	split := strings.Split(w.partial, "\n")
	w.partial = split[len(split)-1]
	newLines := split[:len(split)-1]

	if len(newLines) == 0 {
		return len(p), nil
	}

	w.lines = append(w.lines, newLines...)
	if excess := len(w.lines) - w.maxLines; excess > 0 {
		w.lines = w.lines[excess:]
	}

	w.view.Clear()
	fmt.Fprint(w.view, strings.Join(w.lines, "\n")+"\n")

	return len(p), nil
}

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
	tail := gui.State.ContainerLogsTail
	mainView.Subtitle = fmt.Sprintf("tail: %s (%s to cycle)", containerLogsTailLabel(tail), "t")

	var writer io.Writer = mainView
	if maxLines, err := strconv.Atoi(tail); err == nil && maxLines > 0 {
		// keep re-truncating to the chosen number of lines as new ones stream in,
		// instead of just limiting the initial snapshot and growing unbounded after that
		writer = newTailLimitingWriter(mainView, maxLines)
	}

	if err := gui.writeContainerLogs(container, ctx, writer); err != nil {
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
		Tail:       gui.State.ContainerLogsTail,
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

	if ctr.Details.Config.Tty {
		_, err = io.Copy(writer, readCloser)
		if err != nil {
			return err
		}
	} else {
		_, err = stdcopy.StdCopy(writer, writer, readCloser)
		if err != nil {
			return err
		}
	}

	return nil
}
