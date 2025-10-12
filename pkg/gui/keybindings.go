package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
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

// GetInitialKeybindings is a function.
func (gui *Gui) GetInitialKeybindings(opts types.KeybindingsOpts) []*Binding {
	bindings := []*Binding{
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.Return),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.escape),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.Quit),
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.QuitAlt),
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollUpMain),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollUpMain),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollDownMain),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollDownMain),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollUpMainAlt1),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollUpMain),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollDownMainAlt1),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollDownMain),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.AutoScrollMain),
			Modifier: gocui.ModNone,
			Handler:  gui.autoScrollMain,
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.JumpToTopMain),
			Modifier: gocui.ModNone,
			Handler:  gui.jumpToTopMain,
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.OpenMenu),
			Modifier: gocui.ModNone,
			Handler:  gui.handleCreateOptionsMenu,
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.OpenMenuAlt),
			Modifier: gocui.ModNone,
			Handler:  gui.handleCreateOptionsMenu,
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.CustomCommand),
			Modifier: gocui.ModNone,
			Handler:  gui.handleCustomCommand,
		},
		{
			ViewName:    "project",
			Key:         opts.GetKey(opts.Config.Project.EditConfig),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleEditConfig,
			Description: gui.Tr.EditConfig,
		},
		{
			ViewName:    "project",
			Key:         opts.GetKey(opts.Config.Project.OpenConfig),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleOpenConfig,
			Description: gui.Tr.OpenConfig,
		},
		{
			ViewName:    "project",
			Key:         opts.GetKey(opts.Config.Project.ViewLogs),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleViewAllLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName: "menu",
			Key:      opts.GetKey(opts.Config.Menu.Close),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuClose),
		},
		{
			ViewName: "menu",
			Key:      opts.GetKey(opts.Config.Menu.CloseAlt),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuClose),
		},
		{
			ViewName: "menu",
			Key:      opts.GetKey(opts.Config.Menu.Select),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuPress),
		},
		{
			ViewName: "menu",
			Key:      opts.GetKey(opts.Config.Menu.Confirm),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.handleMenuPress),
		},
		{
			ViewName: "menu",
			Key:      opts.GetKey(opts.Config.Menu.SelectAlt),
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
			Key:         opts.GetKey(opts.Config.Containers.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersRemoveMenu,
			Description: gui.Tr.Remove,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.HideStopped),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleHideStoppedContainers,
			Description: gui.Tr.HideStopped,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.Pause),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerPause,
			Description: gui.Tr.Pause,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.Stop),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerStop,
			Description: gui.Tr.Stop,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.Restart),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerRestart,
			Description: gui.Tr.Restart,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.Attach),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerAttach,
			Description: gui.Tr.Attach,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.ViewLogs),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerViewLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.ExecShell),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersExecShell,
			Description: gui.Tr.ExecShell,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.CustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.BulkCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "containers",
			Key:         opts.GetKey(opts.Config.Containers.OpenInBrowser),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersOpenInBrowserCommand,
			Description: gui.Tr.OpenInBrowser,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.Up),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceUp,
			Description: gui.Tr.UpService,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRemoveMenu,
			Description: gui.Tr.RemoveService,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.Stop),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceStop,
			Description: gui.Tr.Stop,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.Pause),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicePause,
			Description: gui.Tr.Pause,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.Restart),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRestart,
			Description: gui.Tr.Restart,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.Start),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceStart,
			Description: gui.Tr.Start,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.Attach),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceAttach,
			Description: gui.Tr.Attach,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.ViewLogs),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRenderLogsToMain,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.UpProject),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectUp,
			Description: gui.Tr.UpProject,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.DownProject),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectDown,
			Description: gui.Tr.DownProject,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.RestartMenu),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRestartMenu,
			Description: gui.Tr.ViewRestartOptions,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.CustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.BulkCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.ExecShell),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesExecShell,
			Description: gui.Tr.ExecShell,
		},
		{
			ViewName:    "services",
			Key:         opts.GetKey(opts.Config.Services.OpenInBrowser),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesOpenInBrowserCommand,
			Description: gui.Tr.OpenInBrowser,
		},
		{
			ViewName:    "images",
			Key:         opts.GetKey(opts.Config.Images.CustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "images",
			Key:         opts.GetKey(opts.Config.Images.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesRemoveMenu,
			Description: gui.Tr.RemoveImage,
		},
		{
			ViewName:    "images",
			Key:         opts.GetKey(opts.Config.Images.BulkCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "volumes",
			Key:         opts.GetKey(opts.Config.Volumes.CustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "volumes",
			Key:         opts.GetKey(opts.Config.Volumes.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesRemoveMenu,
			Description: gui.Tr.RemoveVolume,
		},
		{
			ViewName:    "volumes",
			Key:         opts.GetKey(opts.Config.Volumes.BulkCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "networks",
			Key:         opts.GetKey(opts.Config.Networks.CustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleNetworksCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "networks",
			Key:         opts.GetKey(opts.Config.Networks.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleNetworksRemoveMenu,
			Description: gui.Tr.RemoveNetwork,
		},
		{
			ViewName:    "networks",
			Key:         opts.GetKey(opts.Config.Networks.BulkCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleNetworksBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "main",
			Key:         opts.GetKey(opts.Config.Main.Return),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleExitMain,
			Description: gui.Tr.Return,
		},
		{
			ViewName: "main",
			Key:      opts.GetKey(opts.Config.Main.ScrollLeft),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "main",
			Key:      opts.GetKey(opts.Config.Main.ScrollRight),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
		{
			ViewName: "main",
			Key:      opts.GetKey(opts.Config.Main.ScrollLeftAlt),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "main",
			Key:      opts.GetKey(opts.Config.Main.ScrollRightAlt),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
		{
			ViewName: "filter",
			Key:      opts.GetKey(opts.Config.Filter.Confirm),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.commitFilter),
		},
		{
			ViewName: "filter",
			Key:      opts.GetKey(opts.Config.Filter.Escape),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.escapeFilterPrompt),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollDownMainAlt2),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollDownMain),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollUpMainAlt2),
			Modifier: gocui.ModNone,
			Handler:  wrappedHandler(gui.scrollUpMain),
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollLeftMain),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "",
			Key:      opts.GetKey(opts.Config.Universal.ScrollRightMain),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
		{
			ViewName:    "",
			Key:         opts.GetKey(opts.Config.Universal.NextScreenMode),
			Handler:     wrappedHandler(gui.nextScreenMode),
			Description: gui.Tr.LcNextScreenMode,
		},
		{
			ViewName:    "",
			Key:         opts.GetKey(opts.Config.Universal.PrevScreenMode),
			Handler:     wrappedHandler(gui.prevScreenMode),
			Description: gui.Tr.LcPrevScreenMode,
		},
	}

	for _, panel := range gui.allSidePanels() {
		bindings = append(bindings, []*Binding{
			{ViewName: panel.GetView().Name(), Key: opts.GetKey(opts.Config.Universal.PrevPanel), Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: panel.GetView().Name(), Key: opts.GetKey(opts.Config.Universal.NextPanel), Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: opts.GetKey(opts.Config.Universal.PrevPanelAlt), Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: panel.GetView().Name(), Key: opts.GetKey(opts.Config.Universal.NextPanelAlt), Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: opts.GetKey(opts.Config.Universal.TogglePanel), Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: panel.GetView().Name(), Key: opts.GetKey(opts.Config.Universal.TogglePanelAlt), Modifier: gocui.ModNone, Handler: gui.previousView},
		}...)
	}

	setUpDownClickBindings := func(viewName string, onUp func() error, onDown func() error, onClick func() error) {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Key: opts.GetKey(opts.Config.Universal.PrevItemAlt), Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: opts.GetKey(opts.Config.Universal.PrevItem), Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: gocui.MouseWheelUp, Modifier: gocui.ModNone, Handler: wrappedHandler(onUp)},
			{ViewName: viewName, Key: opts.GetKey(opts.Config.Universal.NextItemAlt), Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: opts.GetKey(opts.Config.Universal.NextItem), Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: gocui.MouseWheelDown, Modifier: gocui.ModNone, Handler: wrappedHandler(onDown)},
			{ViewName: viewName, Key: gocui.MouseLeft, Modifier: gocui.ModNone, Handler: wrappedHandler(onClick)},
		}...)
	}

	bindings = append(bindings, []*Binding{
		{Handler: gui.handleGoTo(gui.Panels.Projects.View), Key: opts.GetKey(opts.Config.Universal.GoToProject), Description: gui.Tr.FocusProjects},
		{Handler: gui.handleGoTo(gui.Panels.Services.View), Key: opts.GetKey(opts.Config.Universal.GoToServices), Description: gui.Tr.FocusServices},
		{Handler: gui.handleGoTo(gui.Panels.Containers.View), Key: opts.GetKey(opts.Config.Universal.GoToContainers), Description: gui.Tr.FocusContainers},
		{Handler: gui.handleGoTo(gui.Panels.Images.View), Key: opts.GetKey(opts.Config.Universal.GoToImages), Description: gui.Tr.FocusImages},
		{Handler: gui.handleGoTo(gui.Panels.Volumes.View), Key: opts.GetKey(opts.Config.Universal.GoToVolumes), Description: gui.Tr.FocusVolumes},
		{Handler: gui.handleGoTo(gui.Panels.Networks.View), Key: opts.GetKey(opts.Config.Universal.GoToNetworks), Description: gui.Tr.FocusNetworks},
	}...)

	for _, panel := range gui.allListPanels() {
		setUpDownClickBindings(panel.GetView().Name(), panel.HandlePrevLine, panel.HandleNextLine, panel.HandleClick)
	}

	setUpDownClickBindings("main", gui.scrollUpMain, gui.scrollDownMain, gui.handleMainClick)

	for _, panel := range gui.allSidePanels() {
		bindings = append(bindings,
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         opts.GetKey(opts.Config.Universal.EnterMain),
				Modifier:    gocui.ModNone,
				Handler:     gui.handleEnterMain,
				Description: gui.Tr.FocusMain,
			},
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         opts.GetKey(opts.Config.Universal.PrevMainTab),
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(panel.HandlePrevMainTab),
				Description: gui.Tr.PreviousContext,
			},
			&Binding{
				ViewName:    panel.GetView().Name(),
				Key:         opts.GetKey(opts.Config.Universal.NextMainTab),
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
				Key:         opts.GetKey(opts.Config.Universal.Filter),
				Modifier:    gocui.ModNone,
				Handler:     wrappedHandler(gui.handleOpenFilter),
				Description: gui.Tr.LcFilter,
			})
		}
	}

	return bindings
}

func (gui *Gui) keybindings(g *gocui.Gui) error {
	opts := gui.KeybindingOpts()
	bindings := gui.GetInitialKeybindings(opts)

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
