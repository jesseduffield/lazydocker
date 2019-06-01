package gui

import (
	"fmt"
	"math"
	"strings"
	"sync"

	// "io"
	// "io/ioutil"

	"io/ioutil"
	"os"
	"os/exec"
	"time"

	"github.com/fatih/color"
	"github.com/go-errors/errors"

	// "strings"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/jesseduffield/lazydocker/pkg/updates"
	"github.com/jesseduffield/lazydocker/pkg/utils"
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
		ErrSubProcess:   errors.New(gui.Tr.SLocalize("RunningSubprocess")),
		ErrNoContainers: errors.New(gui.Tr.SLocalize("NoContainers")),
		ErrNoImages:     errors.New(gui.Tr.SLocalize("NoImages")),
	}
}

// Teml is short for template used to make the required map[string]interface{} shorter when using gui.Tr.SLocalize and gui.Tr.TemplateLocalize
type Teml i18n.Teml

// Gui wraps the gocui Gui object which handles rendering and events
type Gui struct {
	g             *gocui.Gui
	Log           *logrus.Entry
	DockerCommand *commands.DockerCommand
	OSCommand     *commands.OSCommand
	SubProcess    *exec.Cmd
	State         guiState
	Config        config.AppConfigurer
	Tr            *i18n.Localizer
	Errors        SentinelErrors
	Updater       *updates.Updater
	statusManager *statusManager
	waitForIntro  sync.WaitGroup
	T             *tasks.TaskManager
}

type servicePanelState struct {
	SelectedLine int
	ContextIndex int // for specifying if you are looking at logs/stats/config/etc
}

type containerPanelState struct {
	SelectedLine int
	ContextIndex int // for specifying if you are looking at logs/stats/config/etc
}

type menuPanelState struct {
	SelectedLine int
}

type mainPanelState struct {
	ObjectKey string
}

type imagePanelState struct {
	SelectedLine int
	ContextIndex int // for specifying if you are looking at logs/stats/config/etc
}

type panelStates struct {
	Services   *servicePanelState
	Containers *containerPanelState
	Menu       *menuPanelState
	Main       *mainPanelState
	Images     *imagePanelState
}

type guiState struct {
	Services         []*commands.Service
	Containers       []*commands.Container
	Images           []*commands.Image
	MenuItemCount    int // can't store the actual list because it's of interface{} type
	PreviousView     string
	Platform         commands.Platform
	Updating         bool
	Panels           *panelStates
	SubProcessOutput string
	MainProcessMutex sync.Mutex
	MainProcessChan  chan struct{}
}

// NewGui builds a new gui handler
func NewGui(log *logrus.Entry, dockerCommand *commands.DockerCommand, oSCommand *commands.OSCommand, tr *i18n.Localizer, config config.AppConfigurer, updater *updates.Updater) (*Gui, error) {

	initialState := guiState{
		Containers:   make([]*commands.Container, 0),
		PreviousView: "services",
		Platform:     *oSCommand.Platform,
		Panels: &panelStates{
			Services:   &servicePanelState{SelectedLine: -1, ContextIndex: 0},
			Containers: &containerPanelState{SelectedLine: -1, ContextIndex: 0},
			Images:     &imagePanelState{SelectedLine: -1, ContextIndex: 0},
			Menu:       &menuPanelState{SelectedLine: 0},
			Main: &mainPanelState{
				ObjectKey: "",
			},
		},
		MainProcessChan: make(chan struct{}),
	}

	go func() {
		// setting up a goroutine for listening to the first stop signal on this channel
		// because whenever something wants to lock the mutex, it tells the existing process to stop
		// but on startup we don't have a process so we just mock it
		// this is because we're using an unbuffered channel
		<-initialState.MainProcessChan
	}()

	gui := &Gui{
		Log:           log,
		DockerCommand: dockerCommand,
		OSCommand:     oSCommand,
		State:         initialState,
		Config:        config,
		Tr:            tr,
		Updater:       updater,
		statusManager: &statusManager{},
		T:             tasks.NewTaskManager(),
	}

	gui.GenerateSentinelErrors()

	return gui, nil
}

func (gui *Gui) scrollUpMain(g *gocui.Gui, v *gocui.View) error {
	mainView, _ := g.View("main")
	mainView.Autoscroll = false
	ox, oy := mainView.Origin()
	newOy := int(math.Max(0, float64(oy-gui.Config.GetUserConfig().GetInt("gui.scrollHeight"))))
	return mainView.SetOrigin(ox, newOy)
}

func (gui *Gui) scrollDownMain(g *gocui.Gui, v *gocui.View) error {
	mainView, _ := g.View("main")
	ox, oy := mainView.Origin()
	y := oy
	if !gui.Config.GetUserConfig().GetBool("gui.scrollPastBottom") {
		_, sy := mainView.Size()
		y += sy
	}
	// for some reason we can't work out whether we've hit the bottomq
	// there is a large discrepancy in the origin's y value and the length of BufferLines
	return mainView.SetOrigin(ox, oy+gui.Config.GetUserConfig().GetInt("gui.scrollHeight"))
}

func (gui *Gui) autoScrollMain(g *gocui.Gui, v *gocui.View) error {
	gui.getMainView().Autoscroll = true
	return nil
}

func (gui *Gui) handleRefresh(g *gocui.Gui, v *gocui.View) error {
	return gui.refreshSidePanels(g)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// getFocusLayout returns a manager function for when view gain and lose focus
func (gui *Gui) getFocusLayout() func(g *gocui.Gui) error {
	var previousView *gocui.View
	return func(g *gocui.Gui) error {
		newView := gui.g.CurrentView()
		if err := gui.onFocusChange(); err != nil {
			return err
		}
		// for now we don't consider losing focus to a popup panel as actually losing focus
		if newView != previousView && !gui.isPopupPanel(newView.Name()) {
			if err := gui.onFocusLost(previousView, newView); err != nil {
				return err
			}
			if err := gui.onFocus(newView); err != nil {
				return err
			}
			previousView = newView
		}
		return nil
	}
}

func (gui *Gui) onFocusChange() error {
	currentView := gui.g.CurrentView()
	for _, view := range gui.g.Views() {
		view.Highlight = view == currentView
	}
	return nil
}

func (gui *Gui) onFocusLost(v *gocui.View, newView *gocui.View) error {
	if v == nil {
		return nil
	}
	gui.Log.Info(v.Name() + " focus lost")
	return nil
}

func (gui *Gui) onFocus(v *gocui.View) error {
	if v == nil {
		return nil
	}
	gui.Log.Info(v.Name() + " focus gained")
	return nil
}

// layout is called for every screen re-render e.g. when the screen is resized
func (gui *Gui) layout(g *gocui.Gui) error {
	g.Highlight = true
	width, height := g.Size()

	information := gui.Config.GetVersion()

	minimumHeight := 9
	minimumWidth := 10
	if height < minimumHeight || width < minimumWidth {
		v, err := g.SetView("limit", 0, 0, width-1, height-1, 0)
		if err != nil {
			if err.Error() != "unknown view" {
				return err
			}
			v.Title = gui.Tr.SLocalize("NotEnoughSpace")
			v.Wrap = true
			_, _ = g.SetViewOnTop("limit")
		}
		return nil
	}

	currView := gui.g.CurrentView()
	currentCyclebleView := gui.State.PreviousView
	if currView != nil {
		viewName := currView.Name()
		usePreviouseView := true
		for _, view := range cyclableViews {
			if view == viewName {
				currentCyclebleView = viewName
				usePreviouseView = false
				break
			}
		}
		if usePreviouseView {
			currentCyclebleView = gui.State.PreviousView
		}
	}

	usableSpace := height - 4

	tallPanels := 3

	vHeights := map[string]int{
		"status":     tallPanels,
		"services":   usableSpace/tallPanels + usableSpace%tallPanels,
		"containers": usableSpace / tallPanels,
		"images":     usableSpace / tallPanels,
		"options":    1,
	}

	if height < 28 {
		defaultHeight := 3
		if height < 21 {
			defaultHeight = 1
		}
		vHeights = map[string]int{
			"status":     defaultHeight,
			"services":   defaultHeight,
			"containers": defaultHeight,
			"images":     defaultHeight,
			"options":    defaultHeight,
		}
		vHeights[currentCyclebleView] = height - defaultHeight*tallPanels - 1
	}

	optionsVersionBoundary := width - max(len(utils.Decolorise(information)), 1)
	leftSideWidth := width / 3

	appStatus := gui.statusManager.getStatusString()
	appStatusOptionsBoundary := 0
	if appStatus != "" {
		appStatusOptionsBoundary = len(appStatus) + 2
	}

	_, _ = g.SetViewOnBottom("limit")
	g.DeleteView("limit")

	v, err := g.SetView("main", leftSideWidth+1, 0, width-1, height-2, gocui.LEFT)
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.Wrap = true
		v.FgColor = gocui.ColorWhite
	}

	if v, err := g.SetView("status", 0, 0, leftSideWidth, vHeights["status"]-1, gocui.BOTTOM|gocui.RIGHT); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.Title = gui.Tr.SLocalize("StatusTitle")
		v.FgColor = gocui.ColorWhite
	}

	servicesView, err := g.SetViewBeneath("services", "status", vHeights["services"])
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		servicesView.Highlight = true
		servicesView.Title = gui.Tr.SLocalize("ServicesTitle")
		servicesView.FgColor = gocui.ColorWhite
	}

	containersView, err := g.SetViewBeneath("containers", "services", vHeights["containers"])
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		containersView.Highlight = true
		containersView.Title = gui.Tr.SLocalize("ContainersTitle")
		containersView.FgColor = gocui.ColorWhite
	}

	imagesView, err := g.SetViewBeneath("images", "containers", vHeights["images"])
	if err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		imagesView.Highlight = true
		imagesView.Title = gui.Tr.SLocalize("ImagesTitle")
		imagesView.FgColor = gocui.ColorWhite
	}

	if v, err := g.SetView("options", appStatusOptionsBoundary-1, height-2, optionsVersionBoundary-1, height, 0); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.Frame = false
		if v.FgColor, err = gui.GetOptionsPanelTextColor(); err != nil {
			return err
		}
	}

	if appStatusView, err := g.SetView("appStatus", -1, height-2, width, height, 0); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		appStatusView.BgColor = gocui.ColorDefault
		appStatusView.FgColor = gocui.ColorCyan
		appStatusView.Frame = false
		if _, err := g.SetViewOnBottom("appStatus"); err != nil {
			return err
		}
	}

	if v, err := g.SetView("information", optionsVersionBoundary-1, height-2, width, height, 0); err != nil {
		if err.Error() != "unknown view" {
			return err
		}
		v.BgColor = gocui.ColorDefault
		v.FgColor = gocui.ColorGreen
		v.Frame = false
		if err := gui.renderString(g, "information", information); err != nil {
			return err
		}

		// doing this here because it'll only happen once
		if err := gui.loadNewDirectory(); err != nil {
			return err
		}
	}

	if gui.g.CurrentView() == nil {
		if _, err := gui.g.SetCurrentView(gui.getContainersView().Name()); err != nil {
			return err
		}

		if err := gui.switchFocus(gui.g, nil, gui.getContainersView()); err != nil {
			return err
		}
	}

	type listViewState struct {
		selectedLine int
		lineCount    int
	}

	listViews := map[*gocui.View]listViewState{
		containersView: {selectedLine: gui.State.Panels.Containers.SelectedLine, lineCount: len(gui.State.Containers)},
		imagesView:     {selectedLine: gui.State.Panels.Images.SelectedLine, lineCount: len(gui.State.Images)},
	}

	// menu view might not exist so we check to be safe
	if menuView, err := gui.g.View("menu"); err == nil {
		listViews[menuView] = listViewState{selectedLine: gui.State.Panels.Menu.SelectedLine, lineCount: gui.State.MenuItemCount}
	}
	for view, state := range listViews {
		// check if the selected line is now out of view and if so refocus it
		if err := gui.focusPoint(0, state.selectedLine, state.lineCount, view); err != nil {
			return err
		}
	}

	// here is a good place log some stuff
	// if you download humanlog and do tail -f development.log | humanlog
	// this will let you see these branches as prettified json
	// gui.Log.Info(utils.AsJson(gui.State.Branches[0:4]))
	return gui.resizeCurrentPopupPanel(g)
}

func (gui *Gui) loadNewDirectory() error {
	gui.Updater.CheckForNewUpdate(gui.onBackgroundUpdateCheckFinish, false)

	gui.waitForIntro.Done()

	if err := gui.refreshSidePanels(gui.g); err != nil {
		return err
	}

	if gui.Config.GetUserConfig().GetString("reporting") == "undetermined" {
		if err := gui.promptAnonymousReporting(); err != nil {
			return err
		}
	}
	return nil
}

func (gui *Gui) promptAnonymousReporting() error {
	return gui.createConfirmationPanel(gui.g, nil, gui.Tr.SLocalize("AnonymousReportingTitle"), gui.Tr.SLocalize("AnonymousReportingPrompt"), func(g *gocui.Gui, v *gocui.View) error {
		gui.waitForIntro.Done()
		return gui.Config.WriteToUserConfig("reporting", "on")
	}, func(g *gocui.Gui, v *gocui.View) error {
		gui.waitForIntro.Done()
		return gui.Config.WriteToUserConfig("reporting", "off")
	})
}

func (gui *Gui) renderAppStatus() error {
	appStatus := gui.statusManager.getStatusString()
	if appStatus != "" {
		return gui.renderString(gui.g, "appStatus", appStatus)
	}
	return nil
}

func (gui *Gui) renderGlobalOptions() error {
	return gui.renderOptionsMap(map[string]string{
		"PgUp/PgDn": gui.Tr.SLocalize("scroll"),
		"← → ↑ ↓":   gui.Tr.SLocalize("navigate"),
		"esc/q":     gui.Tr.SLocalize("close"),
		"x":         gui.Tr.SLocalize("menu"),
	})
}

func (gui *Gui) goEvery(interval time.Duration, function func() error) {
	go func() {
		for range time.Tick(interval) {
			_ = function()
		}
	}()
}

// Run setup the gui with keybindings and start the mainloop
func (gui *Gui) Run() error {
	g, err := gocui.NewGui(gocui.OutputNormal, OverlappingEdges)
	if err != nil {
		return err
	}
	defer g.Close()

	if gui.Config.GetUserConfig().GetBool("gui.mouseEvents") {
		g.Mouse = true
	}

	gui.g = g // TODO: always use gui.g rather than passing g around everywhere

	if err := gui.SetColorScheme(); err != nil {
		return err
	}

	if gui.Config.GetUserConfig().GetString("reporting") == "undetermined" {
		gui.waitForIntro.Add(2)
	} else {
		gui.waitForIntro.Add(1)
	}

	go func() {
		gui.waitForIntro.Wait()
		gui.goEvery(time.Millisecond*50, gui.renderAppStatus)
		gui.goEvery(time.Millisecond*30, gui.reRenderMain)
		gui.goEvery(time.Millisecond*500, gui.refreshContainersAndServices)
	}()

	g.SetManager(gocui.ManagerFunc(gui.layout), gocui.ManagerFunc(gui.getFocusLayout()))

	if err = gui.keybindings(g); err != nil {
		return err
	}

	err = g.MainLoop()
	return err
}

func (gui *Gui) reRenderMain() error {
	if gui.getMainView().IsTainted() {
		gui.g.Update(func(g *gocui.Gui) error {
			return nil
		})
	}
	return nil
}

// RunWithSubprocesses loops, instantiating a new gocui.Gui with each iteration
// if the error returned from a run is a ErrSubProcess, it runs the subprocess
// otherwise it handles the error, possibly by quitting the application
func (gui *Gui) RunWithSubprocesses() error {
	for {
		if err := gui.Run(); err != nil {
			if err == gocui.ErrQuit {
				break
			} else if err == gui.Errors.ErrSubProcess {
				if err := gui.runCommand(); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}
	return nil
}

func (gui *Gui) runCommand() error {
	gui.SubProcess.Stdout = os.Stdout
	gui.SubProcess.Stderr = os.Stdout
	gui.SubProcess.Stdin = os.Stdin

	fmt.Fprintf(os.Stdout, "\n%s\n\n", utils.ColoredString("+ "+strings.Join(gui.SubProcess.Args, " "), color.FgBlue))

	if err := gui.SubProcess.Run(); err != nil {
		// not handling the error explicitly because usually we're going to see it
		// in the output anyway
		gui.Log.Error(err)
	}

	gui.SubProcess.Stdout = ioutil.Discard
	gui.SubProcess.Stderr = ioutil.Discard
	gui.SubProcess.Stdin = nil
	gui.SubProcess = nil

	// fmt.Fprintf(os.Stdout, "\n%s", utils.ColoredString(gui.Tr.SLocalize("pressEnterToReturn"), color.FgGreen))

	// fmt.Scanln() // wait for enter press

	return nil
}

func (gui *Gui) quit(g *gocui.Gui, v *gocui.View) error {
	if gui.State.Updating {
		return gui.createUpdateQuitConfirmation(g, v)
	}
	if gui.Config.GetUserConfig().GetBool("confirmOnQuit") {
		return gui.createConfirmationPanel(g, v, "", gui.Tr.SLocalize("ConfirmQuit"), func(g *gocui.Gui, v *gocui.View) error {
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
	if cx > len(gui.Tr.SLocalize("Donate")) {
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
