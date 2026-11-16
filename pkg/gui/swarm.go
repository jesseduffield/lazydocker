package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
// swarm, chosen via the tabs of the projects window (see sideViewNames). Each
// swarm panel has a view of its own, shown in place of its local counterpart
// (swarm services where the services panel was, and so on; secrets and configs
// have no counterpart).

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

// localSwarm is the swarm of the docker host lazydocker is connected to, as
// opposed to the remote swarms of other docker contexts
func (gui *Gui) localSwarm() *commands.SwarmCommand {
	return gui.Swarms[0]
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
			gui.showSwarmConnecting(swarm)
			go gui.refreshSwarm()
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
	// like other clicks, a click on a tab mustn't take the focus from a popup
	if gui.popupPanelFocused() {
		return nil
	}

	tabs := append([]*commands.SwarmCommand{nil}, gui.visibleSwarms()...)
	return gui.setSwarm(tabs[(idx+len(tabs))%len(tabs)])
}

func (gui *Gui) modeTabIndex() int {
	return lo.IndexOf(gui.visibleSwarms(), gui.State.Swarm) + 1
}

func (gui *Gui) handlePreviousSwarmTab(g *gocui.Gui, v *gocui.View) error {
	return gui.selectModeTab(gui.modeTabIndex() - 1)
}

func (gui *Gui) handleNextSwarmTab(g *gocui.Gui, v *gocui.View) error {
	return gui.selectModeTab(gui.modeTabIndex() + 1)
}

// showSwarmConnecting empties the swarm panels, which then say that we're
// connecting until the first refresh is done (rather than showing the previous
// swarm's resources). Connecting to a remote swarm (especially over ssh) can
// take a few seconds, so meanwhile the status bar shows a spinner too.
func (gui *Gui) showSwarmConnecting(swarm *commands.SwarmCommand) {
	gui.swarmLoaded.Store(false)
	gui.setSwarmState(&commands.SwarmState{})
	if !swarm.IsRemote() {
		return
	}
	_ = gui.WithWaitingStatus(fmt.Sprintf(gui.Tr.SwarmConnecting, swarm.Host), func() error {
		for !gui.swarmLoaded.Load() && gui.State.Swarm == swarm {
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

// poll calls f every interval. Unlike goEvery it waits for f to finish before
// timing the next call, so that a slow (e.g. remote) docker host isn't sent
// requests back to back.
func (gui *Gui) poll(interval time.Duration, f func()) {
	for {
		if !gui.PauseBackgroundThreads {
			f()
		}
		time.Sleep(interval)
	}
}

// checkLocalSwarm checks whether the local docker host is a swarm manager,
// which decides whether it gets a tab. That can change while lazydocker runs
// (docker swarm init/leave), so we check even when we're not showing it. While
// we are, refreshSwarm checks anyway.
func (gui *Gui) checkLocalSwarm() {
	local := gui.localSwarm()
	if local == gui.State.Swarm {
		return
	}

	local.UpdateIsManager()
	gui.Update(func() error {
		// after `docker swarm leave` the projects panel may be gone, if it was
		// only there for the swarm's tab
		if gui.currentViewName() == "project" && gui.Panels.Projects.IsHidden() {
			return gui.switchFocus(gui.Views.Containers)
		}
		return nil
	})
}

// refreshSwarm loads the resources of the swarm we're showing, if any. Errors
// are shown in the panels rather than returned.
func (gui *Gui) refreshSwarm() {
	swarm := gui.State.Swarm
	if swarm == nil {
		return
	}

	state, err := swarm.Refresh()
	if err != nil {
		gui.Log.Warn(err)
		state = &commands.SwarmState{}
	}

	// on the main loop, so that we can't switch swarms in between
	gui.Update(func() error {
		if gui.State.Swarm != swarm {
			// we switched swarms while loading
			return nil
		}
		gui.swarmLoaded.Store(true)
		gui.setSwarmState(state)

		// e.g. the node we're connected to left the swarm
		if !swarm.Enabled() {
			gui.State.Swarm = nil
			return gui.switchFocus(gui.Views.Containers)
		}
		return nil
	})
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
			fmt.Fprint(opts.view, utils.ColoredString(gui.Tr.SwarmConnectingPanel, color.FgCyan))
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

// trailingPadding matches the padding that a row's empty last cells (e.g. a
// task without an error) leave behind, before any colour codes
var trailingPadding = regexp.MustCompile(`(?m) +((?:\x1b\[[0-9;]*m)*)$`)

func swarmTable[T any](items []T, cells func(T) []string) string {
	table, err := utils.RenderTable(lo.Map(items, func(item T, _ int) []string { return cells(item) }))
	if err != nil {
		return err.Error()
	}
	// otherwise every row is as wide as the widest one, and in a narrow main
	// view they all wrap
	return trailingPadding.ReplaceAllString(table, "$1")
}

// runSwarmAction runs f in the background with a waiting status, shows any
// error, and refreshes the swarm panels.
func (gui *Gui) runSwarmAction(status string, f func() error) error {
	return gui.WithWaitingStatus(status, func() error {
		err := f()
		gui.refreshSwarm()
		return err
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

// expandHomeDir expands a leading ~/ in a path typed into a prompt, like a
// shell would (we don't run commands through one)
func expandHomeDir(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}

// swarmCreateMenu creates a secret or config, named and with its content
// either typed in or read from a local file
func (gui *Gui) swarmCreateMenu(title string, create func(name string, data []byte) error) error {
	createWith := func(name string, data []byte) error {
		return gui.runSwarmAction(gui.Tr.SwarmCreatingStatus, func() error { return create(name, data) })
	}

	fromValue := func() error {
		return gui.createSwarmPrompt(gui.Tr.SwarmNamePrompt, "", func(name string) error {
			return gui.createSwarmPrompt(gui.Tr.SwarmValuePrompt, "", func(value string) error {
				return createWith(name, []byte(value))
			})
		})
	}

	fromFile := func() error {
		return gui.createSwarmPrompt(gui.Tr.SwarmFilePathPrompt, "", func(path string) error {
			data, err := os.ReadFile(expandHomeDir(path))
			if err != nil {
				return gui.createErrorPanel(err.Error())
			}
			return gui.createSwarmPrompt(gui.Tr.SwarmNamePrompt, filepath.Base(path), func(name string) error {
				return createWith(name, data)
			})
		})
	}

	return gui.Menu(CreateMenuOptions{
		Title: title,
		Items: []*types.MenuItem{
			{Label: gui.Tr.SwarmCreateFromValue, OnPress: fromValue},
			{Label: gui.Tr.SwarmCreateFromFile, OnPress: fromFile},
		},
	})
}

// swarmAttachMenu picks a service that doesn't use the network/secret/config
// called name yet, and calls onSelect with it
func (gui *Gui) swarmAttachMenu(name string, usedBy []*commands.SwarmService, onSelect func(*commands.SwarmService) error) error {
	services := lo.Without(gui.Panels.SwarmServices.List.GetAllItems(), usedBy...)
	items := lo.Map(services, func(service *commands.SwarmService, _ int) *types.MenuItem {
		return &types.MenuItem{Label: service.Name, OnPress: func() error { return onSelect(service) }}
	})

	return gui.Menu(CreateMenuOptions{Title: fmt.Sprintf(gui.Tr.SwarmAttachTo, name), Items: items})
}

// swarmDetachMenu picks one of the services using the network/secret/config
// called name, and detaches it with detach
func (gui *Gui) swarmDetachMenu(name string, usedBy []*commands.SwarmService, detach func(*commands.SwarmService) error) error {
	items := lo.Map(usedBy, func(service *commands.SwarmService, _ int) *types.MenuItem {
		return &types.MenuItem{Label: service.Name, OnPress: func() error {
			return gui.runSwarmAction(gui.Tr.SwarmUpdatingStatus, func() error { return detach(service) })
		}}
	})

	return gui.Menu(CreateMenuOptions{Title: fmt.Sprintf(gui.Tr.SwarmDetachFrom, name), Items: items})
}
