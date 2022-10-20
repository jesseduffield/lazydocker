package gui

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"

	"github.com/go-errors/errors"

	throttle "github.com/boz/go-throttle"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/sirupsen/logrus"
)

// OverlappingEdges determines if panel edges overlap
var OverlappingEdges = false

// SentinelErrors are the errors that have special meaning and need to be checked
// by calling functions. The less of these, the better
type SentinelErrors struct {
	ErrNoContainers error
	ErrNoImages     error
	ErrNoVolumes    error
}

// GenerateSentinelErrors makes the sentinel errors for the gui. We're defining it here
// because we can't do package-scoped errors with localization, and also because
// it seems like package-scoped variables are bad in general
// https://dave.cheney.net/2017/06/11/go-without-package-scoped-variables
// In the future it would be good to implement some of the recommendations of
// that article. For now, if we don't need an error to be a sentinel, we will just
// define it inline. This has implications for error messages that pop up everywhere
// in that we'll be duplicating the default values. We may need to look at
// having a default localisation bundle defined, and just using keys-only when
// localising things in the code.
func (gui *Gui) GenerateSentinelErrors() {
	gui.Errors = SentinelErrors{
		ErrNoContainers: errors.New(gui.Tr.NoContainers),
		ErrNoImages:     errors.New(gui.Tr.NoImages),
		ErrNoVolumes:    errors.New(gui.Tr.NoVolumes),
	}
}

// Gui wraps the gocui Gui object which handles rendering and events
type Gui struct {
	g             *gocui.Gui
	Log           *logrus.Entry
	DockerCommand *commands.DockerCommand
	OSCommand     *commands.OSCommand
	State         guiState
	Config        *config.AppConfig
	Tr            *i18n.TranslationSet
	Errors        SentinelErrors
	statusManager *statusManager
	T             *tasks.TaskManager
	ErrorChan     chan error
	CyclableViews []string
	Views         Views

	// if we've suspended the gui (e.g. because we've switched to a subprocess)
	// we typically want to pause some things that are running like background
	// file refreshes
	PauseBackgroundThreads bool

	Mutexes

	Panels Panels
}

type Panels struct {
	Images *SideListPanel[*commands.Image]
}

type Mutexes struct {
	SubprocessMutex sync.Mutex
	ViewStackMutex  sync.Mutex
}

type servicePanelState struct {
	SelectedLine int
	ContextIndex int // for specifying if you are looking at logs/stats/config/etc
}

type containerPanelState struct {
	SelectedLine int
	ContextIndex int // for specifying if you are looking at logs/stats/config/etc
}

type projectState struct {
	ContextIndex int // for specifying if you are looking at credits/logs
}

type menuPanelState struct {
	SelectedLine int
	OnPress      func(*gocui.Gui, *gocui.View) error
}

type mainPanelState struct {
	// ObjectKey tells us what context we are in. For example, if we are looking at the logs of a particular service in the services panel this key might be 'services-<service id>-logs'. The key is made so that if something changes which might require us to re-run the logs command or run a different command, the key will be different, and we'll then know to do whatever is required. Object key probably isn't the best name for this but Context is already used to refer to tabs. Maybe I should just call them tabs.
	ObjectKey string
}

type volumePanelState struct {
	SelectedLine int
	ContextIndex int
}

type panelStates struct {
	Services   *servicePanelState
	Containers *containerPanelState
	Menu       *menuPanelState
	Main       *mainPanelState
	Volumes    *volumePanelState
	Project    *projectState
}

type guiState struct {
	MenuItemCount int // can't store the actual list because it's of interface{} type
	// the names of views in the current focus stack (last item is the current view)
	ViewStack        []string
	Platform         commands.Platform
	Panels           *panelStates
	SubProcessOutput string
	Stats            map[string]commands.ContainerStats

	ScreenMode WindowMaximisation

	Searching searchingState

	Lists Lists
}

// these are the items we display, after filtering is applied.
type Lists struct {
	Containers *FilteredList[*commands.Container]
	Services   *FilteredList[*commands.Service]
	Images     *FilteredList[*commands.Image]
	Volumes    *FilteredList[*commands.Volume]
}

type searchingState struct {
	view         *gocui.View
	isSearching  bool
	searchString string
}

// screen sizing determines how much space your selected window takes up (window
// as in panel, not your terminal's window). Sometimes you want a bit more space
// to see the contents of a panel, and this keeps track of how much maximisation
// you've set
type WindowMaximisation int

const (
	SCREEN_NORMAL WindowMaximisation = iota
	SCREEN_HALF
	SCREEN_FULL
)

// NewGui builds a new gui handler
func NewGui(log *logrus.Entry, dockerCommand *commands.DockerCommand, oSCommand *commands.OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*Gui, error) {
	initialState := guiState{
		Platform: *oSCommand.Platform,
		Panels: &panelStates{
			Services:   &servicePanelState{SelectedLine: -1, ContextIndex: 0},
			Containers: &containerPanelState{SelectedLine: -1, ContextIndex: 0},
			Volumes:    &volumePanelState{SelectedLine: -1, ContextIndex: 0},
			Menu:       &menuPanelState{SelectedLine: 0},
			Main: &mainPanelState{
				ObjectKey: "",
			},
			Project: &projectState{ContextIndex: 0},
		},
		ViewStack: []string{},
		Lists: Lists{
			Containers: NewFilteredList[*commands.Container](),
			Services:   NewFilteredList[*commands.Service](),
			Images:     NewFilteredList[*commands.Image](),
			Volumes:    NewFilteredList[*commands.Volume](),
		},
	}

	cyclableViews := []string{"project", "containers", "images", "volumes"}
	if dockerCommand.InDockerComposeProject {
		cyclableViews = []string{"project", "services", "containers", "images", "volumes"}
	}

	gui := &Gui{
		Log:           log,
		DockerCommand: dockerCommand,
		OSCommand:     oSCommand,
		// TODO: look into this warning
		State:         initialState,
		Config:        config,
		Tr:            tr,
		statusManager: &statusManager{},
		T:             tasks.NewTaskManager(log, tr),
		ErrorChan:     errorChan,
		CyclableViews: cyclableViews,
	}

	gui.GenerateSentinelErrors()

	return gui, nil
}

func (gui *Gui) renderGlobalOptions() error {
	return gui.renderOptionsMap(map[string]string{
		"PgUp/PgDn": gui.Tr.Scroll,
		"← → ↑ ↓":   gui.Tr.Navigate,
		"esc/q":     gui.Tr.Close,
		"b":         gui.Tr.ViewBulkCommands,
		"x":         gui.Tr.Menu,
	})
}

func (gui *Gui) goEvery(interval time.Duration, function func() error) {
	_ = function() // time.Tick doesn't run immediately so we'll do that here // TODO: maybe change
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if gui.PauseBackgroundThreads {
				return
			}

			_ = function()
		}
	}()
}

// Run setup the gui with keybindings and start the mainloop
func (gui *Gui) Run() error {
	// closing our task manager which in turn closes the current task if there is any, so we aren't leaving processes lying around after closing lazydocker
	defer gui.T.Close()

	g, err := gocui.NewGui(gocui.OutputTrue, OverlappingEdges, gocui.NORMAL, false, map[rune]string{})
	if err != nil {
		return err
	}
	defer g.Close()

	// forgive the double-negative, this is because of my yaml `omitempty` woes
	if !gui.Config.UserConfig.Gui.IgnoreMouseEvents {
		g.Mouse = true
	}

	gui.g = g // TODO: always use gui.g rather than passing g around everywhere

	if err := gui.SetColorScheme(); err != nil {
		return err
	}

	throttledRefresh := throttle.ThrottleFunc(time.Millisecond*50, true, gui.refresh)
	defer throttledRefresh.Stop()

	ctx, finish := context.WithCancel(context.Background())
	defer finish()

	go gui.listenForEvents(ctx, throttledRefresh.Trigger)
	go gui.DockerCommand.MonitorContainerStats(ctx)

	go func() {
		throttledRefresh.Trigger()

		gui.goEvery(time.Millisecond*30, gui.reRenderMain)
		gui.goEvery(time.Millisecond*1000, gui.DockerCommand.UpdateContainerDetails)
		gui.goEvery(time.Millisecond*1000, gui.checkForContextChange)
		gui.goEvery(time.Millisecond*1000, gui.rerenderContainersAndServices)
	}()

	go func() {
		for err := range gui.ErrorChan {
			if err == nil {
				continue
			}
			if strings.Contains(err.Error(), "No such container") {
				// this happens all the time when e.g. restarting containers so we won't worry about it
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

	// TODO: see if we can avoid the circular dependency
	gui.Panels = Panels{
		Images: gui.getImagePanel(),
	}

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

	err = g.MainLoop()
	if err == gocui.ErrQuit {
		return nil
	}
	return err
}

func (gui *Gui) rerenderContainersAndServices() error {
	// we need to regularly re-render these because their stats will be changed in the background
	gui.renderContainersAndServices()
	return nil
}

func (gui *Gui) refresh() {
	go gui.refreshProject()
	go func() {
		if err := gui.refreshContainersAndServices(); err != nil {
			gui.Log.Error(err)
		}
	}()
	go func() {
		if err := gui.refreshVolumes(); err != nil {
			gui.Log.Error(err)
		}
	}()
	go func() {
		if err := gui.reloadImages(); err != nil {
			gui.Log.Error(err)
		}
	}()
}

func (gui *Gui) listenForEvents(ctx context.Context, refresh func()) {
	errorCount := 0

	onError := func(err error) {
		if err != nil {
			gui.ErrorChan <- errors.Errorf("Docker event stream returned error: %s\nRetry count: %d", err.Error(), errorCount)
		}
		errorCount++
		time.Sleep(time.Second * 2)
	}

outer:
	for {
		messageChan, errChan := gui.DockerCommand.Client.Events(context.Background(), types.EventsOptions{})

		if errorCount > 0 {
			select {
			case <-ctx.Done():
				return
			case err := <-errChan:
				onError(err)
				continue outer
			default:
				// If we're here then we lost connection to docker and we just got it back.
				// The reason we do this refresh explicitly is because successfully
				// reconnecting with docker does not mean it's going to send us a new
				// event any time soon.

				// Assuming the confirmation prompt currently holds the given error
				_ = gui.closeConfirmationPrompt()
				refresh()
				errorCount = 0
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case message := <-messageChan:
				// We could be more granular about what events should trigger which refreshes.
				// At the moment it's pretty efficient though, and it might not be worth
				// the maintenance burden of mapping specific events to specific refreshes
				refresh()

				gui.Log.Infof("received event of type: %s", message.Type)
			case err := <-errChan:
				onError(err)
				continue outer
			}
		}
	}
}

// checkForContextChange runs the currently focused panel's 'select' function, simulating the current item having just been selected. This will then trigger a check to see if anything's changed (e.g. a service has a new container) and if so, the appropriate code will run. For example, if you're reading logs from a service and all of a sudden its container changes, this will trigger the 'select' function, which will work out that the context is not different because of the new container, and then it will re-attempt to get the logs, this time for the correct container. This 'context' is stored in the main panel's ObjectKey. I'm using the term 'context' here more broadly than just the different tabs you can view in a panel.
func (gui *Gui) checkForContextChange() error {
	return gui.newLineFocused(gui.g.CurrentView())
}

func (gui *Gui) reRenderMain() error {
	mainView := gui.getMainView()
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

func (gui *Gui) shouldRefresh(key string) bool {
	if gui.State.Panels.Main.ObjectKey == key {
		return false
	}

	gui.State.Panels.Main.ObjectKey = key
	return true
}

func (gui *Gui) ShouldRefresh(key string) bool {
	return gui.shouldRefresh(key)
}

func (gui *Gui) initiallyFocusedViewName() string {
	if gui.DockerCommand.InDockerComposeProject {
		return "services"
	}
	return "containers"
}

func (gui *Gui) IgnoreStrings() []string {
	return gui.Config.UserConfig.Ignore
}

func (gui *Gui) Update(f func() error) {
	gui.g.Update(func(*gocui.Gui) error { return f() })
}
