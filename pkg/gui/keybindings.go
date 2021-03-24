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

// GetDisplayStrings returns the display string of a file
func (b *Binding) GetDisplayStrings(isFocused bool) []string {
	return []string{b.GetKey(), b.Description}
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
			Key:      gocui.KeyEsc,
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Key:      gocui.KeyPgup,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollUpMain,
		},
		{
			ViewName: "",
			Key:      gocui.KeyPgdn,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollDownMain,
		},
		{
			ViewName: "",
			Key:      gocui.KeyCtrlU,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollUpMain,
		},
		{
			ViewName: "",
			Key:      gocui.KeyCtrlD,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollDownMain,
		},
		{
			ViewName: "",
			Key:      gocui.KeyEnd,
			Modifier: gocui.ModNone,
			Handler:  gui.autoScrollMain,
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
			Key:         '[',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "project",
			Key:         ']',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectNextContext,
			Description: gui.Tr.NextContext,
		},
		{
			ViewName: "project",
			Key:      gocui.MouseLeft,
			Modifier: gocui.ModNone,
			Handler:  gui.handleProjectClick,
		},
		{
			ViewName:    "project",
			Key:         'm',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleViewAllLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName: "project",
			Key:      gocui.MouseLeft,
			Modifier: gocui.ModNone,
			Handler:  gui.handleProjectSelect,
		},
		{
			ViewName: "menu",
			Key:      gocui.KeyEsc,
			Modifier: gocui.ModNone,
			Handler:  gui.handleMenuClose,
		},
		{
			ViewName: "menu",
			Key:      'q',
			Modifier: gocui.ModNone,
			Handler:  gui.handleMenuClose,
		},
		{
			ViewName: "information",
			Key:      gocui.MouseLeft,
			Modifier: gocui.ModNone,
			Handler:  gui.handleDonate,
		},
		{
			ViewName:    "containers",
			Key:         '[',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "containers",
			Key:         ']',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersNextContext,
			Description: gui.Tr.NextContext,
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
			Key:         'r',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRestart,
			Description: gui.Tr.Restart,
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
			Handler:     gui.handleServiceViewLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName:    "services",
			Key:         '[',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "services",
			Key:         ']',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesNextContext,
			Description: gui.Tr.NextContext,
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
			ViewName:    "images",
			Key:         '[',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "images",
			Key:         ']',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesNextContext,
			Description: gui.Tr.NextContext,
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
			Key:         '[',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "volumes",
			Key:         ']',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesNextContext,
			Description: gui.Tr.NextContext,
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
			ViewName: "",
			Key:      'J',
			Modifier: gocui.ModNone,
			Handler:  gui.scrollDownMain,
		},
		{
			ViewName: "",
			Key:      'K',
			Modifier: gocui.ModNone,
			Handler:  gui.scrollUpMain,
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
	}

	// TODO: add more views here
	for _, viewName := range []string{"project", "services", "containers", "images", "volumes", "menu"} {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Key: gocui.KeyArrowLeft, Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: viewName, Key: gocui.KeyArrowRight, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: viewName, Key: 'h', Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: viewName, Key: 'l', Modifier: gocui.ModNone, Handler: gui.nextView},
		}...)
	}

	panelMap := map[string]struct {
		onKeyUpPress   func(*gocui.Gui, *gocui.View) error
		onKeyDownPress func(*gocui.Gui, *gocui.View) error
		onClick        func(*gocui.Gui, *gocui.View) error
	}{
		"menu":       {onKeyUpPress: gui.handleMenuPrevLine, onKeyDownPress: gui.handleMenuNextLine, onClick: gui.handleMenuClick},
		"services":   {onKeyUpPress: gui.handleServicesPrevLine, onKeyDownPress: gui.handleServicesNextLine, onClick: gui.handleServicesClick},
		"containers": {onKeyUpPress: gui.handleContainersPrevLine, onKeyDownPress: gui.handleContainersNextLine, onClick: gui.handleContainersClick},
		"images":     {onKeyUpPress: gui.handleImagesPrevLine, onKeyDownPress: gui.handleImagesNextLine, onClick: gui.handleImagesClick},
		"volumes":    {onKeyUpPress: gui.handleVolumesPrevLine, onKeyDownPress: gui.handleVolumesNextLine, onClick: gui.handleVolumesClick},
		"main":       {onKeyUpPress: gui.scrollUpMain, onKeyDownPress: gui.scrollDownMain, onClick: gui.handleMainClick},
	}

	for viewName, functions := range panelMap {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Key: 'k', Modifier: gocui.ModNone, Handler: functions.onKeyUpPress},
			{ViewName: viewName, Key: gocui.KeyArrowUp, Modifier: gocui.ModNone, Handler: functions.onKeyUpPress},
			{ViewName: viewName, Key: gocui.MouseWheelUp, Modifier: gocui.ModNone, Handler: functions.onKeyUpPress},
			{ViewName: viewName, Key: 'j', Modifier: gocui.ModNone, Handler: functions.onKeyDownPress},
			{ViewName: viewName, Key: gocui.KeyArrowDown, Modifier: gocui.ModNone, Handler: functions.onKeyDownPress},
			{ViewName: viewName, Key: gocui.MouseWheelDown, Modifier: gocui.ModNone, Handler: functions.onKeyDownPress},
			{ViewName: viewName, Key: gocui.MouseLeft, Modifier: gocui.ModNone, Handler: functions.onClick},
		}...)
	}

	for _, viewName := range []string{"project", "services", "containers", "images", "volumes"} {
		bindings = append(bindings, &Binding{
			ViewName:    viewName,
			Key:         gocui.KeyEnter,
			Modifier:    gocui.ModNone,
			Handler:     gui.handleEnterMain,
			Description: gui.Tr.FocusMain,
		})
	}

	return bindings
}

func (gui *Gui) keybindings(g *gocui.Gui) error {
	bindings := gui.GetInitialKeybindings()

	for _, binding := range bindings {
		if err := g.SetKeybinding(binding.ViewName, nil, binding.Key, binding.Modifier, binding.Handler); err != nil {
			return err
		}
	}

	if err := g.SetTabClickBinding("main", gui.onMainTabClick); err != nil {
		return err
	}

	return nil
}
