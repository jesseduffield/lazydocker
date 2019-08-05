package gui

import (
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/keybinding"
)

// Binding - a keybinding mapping a key and modifier to a handler. The keypress
// is only handled if the given view has focus, or handled globally if the view
// is ""
type Binding struct {
	ViewName    string
	Handler     func(*gocui.Gui, *gocui.View) error
	Keys        []gocui.Key
	Modifier    gocui.Modifier
	Description string
}

// GetDisplayStrings returns the display string of a file
func (b *Binding) GetDisplayStrings(isFocused bool) []string {
	return []string{b.GetKey(), b.Description}
}

// GetKey is a function.
func (b *Binding) GetKey() string {

	result := ""
	for _, k := range b.Keys {

		// special keys
		switch k {
		case 27:
			result += "esc, "
		case 13:
			result += "enter, "
		case 32:
			result += "space, "
		case 65514:
			result += "►, "
		case 65515:
			result += "◄, "
		case 65517:
			result += "▲, "
		case 65516:
			result += "▼, "
		case 65508:
			result += "PgUp, "
		case 65507:
			result += "PgDn, "
		}
	}

	return result[:len(result)-2]
}

// GetInitialKeybindings is a function.
func (gui *Gui) GetInitialKeybindings() []*Binding {
	bindings := []*Binding{
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.Quit),
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.ScrollUpMain),
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.ScrollDownMain),
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		},
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.ScrollLeftMain),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.ScrollRightMain),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.AutoScrollMain),
			Modifier: gocui.ModNone,
			Handler:  gui.autoScrollMain,
		},
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.ShowOptionsMenu),
			Modifier: gocui.ModNone,
			Handler:  gui.handleCreateOptionsMenu,
		},
		{
			ViewName: "",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.CustomCommand),
			Modifier: gocui.ModNone,
			Handler:  gui.handleCustomCommand, // Might overlap with the other run custom command configs
		},
		{
			ViewName:    "project",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Project.EditConfig),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleEditConfig,
			Description: gui.Tr.EditConfig,
		},
		{
			ViewName:    "project",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Project.OpenConfig),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleOpenConfig,
			Description: gui.Tr.OpenConfig,
		},
		{
			ViewName:    "project",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.PreviousContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "project",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.NextContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleProjectNextContext,
			Description: gui.Tr.NextContext,
		},
		{
			ViewName: "project",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.Project.Click),
			Modifier: gocui.ModNone,
			Handler:  gui.handleProjectClick, // Possbile dub with select
		},
		{
			ViewName:    "project",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Project.ViewLogs),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleViewAllLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName: "project",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.Project.Select),
			Modifier: gocui.ModNone,
			Handler:  gui.handleProjectSelect, // Possible dub with click
		},
		{
			ViewName: "menu",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.Menu.Close),
			Modifier: gocui.ModNone,
			Handler:  gui.handleMenuClose,
		},
		{
			ViewName: "information",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.Information.Donate),
			Modifier: gocui.ModNone,
			Handler:  gui.handleDonate,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.PreviousContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.NextContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersNextContext,
			Description: gui.Tr.NextContext,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersRemoveMenu,
			Description: gui.Tr.Remove,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.HideStopped),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleHideStoppedContainers,
			Description: gui.Tr.HideStopped,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.Stop),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerStop,
			Description: gui.Tr.Stop,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.Restart),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerRestart,
			Description: gui.Tr.Restart,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.Attach),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerAttach,
			Description: gui.Tr.Attach,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.ViewLogs),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerViewLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.RunCustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "containers",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Containers.RunBulkCommands),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRemoveMenu,
			Description: gui.Tr.RemoveService,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.Stop),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceStop,
			Description: gui.Tr.Stop,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.Restart),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRestart,
			Description: gui.Tr.Restart,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.Attach),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceAttach,
			Description: gui.Tr.Attach,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.ViewLogs),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceViewLogs,
			Description: gui.Tr.ViewLogs,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.PreviousContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.NextContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesNextContext,
			Description: gui.Tr.NextContext,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.ViewRestartOptions),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServiceRestartMenu,
			Description: gui.Tr.ViewRestartOptions,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.RunCustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "services",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Services.RunBulkCommands),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleServicesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "images",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.PreviousContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "images",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.NextContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesNextContext,
			Description: gui.Tr.NextContext,
		},
		{
			ViewName:    "images",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Images.RunCustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "images",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Images.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesRemoveMenu,
			Description: gui.Tr.RemoveImage,
		},
		{
			ViewName:    "images",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Images.RunBulkCommands),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleImagesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "volumes",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.PreviousContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesPrevContext,
			Description: gui.Tr.PreviousContext,
		},
		{
			ViewName:    "volumes",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.NextContext),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesNextContext,
			Description: gui.Tr.NextContext,
		},
		{
			ViewName:    "volumes",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Volumes.RunCustomCommand),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesCustomCommand,
			Description: gui.Tr.RunCustomCommand,
		},
		{
			ViewName:    "volumes",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Volumes.Remove),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesRemoveMenu,
			Description: gui.Tr.RemoveVolume,
		},
		{
			ViewName:    "volumes",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Volumes.RunBulkCommands),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleVolumesBulkCommand,
			Description: gui.Tr.ViewBulkCommands,
		},
		{
			ViewName:    "main",
			Keys:        parsedKeybindings(gui.Config.UserConfig.Keybindings.Main.Return),
			Modifier:    gocui.ModNone,
			Handler:     gui.handleExitMain,
			Description: gui.Tr.Return,
		},
		{
			ViewName: "main",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.Main.ScrollLeft),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollLeftMain,
		},
		{
			ViewName: "main",
			Keys:     parsedKeybindings(gui.Config.UserConfig.Keybindings.Main.ScrollRight),
			Modifier: gocui.ModNone,
			Handler:  gui.scrollRightMain,
		},
	}

	// TODO: add more views here and check use
	for _, viewName := range []string{"project", "services", "containers", "images", "volumes", "menu"} {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Keys: []gocui.Key{gocui.KeyArrowLeft}, Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: viewName, Keys: []gocui.Key{gocui.KeyArrowRight}, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: viewName, Keys: []gocui.Key{'h'}, Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: viewName, Keys: []gocui.Key{'l'}, Modifier: gocui.ModNone, Handler: gui.nextView},
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
			{ViewName: viewName, Keys: []gocui.Key{'k'}, Modifier: gocui.ModNone, Handler: functions.onKeyUpPress},
			{ViewName: viewName, Keys: []gocui.Key{gocui.KeyArrowUp}, Modifier: gocui.ModNone, Handler: functions.onKeyUpPress},
			{ViewName: viewName, Keys: []gocui.Key{gocui.MouseWheelUp}, Modifier: gocui.ModNone, Handler: functions.onKeyUpPress},
			{ViewName: viewName, Keys: []gocui.Key{'j'}, Modifier: gocui.ModNone, Handler: functions.onKeyDownPress},
			{ViewName: viewName, Keys: []gocui.Key{gocui.KeyArrowDown}, Modifier: gocui.ModNone, Handler: functions.onKeyDownPress},
			{ViewName: viewName, Keys: []gocui.Key{gocui.MouseWheelDown}, Modifier: gocui.ModNone, Handler: functions.onKeyDownPress},
			{ViewName: viewName, Keys: []gocui.Key{gocui.MouseLeft}, Modifier: gocui.ModNone, Handler: functions.onClick},
		}...)
	}

	for _, viewName := range []string{"project", "services", "containers", "images", "volumes"} {
		bindings = append(bindings, &Binding{
			ViewName:    viewName,
			Keys:        []gocui.Key{gocui.KeyEnter},
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
		if err := g.SetKeybinding(binding.ViewName, binding.Keys, binding.Modifier, binding.Handler); err != nil {
			return err
		}
	}

	if err := g.SetTabClickBinding("main", gui.onMainTabClick); err != nil {
		return err
	}

	return nil
}

func parsedKeybindings(binds []string) []gocui.Key {
	var keys []gocui.Key
	for _, bind := range binds {
		keys = append(keys, keybinding.MustParse(bind).Value) // MustParse panics on failure
	}
	return keys
}
