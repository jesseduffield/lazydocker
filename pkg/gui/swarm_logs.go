package gui

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"strings"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

// swarmLogSource is a service or a task
type swarmLogSource interface {
	Logs(ctx context.Context) (io.ReadCloser, error)
	IsTTY() bool
}

// renderSwarmLogs streams the logs of a service or task to the main view
func (gui *Gui) renderSwarmLogs(source swarmLogSource, label func(taskID string, nodeID string) string) tasks.TaskFunc {
	return gui.NewTask(TaskOpts{
		Autoscroll: true,
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Func: func(ctx context.Context) {
			gui.clearMainView()
			if err := gui.writeSwarmLogs(ctx, gui.Views.Main, source, label); err != nil && ctx.Err() == nil {
				fmt.Fprintln(gui.Views.Main, utils.ColoredString(err.Error(), color.FgRed))
			}
		},
	})
}

// writeSwarmLogs writes the logs of a service or task to out, prefixing each
// line with label(task, node) unless label is nil
func (gui *Gui) writeSwarmLogs(ctx context.Context, out io.Writer, source swarmLogSource, label func(taskID string, nodeID string) string) error {
	reader, err := source.Logs(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()

	writer := &swarmLogWriter{
		out:        out,
		timestamps: gui.Config.UserConfig.Logs.Timestamps,
		label:      label,
	}
	defer writer.Flush()

	if source.IsTTY() {
		_, err = io.Copy(writer, reader)
	} else {
		_, err = stdcopy.StdCopy(writer, writer, reader)
	}
	return err
}

// swarmLogLabel returns e.g. "web.2@node-1" for a log line's task and node
func (gui *Gui) swarmLogLabel(taskID string, nodeID string) string {
	if task, ok := lo.Find(gui.Panels.SwarmTasks.List.GetAllItems(), func(t *commands.SwarmTask) bool { return t.ID == taskID }); ok {
		return task.Name() + "@" + task.NodeName
	}
	return taskID + "@" + nodeID
}

// swarmLogWriter turns swarm log lines, which (because we request 'details')
// look like:
//
//	[timestamp ]com.docker.swarm.node.id=..,com.docker.swarm.service.id=..,com.docker.swarm.task.id=.. message
//
// into:
//
//	[timestamp ]web.1@node-1 | message
//
// If label is nil, the details are dropped without adding a prefix.
type swarmLogWriter struct {
	out        io.Writer
	timestamps bool
	label      func(taskID string, nodeID string) string
	buf        []byte
}

var swarmLogColors = []color.Attribute{color.FgCyan, color.FgYellow, color.FgGreen, color.FgMagenta, color.FgBlue}

func (w *swarmLogWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx == -1 {
			return len(p), nil
		}
		w.writeLine(string(w.buf[:idx]))
		w.buf = w.buf[idx+1:]
	}
}

func (w *swarmLogWriter) Flush() {
	if len(w.buf) > 0 {
		w.writeLine(string(w.buf))
		w.buf = nil
	}
}

func (w *swarmLogWriter) writeLine(line string) {
	timestamp := ""
	if w.timestamps {
		if ts, rest, ok := strings.Cut(line, " "); ok {
			timestamp, line = ts+" ", rest
		}
	}

	prefix := ""
	if detailsStr, message, _ := strings.Cut(line, " "); strings.Contains(detailsStr, "com.docker.swarm.") {
		line = message
		if w.label != nil {
			details := map[string]string{}
			for _, component := range strings.Split(detailsStr, ",") {
				key, value, _ := strings.Cut(component, "=")
				details[key] = value
			}
			label := w.label(details["com.docker.swarm.task.id"], details["com.docker.swarm.node.id"])
			hash := fnv.New32a()
			_, _ = hash.Write([]byte(label))
			prefix = utils.ColoredString(label+" | ", swarmLogColors[hash.Sum32()%uint32(len(swarmLogColors))])
		}
	}

	fmt.Fprintf(w.out, "%s%s%s\n", timestamp, prefix, line)
}
