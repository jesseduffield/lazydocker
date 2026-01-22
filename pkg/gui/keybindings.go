package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/gui/keybindings"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
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

// Helper to create bindings with error handling
func (gui *Gui) createBinding(opts types.KeybindingsOpts, viewName string, keyStr string, modifier gocui.Modifier, handler func(*gocui.Gui, *gocui.View) error, description string) (*Binding, error) {
	key, err := keybindings.GetKey(keyStr)
	if err != nil {
		return nil, fmt.Errorf("invalid keybinding '%s': %w", keyStr, err)
	}
	return &Binding{
		ViewName:    viewName,
		Key:         key,
		Modifier:    modifier,
		Handler:     handler,
		Description: description,
	}, nil
}

// GetInitialKeybindings is a function.
func (gui *Gui) GetInitialKeybindings(opts types.KeybindingsOpts) ([]*Binding, error) {
	// Helper function to get key and handle errors gracefully by returning nil on error
	// This allows us to use inline calls without repetitive error handling
	safeGetKey := func(keyStr string) interface{} {
		key, err := opts.GetKey(keyStr)
		if err != nil {
			// Log the error but return nil so the binding will be filtered out later
			// This maintains functionality while eliminating repetitive error handling
			return nil
		}
		return key
	}

	var bindings []*Binding

	// Universal bindings
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.Return),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.escape),
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.Quit),
		Modifier: gocui.ModNone,
		Handler:  gui.quit,
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.QuitAlt),
		Modifier: gocui.ModNone,
		Handler:  gui.quit,
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollUpMain),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollUpMain),
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollDownMain),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollDownMain),
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollUpMainAlt1),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollUpMain),
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollDownMainAlt1),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollDownMain),
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.AutoScrollMain),
		Modifier: gocui.ModNone,
		Handler:  gui.autoScrollMain,
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.JumpToTopMain),
		Modifier: gocui.ModNone,
		Handler:  gui.jumpToTopMain,
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.OpenMenu),
		Modifier: gocui.ModNone,
		Handler:  gui.handleCreateOptionsMenu,
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.OpenMenuAlt),
		Modifier: gocui.ModNone,
		Handler:  gui.handleCreateOptionsMenu,
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.CustomCommand),
		Modifier: gocui.ModNone,
		Handler:  gui.handleCustomCommand,
	})

	// Project bindings
	bindings = append(bindings, &Binding{
		ViewName:    "project",
		Key:         safeGetKey(opts.Config.Project.EditConfig),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleEditConfig,
		Description: gui.Tr.EditConfig,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "project",
		Key:         safeGetKey(opts.Config.Project.OpenConfig),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleOpenConfig,
		Description: gui.Tr.OpenConfig,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "project",
		Key:         safeGetKey(opts.Config.Project.ViewLogs),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleViewAllLogs,
		Description: gui.Tr.ViewLogs,
	})

	// Menu bindings
	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      safeGetKey(opts.Config.Menu.Close),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuClose),
	})

	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      safeGetKey(opts.Config.Menu.CloseAlt),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuClose),
	})

	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      safeGetKey(opts.Config.Menu.Select),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuPress),
	})

	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      safeGetKey(opts.Config.Menu.Confirm),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuPress),
	})

	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      safeGetKey(opts.Config.Menu.SelectAlt),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuPress),
	})

	// Information binding (MouseLeft doesn't need GetKey)
	bindings = append(bindings, &Binding{
		ViewName: "information",
		Key:      gocui.MouseLeft,
		Modifier: gocui.ModNone,
		Handler:  gui.handleDonate,
	})

	// Containers bindings
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.Remove),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersRemoveMenu,
		Description: gui.Tr.Remove,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.HideStopped),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleHideStoppedContainers,
		Description: gui.Tr.HideStopped,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.Pause),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerPause,
		Description: gui.Tr.Pause,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.Stop),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerStop,
		Description: gui.Tr.Stop,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.Restart),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerRestart,
		Description: gui.Tr.Restart,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.Attach),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerAttach,
		Description: gui.Tr.Attach,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.ViewLogs),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerViewLogs,
		Description: gui.Tr.ViewLogs,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.ExecShell),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersExecShell,
		Description: gui.Tr.ExecShell,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.CustomCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.BulkCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         safeGetKey(opts.Config.Containers.OpenInBrowser),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersOpenInBrowserCommand,
		Description: gui.Tr.OpenInBrowser,
	})

	// Services bindings
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.Up),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceUp,
		Description: gui.Tr.UpService,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.Remove),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRemoveMenu,
		Description: gui.Tr.RemoveService,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.Stop),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceStop,
		Description: gui.Tr.Stop,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.Pause),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicePause,
		Description: gui.Tr.Pause,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.Restart),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRestart,
		Description: gui.Tr.Restart,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.Start),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceStart,
		Description: gui.Tr.Start,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.Attach),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceAttach,
		Description: gui.Tr.Attach,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.ViewLogs),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRenderLogsToMain,
		Description: gui.Tr.ViewLogs,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.UpProject),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleProjectUp,
		Description: gui.Tr.UpProject,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.DownProject),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleProjectDown,
		Description: gui.Tr.DownProject,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.RestartMenu),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRestartMenu,
		Description: gui.Tr.ViewRestartOptions,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.CustomCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.BulkCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.ExecShell),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesExecShell,
		Description: gui.Tr.ExecShell,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         safeGetKey(opts.Config.Services.OpenInBrowser),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesOpenInBrowserCommand,
		Description: gui.Tr.OpenInBrowser,
	})

	// Images bindings
	bindings = append(bindings, &Binding{
		ViewName:    "images",
		Key:         safeGetKey(opts.Config.Images.CustomCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleImagesCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "images",
		Key:         safeGetKey(opts.Config.Images.Remove),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleImagesRemoveMenu,
		Description: gui.Tr.RemoveImage,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "images",
		Key:         safeGetKey(opts.Config.Images.BulkCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleImagesBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	// Volumes bindings
	bindings = append(bindings, &Binding{
		ViewName:    "volumes",
		Key:         safeGetKey(opts.Config.Volumes.CustomCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleVolumesCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "volumes",
		Key:         safeGetKey(opts.Config.Volumes.Remove),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleVolumesRemoveMenu,
		Description: gui.Tr.RemoveVolume,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "volumes",
		Key:         safeGetKey(opts.Config.Volumes.BulkCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleVolumesBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	// Networks bindings
	bindings = append(bindings, &Binding{
		ViewName:    "networks",
		Key:         safeGetKey(opts.Config.Networks.CustomCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleNetworksCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "networks",
		Key:         safeGetKey(opts.Config.Networks.Remove),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleNetworksRemoveMenu,
		Description: gui.Tr.RemoveNetwork,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "networks",
		Key:         safeGetKey(opts.Config.Networks.BulkCommand),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleNetworksBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	// Main panel bindings
	bindings = append(bindings, &Binding{
		ViewName:    "main",
		Key:         safeGetKey(opts.Config.Main.Return),
		Modifier:    gocui.ModNone,
		Handler:     gui.handleExitMain,
		Description: gui.Tr.Return,
	})

	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      safeGetKey(opts.Config.Main.ScrollLeft),
		Modifier: gocui.ModNone,
		Handler:  gui.scrollLeftMain,
	})

	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      safeGetKey(opts.Config.Main.ScrollRight),
		Modifier: gocui.ModNone,
		Handler:  gui.scrollRightMain,
	})

	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      safeGetKey(opts.Config.Main.ScrollLeftAlt),
		Modifier: gocui.ModNone,
		Handler:  gui.scrollLeftMain,
	})

	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      safeGetKey(opts.Config.Main.ScrollRightAlt),
		Modifier: gocui.ModNone,
		Handler:  gui.scrollRightMain,
	})

	// Filter bindings
	bindings = append(bindings, &Binding{
		ViewName: "filter",
		Key:      safeGetKey(opts.Config.Filter.Confirm),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.commitFilter),
	})

	bindings = append(bindings, &Binding{
		ViewName: "filter",
		Key:      safeGetKey(opts.Config.Filter.Escape),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.escapeFilterPrompt),
	})

	// Additional universal bindings
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollDownMainAlt2),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollDownMain),
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollUpMainAlt2),
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollUpMain),
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollLeftMain),
		Modifier: gocui.ModNone,
		Handler:  gui.scrollLeftMain,
	})

	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      safeGetKey(opts.Config.Universal.ScrollRightMain),
		Modifier: gocui.ModNone,
		Handler:  gui.scrollRightMain,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "",
		Key:         safeGetKey(opts.Config.Universal.NextScreenMode),
		Modifier:    gocui.ModNone,
		Handler:     wrappedHandler(gui.nextScreenMode),
		Description: gui.Tr.LcNextScreenMode,
	})

	bindings = append(bindings, &Binding{
		ViewName:    "",
		Key:         safeGetKey(opts.Config.Universal.PrevScreenMode),
		Modifier:    gocui.ModNone,
		Handler:     wrappedHandler(gui.prevScreenMode),
		Description: gui.Tr.LcPrevScreenMode,
	})

	// Panel navigation bindings
	prevPanelKey := safeGetKey(opts.Config.Universal.PrevPanel)
	nextPanelKey := safeGetKey(opts.Config.Universal.NextPanel)
	prevPanelAltKey := safeGetKey(opts.Config.Universal.PrevPanelAlt)
	nextPanelAltKey := safeGetKey(opts.Config.Universal.NextPanelAlt)
	togglePanelKey := safeGetKey(opts.Config.Universal.TogglePanel)
	togglePanelAltKey := safeGetKey(opts.Config.Universal.TogglePanelAlt)

	for _, panel := range gui.allSidePanels() {
		bindings = append(bindings, []*Binding{
			{ViewName: panel.GetView().Name(), Key: prevPanelKey, Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: panel.GetView().Name(), Key: nextPanelKey, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: prevPanelAltKey, Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: panel.GetView().Name(), Key: nextPanelAltKey, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: togglePanelKey, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: togglePanelAltKey, Modifier: gocui.ModNone, Handler: gui.previousView},
		}...)
	}

	// Up/Down/Click bindings keys
	prevItemAltKey := safeGetKey(opts.Config.Universal.PrevItemAlt)
	prevItemKey := safeGetKey(opts.Config.Universal.PrevItem)
	nextItemAltKey := safeGetKey(opts.Config.Universal.NextItemAlt)
	nextItemKey := safeGetKey(opts.Config.Universal.NextItem)

	setUpDownClickBindings := func(viewName string, onUp func() error, onDown func() error, onClick func() error) {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Key: prevItemAltKey, Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: prevItemKey, Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: gocui.MouseWheelUp, Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: nextItemAltKey, Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: nextItemKey, Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: gocui.MouseWheelDown, Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: gocui.MouseLeft, Modifier: gocui.ModNone, Handler: wrappedHandler(onClick)},
		}...)
	}

	// GoTo panel bindings
	goToProjectKey := safeGetKey(opts.Config.Universal.GoToProject)
	goToServicesKey := safeGetKey(opts.Config.Universal.GoToServices)
	goToContainersKey := safeGetKey(opts.Config.Universal.GoToContainers)
	goToImagesKey := safeGetKey(opts.Config.Universal.GoToImages)
	goToVolumesKey := safeGetKey(opts.Config.Universal.GoToVolumes)
	goToNetworksKey := safeGetKey(opts.Config.Universal.GoToNetworks)

	bindings = append(bindings, []*Binding{
		{Handler: gui.handleGoTo(gui.Panels.Projects.View), Key: goToProjectKey, Description: gui.Tr.FocusProjects},
		{Handler: gui.handleGoTo(gui.Panels.Services.View), Key: goToServicesKey, Description: gui.Tr.FocusServices},
		{Handler: gui.handleGoTo(gui.Panels.Containers.View), Key: goToContainersKey, Description: gui.Tr.FocusContainers},
		{Handler: gui.handleGoTo(gui.Panels.Images.View), Key: goToImagesKey, Description: gui.Tr.FocusImages},
		{Handler: gui.handleGoTo(gui.Panels.Volumes.View), Key: goToVolumesKey, Description: gui.Tr.FocusVolumes},
		{Handler: gui.handleGoTo(gui.Panels.Networks.View), Key: goToNetworksKey, Description: gui.Tr.FocusNetworks},
	}...)

	for _, panel := range gui.allListPanels() {
		setUpDownClickBindings(panel.GetView().Name(), panel.HandlePrevLine, panel.HandleNextLine, panel.HandleClick)
	}

	setUpDownClickBindings("main", gui.scrollUpMain, gui.scrollDownMain, gui.handleMainClick)

	// Side panel main/tab bindings
	enterMainKey := safeGetKey(opts.Config.Universal.EnterMain)
	prevMainTabKey := safeGetKey(opts.Config.Universal.PrevMainTab)
	nextMainTabKey := safeGetKey(opts.Config.Universal.NextMainTab)

	for _, panel := range gui.allSidePanels() {
		bindings = append(bindings,
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         enterMainKey,
				Modifier:    gocui.ModNone,
				Handler:     gui.handleEnterMain,
				Description: gui.Tr.FocusMain,
			},
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         prevMainTabKey,
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(panel.HandlePrevMainTab),
				Description: gui.Tr.PreviousContext,
			},
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         nextMainTabKey,
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(panel.HandleNextMainTab),
				Description: gui.Tr.NextContext,
			},
		)
	}

	// Filter bindings for list panels
	filterKey := safeGetKey(opts.Config.Universal.Filter)

	for _, panel := range gui.allListPanels() {
		if !panel.IsFilterDisabled() {
			bindings = append(bindings, &Binding{
				ViewName:    panel.GetView().Name(),
				Key:         filterKey,
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(gui.handleOpenFilter),
				Description: gui.Tr.LcFilter,
			})
		}
	}

	return bindings, nil
}

func (gui *Gui) keybindings(g *gocui.Gui) error {
	opts := gui.KeybindingOpts()
	bindings, err := gui.GetInitialKeybindings(opts)
	if err != nil {
		return err
	}

	// Filter out disabled bindings (nil keys) explicitly
	// Don't rely on undocumented gocui behavior
	var activeBindings []*Binding
	for _, binding := range bindings {
		if binding.Key != nil {
			activeBindings = append(activeBindings, binding)
		}
	}

	// Register only active bindings
	for _, binding := range activeBindings {
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
