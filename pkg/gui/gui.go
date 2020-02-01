package gui

import (
	"github.com/golang-collections/collections/stack"
	"strings"
	"sync"

	// "io"
	// "io/ioutil"

	"os/exec"
	"time"

	"github.com/go-errors/errors"

	// "strings"

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
	ErrSubProcess   error
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
		ErrSubProcess:   errors.New(gui.Tr.RunningSubprocess),
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
	SubProcess    *exec.Cmd
	State         guiState
	Config        *config.AppConfig
	Tr            *i18n.TranslationSet
	Errors        SentinelErrors
	statusManager *statusManager
	waitForIntro  sync.WaitGroup
	T             *tasks.TaskManager
	ErrorChan     chan error
	CyclableViews []string
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

type imagePanelState struct {
	SelectedLine int
	ContextIndex int // for specifying if you are looking at logs/stats/config/etc
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
	Images     *imagePanelState
	Volumes    *volumePanelState
	Project    *projectState
}

type guiState struct {
	MenuItemCount    int // can't store the actual list because it's of interface{} type
	PreviousViews    *stack.Stack
	Platform         commands.Platform
	Panels           *panelStates
	SubProcessOutput string
	Stats            map[string]commands.ContainerStats

	// SessionIndex tells us how many times we've come back from a subprocess.
	// We increment it each time we switch to a new subprocess
	// Every time we go to a subprocess we need to close a few goroutines so this index is used for that purpose
	SessionIndex int
}

// NewGui builds a new gui handler
func NewGui(log *logrus.Entry, dockerCommand *commands.DockerCommand, oSCommand *commands.OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*Gui, error) {
	initialState := guiState{
		Platform: *oSCommand.Platform,
		Panels: &panelStates{
			Services:   &servicePanelState{SelectedLine: -1, ContextIndex: 0},
			Containers: &containerPanelState{SelectedLine: -1, ContextIndex: 0},
			Images:     &imagePanelState{SelectedLine: -1, ContextIndex: 0},
			Volumes:    &volumePanelState{SelectedLine: -1, ContextIndex: 0},
			Menu:       &menuPanelState{SelectedLine: 0},
			Main: &mainPanelState{
				ObjectKey: "",
			},
			Project: &projectState{ContextIndex: 0},
		},
		SessionIndex:  0,
		PreviousViews: stack.New(),
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (gui *Gui) loadNewDirectory() error {
	gui.waitForIntro.Done()

	if err := gui.refreshSidePanels(gui.g); err != nil {
		return err
	}

	if gui.Config.UserConfig.Reporting == "undetermined" {
		if err := gui.promptAnonymousReporting(); err != nil {
			return err
		}
	}
	return nil
}

func (gui *Gui) promptAnonymousReporting() error {
	return gui.createConfirmationPanel(gui.g, nil, gui.Tr.AnonymousReportingTitle, gui.Tr.AnonymousReportingPrompt, func(g *gocui.Gui, v *gocui.View) error {
		gui.waitForIntro.Done()
		// setting the value here explicitly so that we don't re-request after coming back from a subprocess. The proper solution would be to reload the config but that's tricky because it's loaded at the very top level
		gui.Config.UserConfig.Reporting = "on"
		return gui.Config.WriteToUserConfig(func(userConfig *config.UserConfig) error {
			userConfig.Reporting = "on"
			return nil
		})
	}, func(g *gocui.Gui, v *gocui.View) error {
		gui.waitForIntro.Done()
		gui.Config.UserConfig.Reporting = "off"
		return gui.Config.WriteToUserConfig(func(userConfig *config.UserConfig) error {
			userConfig.Reporting = "off"
			return nil
		})
	})
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
	currentSessionIndex := gui.State.SessionIndex
	_ = function() // time.Tick doesn't run immediately so we'll do that here // TODO: maybe change
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if gui.State.SessionIndex > currentSessionIndex {
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

	g, err := gocui.NewGui(gocui.OutputNormal, OverlappingEdges)
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

	if gui.Config.UserConfig.Reporting == "undetermined" {
		gui.waitForIntro.Add(2)
	} else {
		gui.waitForIntro.Add(1)
	}

	dockerRefreshInterval := gui.Config.UserConfig.Update.DockerRefreshInterval
	go func() {
		gui.waitForIntro.Wait()
		gui.goEvery(time.Millisecond*30, gui.reRenderMain)
		gui.goEvery(dockerRefreshInterval, gui.refreshProject)
		gui.goEvery(dockerRefreshInterval, gui.refreshContainersAndServices)
		gui.goEvery(dockerRefreshInterval, gui.refreshVolumes)
		gui.goEvery(time.Millisecond*1000, gui.DockerCommand.UpdateContainerDetails)
		gui.goEvery(time.Millisecond*1000, gui.checkForContextChange)
	}()

	gui.DockerCommand.MonitorContainerStats()

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
			gui.createErrorPanel(gui.g, err.Error())
		}
	}()

	g.SetManager(gocui.ManagerFunc(gui.layout), gocui.ManagerFunc(gui.getFocusLayout()))

	if err = gui.keybindings(g); err != nil {
		return err
	}

	err = g.MainLoop()
	return err
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
		return gui.createConfirmationPanel(g, v, "", gui.Tr.ConfirmQuit, func(g *gocui.Gui, v *gocui.View) error {
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
	return gui.OSCommand.OpenLink("https://donorbox.org/lazydocker")
}

func (gui *Gui) editFile(filename string) error {
	_, err := gui.runSyncOrAsyncCommand(gui.OSCommand.EditFile(filename))
	return err
}

func (gui *Gui) openFile(filename string) error {
	if err := gui.OSCommand.OpenFile(filename); err != nil {
		return gui.createErrorPanel(gui.g, err.Error())
	}
	return nil
}

// runSyncOrAsyncCommand takes the output of a command that may have returned
// either no error, an error, or a subprocess to execute, and if a subprocess
// needs to be set on the gui object, it does so, and then returns the error
// the bool returned tells us whether the calling code should continue
func (gui *Gui) runSyncOrAsyncCommand(sub *exec.Cmd, err error) (bool, error) {
	if err != nil {
		if err != gui.Errors.ErrSubProcess {
			return false, gui.createErrorPanel(gui.g, err.Error())
		}
	}
	if sub != nil {
		gui.SubProcess = sub
		return false, gui.Errors.ErrSubProcess
	}
	return true, nil
}

func (gui *Gui) handleCustomCommand(g *gocui.Gui, v *gocui.View) error {
	return gui.createPromptPanel(g, v, gui.Tr.CustomCommandTitle, func(g *gocui.Gui, v *gocui.View) error {
		command := gui.trimmedContent(v)
		gui.SubProcess = gui.OSCommand.RunCustomCommand(command)
		return gui.Errors.ErrSubProcess
	})
}

func (gui *Gui) shouldRefresh(key string) bool {
	if gui.State.Panels.Main.ObjectKey == key {
		return false
	}

	gui.State.Panels.Main.ObjectKey = key
	return true
}

func (gui *Gui) initiallyFocusedViewName() string {
	if gui.DockerCommand.InDockerComposeProject {
		return "services"
	}
	return "containers"
}
