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
	// Helper function to get key and handle errors inline
	getKey := func(keyStr string) (interface{}, error) {
		key, err := keybindings.GetKey(keyStr)
		if err != nil {
			return nil, fmt.Errorf("invalid keybinding '%s': %w", keyStr, err)
		}
		return key, nil
	}

	var bindings []*Binding
	var err error
	var key interface{}

	// Universal bindings
	if key, err = getKey(opts.Config.Universal.Return); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.escape),
	})

	if key, err = getKey(opts.Config.Universal.Quit); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.quit,
	})

	if key, err = getKey(opts.Config.Universal.QuitAlt); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.quit,
	})

	if key, err = getKey(opts.Config.Universal.ScrollUpMain); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollUpMain),
	})

	if key, err = getKey(opts.Config.Universal.ScrollDownMain); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollDownMain),
	})

	if key, err = getKey(opts.Config.Universal.ScrollUpMainAlt1); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollUpMain),
	})

	if key, err = getKey(opts.Config.Universal.ScrollDownMainAlt1); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollDownMain),
	})

	if key, err = getKey(opts.Config.Universal.AutoScrollMain); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.autoScrollMain,
	})

	if key, err = getKey(opts.Config.Universal.JumpToTopMain); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.jumpToTopMain,
	})

	if key, err = getKey(opts.Config.Universal.OpenMenu); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.handleCreateOptionsMenu,
	})

	if key, err = getKey(opts.Config.Universal.OpenMenuAlt); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.handleCreateOptionsMenu,
	})

	if key, err = getKey(opts.Config.Universal.CustomCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.handleCustomCommand,
	})

	// Project bindings
	if key, err = getKey(opts.Config.Project.EditConfig); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "project",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleEditConfig,
		Description: gui.Tr.EditConfig,
	})

	if key, err = getKey(opts.Config.Project.OpenConfig); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "project",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleOpenConfig,
		Description: gui.Tr.OpenConfig,
	})

	if key, err = getKey(opts.Config.Project.ViewLogs); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "project",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleViewAllLogs,
		Description: gui.Tr.ViewLogs,
	})

	// Menu bindings
	if key, err = getKey(opts.Config.Menu.Close); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuClose),
	})

	if key, err = getKey(opts.Config.Menu.CloseAlt); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuClose),
	})

	if key, err = getKey(opts.Config.Menu.Select); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuPress),
	})

	if key, err = getKey(opts.Config.Menu.Confirm); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.handleMenuPress),
	})

	if key, err = getKey(opts.Config.Menu.SelectAlt); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "menu",
		Key:      key,
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
	if key, err = getKey(opts.Config.Containers.Remove); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersRemoveMenu,
		Description: gui.Tr.Remove,
	})

	if key, err = getKey(opts.Config.Containers.HideStopped); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleHideStoppedContainers,
		Description: gui.Tr.HideStopped,
	})

	if key, err = getKey(opts.Config.Containers.Pause); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerPause,
		Description: gui.Tr.Pause,
	})

	if key, err = getKey(opts.Config.Containers.Stop); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerStop,
		Description: gui.Tr.Stop,
	})

	if key, err = getKey(opts.Config.Containers.Restart); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerRestart,
		Description: gui.Tr.Restart,
	})

	if key, err = getKey(opts.Config.Containers.Attach); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerAttach,
		Description: gui.Tr.Attach,
	})

	if key, err = getKey(opts.Config.Containers.ViewLogs); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainerViewLogs,
		Description: gui.Tr.ViewLogs,
	})

	if key, err = getKey(opts.Config.Containers.ExecShell); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersExecShell,
		Description: gui.Tr.ExecShell,
	})

	if key, err = getKey(opts.Config.Containers.CustomCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	if key, err = getKey(opts.Config.Containers.BulkCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	if key, err = getKey(opts.Config.Containers.OpenInBrowser); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "containers",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleContainersOpenInBrowserCommand,
		Description: gui.Tr.OpenInBrowser,
	})

	// Services bindings
	if key, err = getKey(opts.Config.Services.Up); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceUp,
		Description: gui.Tr.UpService,
	})

	if key, err = getKey(opts.Config.Services.Remove); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRemoveMenu,
		Description: gui.Tr.RemoveService,
	})

	if key, err = getKey(opts.Config.Services.Stop); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceStop,
		Description: gui.Tr.Stop,
	})

	if key, err = getKey(opts.Config.Services.Pause); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicePause,
		Description: gui.Tr.Pause,
	})

	if key, err = getKey(opts.Config.Services.Restart); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRestart,
		Description: gui.Tr.Restart,
	})

	if key, err = getKey(opts.Config.Services.Start); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceStart,
		Description: gui.Tr.Start,
	})

	if key, err = getKey(opts.Config.Services.Attach); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceAttach,
		Description: gui.Tr.Attach,
	})

	if key, err = getKey(opts.Config.Services.ViewLogs); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRenderLogsToMain,
		Description: gui.Tr.ViewLogs,
	})

	if key, err = getKey(opts.Config.Services.UpProject); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleProjectUp,
		Description: gui.Tr.UpProject,
	})

	if key, err = getKey(opts.Config.Services.DownProject); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleProjectDown,
		Description: gui.Tr.DownProject,
	})

	if key, err = getKey(opts.Config.Services.RestartMenu); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServiceRestartMenu,
		Description: gui.Tr.ViewRestartOptions,
	})

	if key, err = getKey(opts.Config.Services.CustomCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	if key, err = getKey(opts.Config.Services.BulkCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	if key, err = getKey(opts.Config.Services.ExecShell); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesExecShell,
		Description: gui.Tr.ExecShell,
	})

	if key, err = getKey(opts.Config.Services.OpenInBrowser); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "services",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleServicesOpenInBrowserCommand,
		Description: gui.Tr.OpenInBrowser,
	})

	// Images bindings
	if key, err = getKey(opts.Config.Images.CustomCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "images",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleImagesCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	if key, err = getKey(opts.Config.Images.Remove); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "images",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleImagesRemoveMenu,
		Description: gui.Tr.RemoveImage,
	})

	if key, err = getKey(opts.Config.Images.BulkCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "images",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleImagesBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	// Volumes bindings
	if key, err = getKey(opts.Config.Volumes.CustomCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "volumes",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleVolumesCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	if key, err = getKey(opts.Config.Volumes.Remove); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "volumes",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleVolumesRemoveMenu,
		Description: gui.Tr.RemoveVolume,
	})

	if key, err = getKey(opts.Config.Volumes.BulkCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "volumes",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleVolumesBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	// Networks bindings
	if key, err = getKey(opts.Config.Networks.CustomCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "networks",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleNetworksCustomCommand,
		Description: gui.Tr.RunCustomCommand,
	})

	if key, err = getKey(opts.Config.Networks.Remove); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "networks",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleNetworksRemoveMenu,
		Description: gui.Tr.RemoveNetwork,
	})

	if key, err = getKey(opts.Config.Networks.BulkCommand); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "networks",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleNetworksBulkCommand,
		Description: gui.Tr.ViewBulkCommands,
	})

	// Main panel bindings
	if key, err = getKey(opts.Config.Main.Return); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "main",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     gui.handleExitMain,
		Description: gui.Tr.Return,
	})

	if key, err = getKey(opts.Config.Main.ScrollLeft); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.scrollLeftMain,
	})

	if key, err = getKey(opts.Config.Main.ScrollRight); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.scrollRightMain,
	})

	if key, err = getKey(opts.Config.Main.ScrollLeftAlt); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.scrollLeftMain,
	})

	if key, err = getKey(opts.Config.Main.ScrollRightAlt); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "main",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.scrollRightMain,
	})

	// Filter bindings
	if key, err = getKey(opts.Config.Filter.Confirm); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "filter",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.commitFilter),
	})

	if key, err = getKey(opts.Config.Filter.Escape); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "filter",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.escapeFilterPrompt),
	})

	// Additional universal bindings
	if key, err = getKey(opts.Config.Universal.ScrollDownMainAlt2); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollDownMain),
	})

	if key, err = getKey(opts.Config.Universal.ScrollUpMainAlt2); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  wrappedHandler(gui.scrollUpMain),
	})

	if key, err = getKey(opts.Config.Universal.ScrollLeftMain); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.scrollLeftMain,
	})

	if key, err = getKey(opts.Config.Universal.ScrollRightMain); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName: "",
		Key:      key,
		Modifier: gocui.ModNone,
		Handler:  gui.scrollRightMain,
	})

	if key, err = getKey(opts.Config.Universal.NextScreenMode); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     wrappedHandler(gui.nextScreenMode),
		Description: gui.Tr.LcNextScreenMode,
	})

	if key, err = getKey(opts.Config.Universal.PrevScreenMode); err != nil {
		return nil, err
	}
	bindings = append(bindings, &Binding{
		ViewName:    "",
		Key:         key,
		Modifier:    gocui.ModNone,
		Handler:     wrappedHandler(gui.prevScreenMode),
		Description: gui.Tr.LcPrevScreenMode,
	})

	// Panel navigation bindings
	var prevPanelKey, nextPanelKey, prevPanelAltKey, nextPanelAltKey, togglePanelKey, togglePanelAltKey interface{}

	if prevPanelKey, err = getKey(opts.Config.Universal.PrevPanel); err != nil {
		return nil, err
	}
	if nextPanelKey, err = getKey(opts.Config.Universal.NextPanel); err != nil {
		return nil, err
	}
	if prevPanelAltKey, err = getKey(opts.Config.Universal.PrevPanelAlt); err != nil {
		return nil, err
	}
	if nextPanelAltKey, err = getKey(opts.Config.Universal.NextPanelAlt); err != nil {
		return nil, err
	}
	if togglePanelKey, err = getKey(opts.Config.Universal.TogglePanel); err != nil {
		return nil, err
	}
	if togglePanelAltKey, err = getKey(opts.Config.Universal.TogglePanelAlt); err != nil {
		return nil, err
	}

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
	var prevItemAltKey, prevItemKey, nextItemAltKey, nextItemKey interface{}

	if prevItemAltKey, err = getKey(opts.Config.Universal.PrevItemAlt); err != nil {
		return nil, err
	}
	if prevItemKey, err = getKey(opts.Config.Universal.PrevItem); err != nil {
		return nil, err
	}
	if nextItemAltKey, err = getKey(opts.Config.Universal.NextItemAlt); err != nil {
		return nil, err
	}
	if nextItemKey, err = getKey(opts.Config.Universal.NextItem); err != nil {
		return nil, err
	}

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
	var goToProjectKey, goToServicesKey, goToContainersKey, goToImagesKey, goToVolumesKey, goToNetworksKey interface{}

	if goToProjectKey, err = getKey(opts.Config.Universal.GoToProject); err != nil {
		return nil, err
	}
	if goToServicesKey, err = getKey(opts.Config.Universal.GoToServices); err != nil {
		return nil, err
	}
	if goToContainersKey, err = getKey(opts.Config.Universal.GoToContainers); err != nil {
		return nil, err
	}
	if goToImagesKey, err = getKey(opts.Config.Universal.GoToImages); err != nil {
		return nil, err
	}
	if goToVolumesKey, err = getKey(opts.Config.Universal.GoToVolumes); err != nil {
		return nil, err
	}
	if goToNetworksKey, err = getKey(opts.Config.Universal.GoToNetworks); err != nil {
		return nil, err
	}

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
	var enterMainKey, prevMainTabKey, nextMainTabKey interface{}

	if enterMainKey, err = getKey(opts.Config.Universal.EnterMain); err != nil {
		return nil, err
	}
	if prevMainTabKey, err = getKey(opts.Config.Universal.PrevMainTab); err != nil {
		return nil, err
	}
	if nextMainTabKey, err = getKey(opts.Config.Universal.NextMainTab); err != nil {
		return nil, err
	}

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
	var filterKey interface{}

	if filterKey, err = getKey(opts.Config.Universal.Filter); err != nil {
		return nil, err
	}

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
