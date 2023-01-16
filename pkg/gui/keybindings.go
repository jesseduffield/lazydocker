package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
)

// Binding - a keybinding mapping a key and modifier to a handler. The keypress
// is only handled if the given view has focus, or handled globally if the view
// is ""
type Binding struct {
	ViewName    string
	Handler     func(*gocui.Gui, *gocui.View) error
	Key         interface{} // FIXME: find out how to get `gocui.Key | rune`
	Modifier    gocui.Modifier
	Description string
}

// GetKey is a function.
func (b *Binding) GetKey() string {
	key := 0

	switch b.Key.(type) {
	case rune:
		key = int(b.Key.(rune))
	case gocui.Key:
		key = int(b.Key.(gocui.Key))
	}

	// special keys
	switch key {
	case 27:
		return "esc"
	case 13:
		return "enter"
	case 32:
		return "space"
	case 65514:
		return "►"
	case 65515:
		return "◄"
	case 65517:
		return "▲"
	case 65516:
		return "▼"
	case 65508:
		return "PgUp"
	case 65507:
		return "PgDn"
	}

	return fmt.Sprintf("%c", key)
}

// GetInitialKeybindings is a function.
func (gui *Gui) GetInitialKeybindings() []*Binding {
	bindings := []*Binding{
		{
			ViewName: "",
			Key:      gocui.KeyEsc,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.escape),
		},
		{
			ViewName: "",
			Key:      'q',
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Key:      gocui.KeyCtrlC,
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Key:      gocui.KeyPgup,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollUpMain),
		},
		{
			ViewName: "",
			Key:      gocui.KeyPgdn,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollDownMain),
		},
		{
			ViewName: "",
			Key:      gocui.KeyCtrlU,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollUpMain),
		},
		{
			ViewName: "",
			Key:      gocui.KeyCtrlD,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollDownMain),
		},
		{
			ViewName: "",
			Key:      gocui.KeyEnd,
			Modifier: gocui.ModNone,
			Handler:  gui.autoScrollMain,
		},
		{
			ViewName: "",
			Key:      gocui.KeyHome,
			Modifier: gocui.ModNone,
			Handler:  gui.jumpToTopMain,
		},
		{
			ViewName: "",
			Key:      'x',
			Modifier: gocui.ModNone,
			Handler:  gui.handleCreateOptionsMenu,
		},
		{
			ViewName: "",
			Key:      '?',
			Modifier: gocui.ModNone,
			Handler:  gui.handleCreateOptionsMenu,
		},
		{
			ViewName: "",
			Key:      'X',
			Modifier: gocui.ModNone,
			Handler:  gui.handleCustomCommand,
		},
		{
			ViewName:    "project",
			Key:         'e',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleEditConfig,
			Description: gui.Tr.EditConfig,
		},
		{
			ViewName:    "project",
			Key:         'o',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleOpenConfig,
			Description: gui.Tr.OpenConfig,
		},
		{
			ViewName:    "project",
			Key:         'm',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleViewAllLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName: "menu",
			Key:      gocui.KeyEsc,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuClose),
		},
		{
			ViewName: "menu",
			Key:      'q',
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuClose),
		},
		{
			ViewName: "menu",
			Key:      ' ',
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuPress),
		},
		{
			ViewName: "menu",
			Key:      gocui.KeyEnter,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuPress),
		},
		{
			ViewName: "menu",
			Key:      'y',
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuPress),
		},
		{
			ViewName: "information",
			Key:      gocui.MouseLeft,
			Modifier: gocui.ModNone,
			Handler:  gui.handleDonate,
		},
		{
			ViewName:    "containers",
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersRemoveMenu,
			Description: gui.Tr.Remove,
		},
		{
			ViewName:    "containers",
			Key:         'e',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleHideStoppedContainers,
			Description: gui.Tr.HideStopped,
		},
		{
			ViewName:    "containers",
			Key:         'p',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerPause,
			Description: gui.Tr.Pause,
		},
		{
			ViewName:    "containers",
			Key:         's',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerStop,
			Description: gui.Tr.Stop,
		},
		{
			ViewName:    "containers",
			Key:         'r',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerRestart,
			Description: gui.Tr.Restart,
		},
		{
			ViewName:    "containers",
			Key:         'a',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerAttach,
			Description: gui.Tr.Attach,
		},
		{
			ViewName:    "containers",
			Key:         'm',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerViewLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName:    "containers",
			Key:         'E',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersExecShell,
			Description: gui.Tr.ExecShell,
		},
		{
			ViewName:    "containers",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "containers",
			Key:         'b',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "containers",
			Key:         'w',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersOpenInBrowserCommand,
			Description: gui.Tr.OpenInBrowser,
		},
		{
			ViewName:    "services",
			Key:         'u',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceUp,
			Description: gui.Tr.UpService,
		},
		{
			ViewName:    "services",
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRemoveMenu,
			Description: gui.Tr.RemoveService,
		},
		{
			ViewName:    "services",
			Key:         's',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceStop,
			Description: gui.Tr.Stop,
		},
		{
			ViewName:    "services",
			Key:         'p',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicePause,
			Description: gui.Tr.Pause,
		},
		{
			ViewName:    "services",
			Key:         'r',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRestart,
			Description: gui.Tr.Restart,
		},
		{
			ViewName:    "services",
			Key:         'S',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceStart,
			Description: gui.Tr.Start,
		},
		{
			ViewName:    "services",
			Key:         'a',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceAttach,
			Description: gui.Tr.Attach,
		},
		{
			ViewName:    "services",
			Key:         'm',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRenderLogsToMain,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName:    "services",
			Key:         'U',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectUp,
			Description: gui.Tr.UpProject,
		},
		{
			ViewName:    "services",
			Key:         'D',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectDown,
			Description: gui.Tr.DownProject,
		},
		{
			ViewName:    "services",
			Key:         'R',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRestartMenu,
			Description: gui.Tr.ViewRestartOptions,
		},
		{
			ViewName:    "services",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "services",
			Key:         'b',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "services",
			Key:         'E',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesExecShell,
			Description: gui.Tr.ExecShell,
		},
		{
			ViewName:    "services",
			Key:         'w',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesOpenInBrowserCommand,
			Description: gui.Tr.OpenInBrowser,
		},
		{
			ViewName:    "images",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "images",
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesRemoveMenu,
			Description: gui.Tr.RemoveImage,
		},
		{
			ViewName:    "images",
			Key:         'b',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "volumes",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "volumes",
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesRemoveMenu,
			Description: gui.Tr.RemoveVolume,
		},
		{
			ViewName:    "volumes",
			Key:         'b',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "networks",
			Key:         'c',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleNetworksCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "networks",
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleNetworksRemoveMenu,
			Description: gui.Tr.RemoveNetwork,
		},
		{
			ViewName:    "networks",
			Key:         'b',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleNetworksBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "main",
			Key:         gocui.KeyEsc,
			Modifier:    gocui.ModNone,
			Handler:     gui.handleExitMain,
			Description: gui.Tr.Return,
		},
		{
			ViewName: "main",
			Key:      gocui.KeyArrowLeft,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "main",
			Key:      gocui.KeyArrowRight,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
		{
			ViewName: "main",
			Key:      'h',
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "main",
			Key:      'l',
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
		{
			ViewName: "filter",
			Key:      gocui.KeyEnter,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.commitFilter),
		},
		{
			ViewName: "filter",
			Key:      gocui.KeyEsc,
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.escapeFilterPrompt),
		},
		{
			ViewName: "",
			Key:      'J',
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollDownMain),
		},
		{
			ViewName: "",
			Key:      'K',
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollUpMain),
		},
		{
			ViewName: "",
			Key:      'H',
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "",
			Key:      'L',
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
		{
			ViewName:    "",
			Key:         '+',
			Handler:     wrappedHandler(gui.nextScreenMode),
			Description: gui.Tr.LcNextScreenMode,
		},
		{
			ViewName:    "",
			Key:         '_',
			Handler:     wrappedHandler(gui.prevScreenMode),
			Description: gui.Tr.LcPrevScreenMode,
		},
	}

	for _, panel := range gui.allSidePanels() {
		bindings = append(bindings, []*Binding{
			{ViewName: panel.GetView().Name(), Key: gocui.KeyArrowLeft, Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: panel.GetView().Name(), Key: gocui.KeyArrowRight, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: 'h', Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: panel.GetView().Name(), Key: 'l', Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: gocui.KeyTab, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: gocui.KeyBacktab, Modifier: gocui.ModNone, Handler: gui.previousView},
		}...)
	}

	setUpDownClickBindings := func(viewName string, onUp func() error, onDown func() error, onClick func() error) {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Key: 'k', Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: gocui.KeyArrowUp, Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: gocui.MouseWheelUp, Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: 'j', Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: gocui.KeyArrowDown, Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: gocui.MouseWheelDown, Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: gocui.MouseLeft, Modifier: gocui.ModNone, Handler: wrappedHandler(onClick)},
		}...)
	}

	for _, panel := range gui.allListPanels() {
		setUpDownClickBindings(panel.GetView().Name(), panel.HandlePrevLine, panel.HandleNextLine, panel.HandleClick)
	}

	setUpDownClickBindings("main", gui.scrollUpMain, gui.scrollDownMain, gui.handleMainClick)

	for _, panel := range gui.allSidePanels() {
		bindings = append(bindings,
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         gocui.KeyEnter,
				Modifier:    gocui.ModNone,
				Handler:     gui.handleEnterMain,
				Description: gui.Tr.FocusMain,
			},
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         '[',
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(panel.HandlePrevMainTab),
				Description: gui.Tr.PreviousContext,
			},
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         ']',
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(panel.HandleNextMainTab),
				Description: gui.Tr.NextContext,
			},
		)
	}

	for _, panel := range gui.allListPanels() {
		if !panel.IsFilterDisabled() {
			bindings = append(bindings, &Binding{
				ViewName:    panel.GetView().Name(),
				Key:         '/',
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(gui.handleOpenFilter),
				Description: gui.Tr.LcFilter,
			})
		}
	}

	return bindings
}

func (gui *Gui) keybindings(g *gocui.Gui) error {
	bindings := gui.GetInitialKeybindings()

	for _, binding := range bindings {
		if err := g.SetKeybinding(binding.ViewName, binding.Key, binding.Modifier, binding.Handler); err != nil {
			return err
		}
	}

	if err := g.SetTabClickBinding("main", gui.onMainTabClick); err != nil {
		return err
	}

	return nil
}

func wrappedHandler(f func() error) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		return f()
	}
}
