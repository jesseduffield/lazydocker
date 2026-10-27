package gui

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

// The side panels can show either the local docker resources or those of a
// swarm, chosen via the tabs of the projects window. Each swarm panel has its
// own view, placed in the same window as its local counterpart (e.g. swarm
// services in the services window, secrets and configs in windows of their
// own); the panels of the inactive mode are hidden
// via their Hide function, so the layout and focus code needs no special casing.

// swarm has no events for task state changes, so we poll
const swarmRefreshInterval = 2 * time.Second

func (gui *Gui) swarmSidePanels() []panels.ISideListPanel {
	return []panels.ISideListPanel{
		gui.Panels.SwarmStacks,
		gui.Panels.SwarmServices,
		gui.Panels.SwarmTasks,
		gui.Panels.SwarmNodes,
		gui.Panels.SwarmVolumes,
		gui.Panels.SwarmNetworks,
		gui.Panels.SwarmSecrets,
		gui.Panels.SwarmConfigs,
	}
}

func (gui *Gui) isSwarmMode() bool {
	return gui.State.Swarm != nil && gui.State.Swarm.Enabled()
}

// the swarms that get a tab in the projects window
func (gui *Gui) visibleSwarms() []*commands.SwarmCommand {
	return lo.Filter(gui.Swarms, func(swarm *commands.SwarmCommand, _ int) bool { return swarm.Enabled() })
}

// setSwarm switches the side panels to the given swarm, or to the local docker
// resources if swarm is nil
func (gui *Gui) setSwarm(swarm *commands.SwarmCommand) error {
	if swarm != gui.State.Swarm {
		gui.State.Swarm = swarm
		if swarm != nil {
			// rather than the previous swarm's resources, show that we're loading
			gui.swarmLoaded.Store(false)
			gui.setSwarmState(&commands.SwarmState{})
			go gui.animateSwarmConnecting(swarm)
			go func() { _ = gui.refreshSwarm() }()
		}
	}

	gui.resetMainView()
	if gui.isSwarmMode() {
		return gui.switchFocus(gui.Views.SwarmStacks)
	}
	return gui.switchFocus(gui.Views.Project)
}

// selectModeTab switches to a tab of the projects window: local, then one per
// swarm. idx may be one past either end, wrapping around.
func (gui *Gui) selectModeTab(idx int) error {
	tabs := append([]*commands.SwarmCommand{nil}, gui.visibleSwarms()...)
	return gui.setSwarm(tabs[(idx+len(tabs))%len(tabs)])
}

func (gui *Gui) modeTabIndex() int {
	return lo.IndexOf(gui.visibleSwarms(), gui.State.Swarm) + 1
}

// connecting to a remote swarm (especially over ssh) can take a few seconds, so
// until the first refresh is done we show a spinner in the status bar and in
// the (still empty) swarm panels
func (gui *Gui) animateSwarmConnecting(swarm *commands.SwarmCommand) {
	if !swarm.IsRemote() {
		return
	}
	_ = gui.WithWaitingStatus(fmt.Sprintf(gui.Tr.SwarmConnecting, swarm.Host), func() error {
		for !gui.swarmLoaded.Load() && gui.State.Swarm == swarm {
			for _, panel := range gui.swarmSidePanels() {
				_ = panel.RerenderList()
			}
			time.Sleep(100 * time.Millisecond)
		}
		return nil
	})
}

// the Local/<swarm> tabs are only shown if there is a swarm to manage
func (gui *Gui) updateModeTabs() {
	swarms := gui.visibleSwarms()
	var tabs []string
	if len(swarms) > 0 {
		tabs = append([]string{gui.Tr.LocalTitle}, lo.Map(swarms, func(swarm *commands.SwarmCommand, _ int) string { return swarm.Name })...)
	}
	gui.Views.Project.Tabs = tabs
	gui.Views.SwarmStacks.Tabs = tabs
	gui.Views.SwarmStacks.TabIndex = gui.modeTabIndex()

	gui.Views.SwarmStacks.Footer = ""
	if gui.State.Swarm != nil {
		gui.Views.SwarmStacks.Footer = gui.State.Swarm.Host
	}
}

func (gui *Gui) selectedSwarmStack() *commands.SwarmStack {
	stack, err := gui.Panels.SwarmStacks.GetSelectedItem()
	if err != nil {
		return nil
	}
	return stack
}

func (gui *Gui) isInSelectedSwarmStack(stack string) bool {
	selected := gui.selectedSwarmStack()
	return selected == nil || selected.IsAll() || selected.Name == stack
}

// refreshSwarm loads the resources of the swarm we're showing
func (gui *Gui) refreshSwarm() error {
	swarm := gui.State.Swarm

	// whether the default docker host is a swarm manager decides whether it has
	// a tab, so we check even when we're not showing it
	if defaultSwarm := gui.Swarms[0]; defaultSwarm != swarm {
		defaultSwarm.UpdateIsManager()
	}
	if swarm == nil {
		return nil
	}

	state, err := swarm.Refresh()
	if err != nil {
		gui.Log.Warn(err)
	}
	if gui.State.Swarm != swarm {
		// we switched swarms while loading
		return nil
	}
	gui.swarmLoaded.Store(true)

	// e.g. the node we're connected to left the swarm
	if !swarm.Enabled() {
		gui.Update(func() error {
			gui.State.Swarm = nil
			return gui.switchFocus(gui.Views.Containers)
		})
	}

	gui.setSwarmState(state)
	return nil
}

func (gui *Gui) setSwarmState(state *commands.SwarmState) {
	// keep the selected stack selected, even if stacks came or went
	selected := gui.selectedSwarmStack()
	gui.Panels.SwarmStacks.SetItems(state.Stacks)
	if selected != nil {
		_, idx, _ := lo.FindIndexOf(gui.Panels.SwarmStacks.List.GetItems(), func(s *commands.SwarmStack) bool { return s.Name == selected.Name })
		gui.Panels.SwarmStacks.SetSelectedLineIdx(max(idx, 0))
	}
	gui.Panels.SwarmServices.SetItems(state.Services)
	gui.Panels.SwarmTasks.SetItems(state.Tasks)
	gui.Panels.SwarmNodes.SetItems(state.Nodes)
	gui.Panels.SwarmVolumes.SetItems(state.Volumes)
	gui.Panels.SwarmNetworks.SetItems(state.Networks)
	gui.Panels.SwarmSecrets.SetItems(state.Secrets)
	gui.Panels.SwarmConfigs.SetItems(state.Configs)

	for _, panel := range gui.swarmSidePanels() {
		if err := panel.RerenderList(); err != nil {
			gui.Log.Error(err)
		}
	}
}

type swarmPanelOpts[T any] struct {
	view     *gocui.View
	noItems  string
	getID    func(T) string
	cells    func(T) []string
	less     func(a, b T) bool
	filter   func(T) bool
	onSelect func(T) error
	tabs     []swarmTab[T]
}

type swarmTab[T any] struct {
	key   string
	title string
	// live tabs are re-rendered every second with the latest version of the
	// item (items are re-created on every refresh), keeping the scroll position
	live func(T) string
	// for tabs that aren't re-rendered, e.g. logs
	render func(T) tasks.TaskFunc
}

func newSwarmPanel[T comparable](gui *Gui, opts swarmPanelOpts[T]) *panels.SideListPanel[T] {
	panel := &panels.SideListPanel[T]{
		ListPanel: panels.ListPanel[T]{
			List: panels.NewFilteredList[T](),
			View: opts.view,
		},
		NoItemsMessage: opts.noItems,
		Gui:            gui.intoInterface(),
		Filter:         opts.filter,
		Sort:           opts.less,
		GetTableCells:  opts.cells,
		OnSelect:       opts.onSelect,
		Hide:           func() bool { return !gui.isSwarmMode() },
	}

	latest := func(item T) T {
		found, ok := lo.Find(panel.List.GetAllItems(), func(other T) bool { return opts.getID(other) == opts.getID(item) })
		if !ok {
			return item
		}
		return found
	}

	mainTabs := lo.Map(opts.tabs, func(tab swarmTab[T], _ int) panels.MainTab[T] {
		render := tab.render
		if tab.live != nil {
			render = func(item T) tasks.TaskFunc {
				return gui.NewTickerTask(TickerTaskOpts{
					Func: func(ctx context.Context, notifyStopped chan struct{}) {
						gui.reRenderStringMain(tab.live(latest(item)))
					},
					Duration: time.Second,
					Before:   func(ctx context.Context) { gui.clearMainView() },
					Wrap:     gui.Config.UserConfig.Gui.WrapMainPanel,
				})
			}
		}
		return panels.MainTab[T]{Key: tab.key, Title: tab.title, Render: render}
	})

	panel.ContextState = &panels.ContextState[T]{
		GetMainTabs:            func() []panels.MainTab[T] { return mainTabs },
		GetItemContextCacheKey: func(item T) string { return opts.view.Name() + "-" + opts.getID(item) },
	}

	// show why the list is empty: still connecting, or e.g. the remote swarm
	// is unreachable
	panel.OnRerender = func() error {
		swarm := gui.State.Swarm
		opts.view.Wrap = false
		switch {
		case swarm == nil || panel.List.Len() > 0:
		case !gui.swarmLoaded.Load():
			fmt.Fprint(opts.view, utils.ColoredString(utils.Loader()+" "+gui.Tr.SwarmConnectingPanel, color.FgCyan))
		case swarm.LastError() != "":
			opts.view.Wrap = true
			fmt.Fprint(opts.view, utils.ColoredString(swarm.LastError(), color.FgRed))
		}
		return nil
	}

	return panel
}

func swarmYaml(obj interface{}) string {
	data, err := utils.MarshalIntoYaml(obj)
	if err != nil {
		return err.Error()
	}
	return utils.ColoredYamlString(string(data))
}

func swarmTable[T any](items []T, cells func(T) []string) string {
	table, err := utils.RenderTable(lo.Map(items, func(item T, _ int) []string { return cells(item) }))
	if err != nil {
		return err.Error()
	}
	return table
}

// runSwarmAction runs f in the background with a waiting status, shows any
// error, and refreshes the swarm panels.
func (gui *Gui) runSwarmAction(status string, f func() error) error {
	return gui.WithWaitingStatus(status, func() error {
		err := f()
		if refreshErr := gui.refreshSwarm(); refreshErr != nil {
			gui.Log.Error(refreshErr)
		}
		if err != nil {
			return gui.createErrorPanel(err.Error())
		}
		return nil
	})
}

func (gui *Gui) confirmSwarmAction(prompt string, status string, f func() error) error {
	return gui.createConfirmationPanel(gui.Tr.Confirm, prompt, func(*gocui.Gui, *gocui.View) error {
		return gui.runSwarmAction(status, f)
	}, nil)
}

// createSwarmPrompt opens a prompt pre-filled with initialValue. Empty input is
// treated as cancelling.
func (gui *Gui) createSwarmPrompt(title string, initialValue string, onConfirm func(string) error) error {
	if err := gui.createPromptPanel(title, func(g *gocui.Gui, v *gocui.View) error {
		if input := gui.trimmedContent(v); input != "" {
			return onConfirm(input)
		}
		return nil
	}); err != nil {
		return err
	}

	gui.Views.Confirmation.TextArea.TypeString(initialValue)
	gui.Views.Confirmation.RenderTextArea()
	return nil
}

func (gui *Gui) swarmMenu(title string, items ...*types.MenuItem) error {
	return gui.Menu(CreateMenuOptions{Title: title, Items: items})
}

func swarmMenuItem(label string, onPress func() error) *types.MenuItem {
	return &types.MenuItem{LabelColumns: []string{label}, OnPress: onPress}
}

// renderSwarmLogs streams service or task logs to the main view
func (gui *Gui) renderSwarmLogs(id string, isTask bool, tty bool) tasks.TaskFunc {
	return gui.NewTask(TaskOpts{
		Autoscroll: true,
		Wrap:       gui.Config.UserConfig.Gui.WrapMainPanel,
		Func: func(ctx context.Context) {
			gui.clearMainView()
			if err := gui.writeSwarmLogs(ctx, id, isTask, tty); err != nil && ctx.Err() == nil {
				fmt.Fprintln(gui.Views.Main, utils.ColoredString(err.Error(), color.FgRed))
			}
		},
	})
}

func (gui *Gui) writeSwarmLogs(ctx context.Context, id string, isTask bool, tty bool) error {
	reader, err := gui.State.Swarm.Logs(ctx, id, isTask)
	if err != nil {
		return err
	}
	defer reader.Close()

	writer := &swarmLogWriter{
		out:        gui.Views.Main,
		timestamps: gui.Config.UserConfig.Logs.Timestamps,
	}
	// a single task's lines don't need telling apart
	if !isTask {
		writer.label = gui.swarmLogLabel
	}
	defer writer.Flush()

	if tty {
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

// swarmLogsCmd follows logs full-screen via the docker CLI
func (gui *Gui) swarmLogsCmd(target string) error {
	args := []string{"service", "logs", "--follow"}
	if tail := gui.Config.UserConfig.Logs.Tail; tail != "" {
		args = append(args, "--tail", tail)
	}
	return gui.runSubprocess(gui.State.Swarm.NewCmd(append(args, target)...))
}
