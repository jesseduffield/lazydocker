package gui

import (
	"context"
	"os"
	"strings"
	"time"

	throttle "github.com/boz/go-throttle"
	"github.com/jesseduffield/gocui"
	lcUtils "github.com/jesseduffield/lazycore/pkg/utils"
	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/jesseduffield/lazycontainer/pkg/config"
	"github.com/jesseduffield/lazycontainer/pkg/gui/panels"
	"github.com/jesseduffield/lazycontainer/pkg/gui/types"
	"github.com/jesseduffield/lazycontainer/pkg/i18n"
	"github.com/jesseduffield/lazycontainer/pkg/tasks"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
)

type Gui struct {
	g               *gocui.Gui
	Log             *logrus.Entry
	ContainerCmd    *commands.ContainerCommand
	OSCommand       *commands.OSCommand
	State           guiState
	Config          *config.AppConfig
	Tr              *i18n.TranslationSet
	statusManager   *statusManager
	taskManager     *tasks.TaskManager
	ErrorChan       chan error
	Views           Views

	PauseBackgroundThreads bool

	Mutexes

	Panels Panels
}

type Panels struct {
	Containers *panels.SideListPanel[*commands.Container]
	Images     *panels.SideListPanel[*commands.Image]
	Volumes    *panels.SideListPanel[*commands.Volume]
	Networks   *panels.SideListPanel[*commands.Network]
	Menu       *panels.SideListPanel[*types.MenuItem]
}

type Mutexes struct {
	SubprocessMutex deadlock.Mutex
	ViewStackMutex  deadlock.Mutex
}

type mainPanelState struct {
	ObjectKey string
}

type panelStates struct {
	Main *mainPanelState
}

type guiState struct {
	ViewStack        []string
	Platform         commands.Platform
	Panels           *panelStates
	SubProcessOutput string
	Stats            map[string]commands.ContainerStats

	ShowExitedContainers bool

	ScreenMode WindowMaximisation

	Filter filterState
}

type filterState struct {
	active bool
	panel  panels.ISideListPanel
	needle string
}

type WindowMaximisation int

const (
	SCREEN_NORMAL WindowMaximisation = iota
	SCREEN_HALF
	SCREEN_FULL
)

func getScreenMode(config *config.AppConfig) WindowMaximisation {
	switch config.UserConfig.Gui.ScreenMode {
	case "normal":
		return SCREEN_NORMAL
	case "half":
		return SCREEN_HALF
	case "fullscreen":
		return SCREEN_FULL
	default:
		return SCREEN_NORMAL
	}
}

func NewGui(log *logrus.Entry, containerCmd *commands.ContainerCommand, oSCommand *commands.OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*Gui, error) {
	initialState := guiState{
		Platform: *oSCommand.Platform,
		Panels: &panelStates{
			Main: &mainPanelState{
				ObjectKey: "",
			},
		},
		ViewStack: []string{},

		ShowExitedContainers: true,
		ScreenMode:           getScreenMode(config),
	}

	gui := &Gui{
		Log:           log,
		ContainerCmd:  containerCmd,
		OSCommand:     oSCommand,
		State:         initialState,
		Config:        config,
		Tr:            tr,
		statusManager: &statusManager{},
		taskManager:   tasks.NewTaskManager(log, tr),
		ErrorChan:     errorChan,
	}

	deadlock.Opts.Disable = !gui.Config.Debug
	deadlock.Opts.DeadlockTimeout = 10 * time.Second

	return gui, nil
}

func (gui *Gui) renderGlobalOptions() error {
	return gui.renderOptionsMap(map[string]string{
		"PgUp/PgDn": gui.Tr.Scroll,
		"← → ↑ ↓":   gui.Tr.Navigate,
		"q":         gui.Tr.Quit,
		"b":         gui.Tr.ViewBulkCommands,
		"x":         gui.Tr.Menu,
	})
}

func (gui *Gui) goEvery(interval time.Duration, function func() error) {
	_ = function()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if !gui.PauseBackgroundThreads {
				_ = function()
			}
		}
	}()
}

func (gui *Gui) Run() error {
	defer gui.taskManager.Close()

	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:       gocui.OutputTrue,
		RuneReplacements: map[rune]string{},
	})
	if err != nil {
		return err
	}
	defer g.Close()

	if !gui.Config.UserConfig.Gui.IgnoreMouseEvents {
		g.Mouse = true
	}

	gui.g = g

	deadlock.Opts.LogBuf = lcUtils.NewOnceWriter(os.Stderr, func() {
		gui.g.Close()
	})

	if err := gui.SetColorScheme(); err != nil {
		return err
	}

	throttledRefresh := throttle.ThrottleFunc(time.Millisecond*50, true, gui.refresh)
	defer throttledRefresh.Stop()

	go func() {
		for err := range gui.ErrorChan {
			if err == nil {
				continue
			}
			if strings.Contains(err.Error(), "No such container") {
				gui.Log.Warn(err)
				continue
			}
			_ = gui.createErrorPanel(err.Error())
		}
	}()

	g.SetManager(gocui.ManagerFunc(gui.layout), gocui.ManagerFunc(gui.getFocusLayout()))

	if err := gui.createAllViews(); err != nil {
		return err
	}
	if err := gui.setInitialViewContent(); err != nil {
		return err
	}

	gui.setPanels()

	if err = gui.keybindings(g); err != nil {
		return err
	}

	if gui.g.CurrentView() == nil {
		viewName := gui.initiallyFocusedViewName()
		view, err := gui.g.View(viewName)
		if err != nil {
			return err
		}

		if err := gui.switchFocus(view); err != nil {
			return err
		}
	}

	ctx, finish := context.WithCancel(context.Background())
	defer finish()

	go gui.monitorContainerStats(ctx)

	go func() {
		throttledRefresh.Trigger()

		gui.goEvery(time.Millisecond*30, gui.reRenderMain)
		gui.goEvery(time.Millisecond*1000, gui.renderContainers)
		gui.goEvery(time.Millisecond*1000, gui.checkForContextChange)
	}()

	err = g.MainLoop()
	if err == gocui.ErrQuit {
		return nil
	}
	return err
}

func (gui *Gui) setPanels() {
	gui.Panels = Panels{
		Containers: gui.getContainersPanel(),
		Images:     gui.getImagesPanel(),
		Volumes:    gui.getVolumesPanel(),
		Networks:   gui.getNetworksPanel(),
		Menu:       gui.getMenuPanel(),
	}
}

func (gui *Gui) renderContainers() error {
	return gui.Panels.Containers.RerenderList()
}

func (gui *Gui) refresh() {
	go func() {
		if err := gui.refreshContainers(); err != nil {
			gui.Log.Error(err)
		}
	}()
	go func() {
		if err := gui.reloadVolumes(); err != nil {
			gui.Log.Error(err)
		}
	}()
	go func() {
		if err := gui.reloadNetworks(); err != nil {
			gui.Log.Error(err)
		}
	}()
	go func() {
		if err := gui.reloadImages(); err != nil {
			gui.Log.Error(err)
		}
	}()
}

func (gui *Gui) checkForContextChange() error {
	return gui.newLineFocused(gui.g.CurrentView())
}

func (gui *Gui) reRenderMain() error {
	mainView := gui.Views.Main
	if mainView == nil {
		return nil
	}
	if mainView.IsTainted() {
		gui.g.Update(func(g *gocui.Gui) error {
			return nil
		})
	}
	return nil
}

func (gui *Gui) quit(g *gocui.Gui, v *gocui.View) error {
	if gui.Config.UserConfig.ConfirmOnQuit {
		return gui.createConfirmationPanel("", gui.Tr.ConfirmQuit, func(g *gocui.Gui, v *gocui.View) error {
			return gocui.ErrQuit
		}, nil)
	}
	return gocui.ErrQuit
}

func (gui *Gui) escape() error {
	if gui.State.Filter.active {
		return gui.clearFilter()
	}

	return nil
}

func (gui *Gui) handleDonate(g *gocui.Gui, v *gocui.View) error {
	if !gui.g.Mouse {
		return nil
	}

	cx, _ := v.Cursor()
	if cx > len(gui.Tr.Donate) {
		return nil
	}
	return gui.OSCommand.OpenLink("https://github.com/sponsors/jesseduffield")
}

func (gui *Gui) editFile(filename string) error {
	cmd, err := gui.OSCommand.EditFile(filename)
	if err != nil {
		return gui.createErrorPanel(err.Error())
	}

	return gui.runSubprocess(cmd)
}

func (gui *Gui) openFile(filename string) error {
	if err := gui.OSCommand.OpenFile(filename); err != nil {
		return gui.createErrorPanel(err.Error())
	}
	return nil
}

func (gui *Gui) handleCustomCommand(g *gocui.Gui, v *gocui.View) error {
	return gui.createPromptPanel(gui.Tr.CustomCommandTitle, func(g *gocui.Gui, v *gocui.View) error {
		command := gui.trimmedContent(v)
		return gui.runSubprocess(gui.OSCommand.RunCustomCommand(command))
	})
}

func (gui *Gui) ShouldRefresh(key string) bool {
	if gui.State.Panels.Main.ObjectKey == key {
		return false
	}

	gui.State.Panels.Main.ObjectKey = key
	return true
}

func (gui *Gui) initiallyFocusedViewName() string {
	return "containers"
}

func (gui *Gui) IgnoreStrings() []string {
	return gui.Config.UserConfig.Ignore
}

func (gui *Gui) Update(f func() error) {
	gui.g.Update(func(*gocui.Gui) error { return f() })
}

func (gui *Gui) monitorContainerStats(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, container := range gui.Panels.Containers.List.GetAllItems() {
				if !container.MonitoringStats {
					go gui.ContainerCmd.CreateClientStatMonitor(container)
				}
			}
		}
	}
}

func (gui *Gui) SetupFakeGui() {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:       gocui.OutputTrue,
		RuneReplacements: map[rune]string{},
		Headless:         true,
	})
	if err != nil {
		panic(err)
	}
	gui.g = g
	defer g.Close()
	if err := gui.createAllViews(); err != nil {
		panic(err)
	}

	gui.setPanels()
}
