package gui

import (
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

	return string(key)
}

// GetInitialKeybindings is a function.
func (gui *Gui) GetInitialKeybindings() []*Binding {
	bindings := []*Binding{
		{
			ViewName: "",
			Key:      'q',
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		}, {
			ViewName: "",
			Key:      gocui.KeyCtrlC,
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		}, {
			ViewName: "",
			Key:      gocui.KeyEsc,
			Modifier: gocui.ModNone,
			Handler:  gui.quit,
		}, {
			ViewName: "",
			Key:      gocui.KeyPgup,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollUpMain,
		}, {
			ViewName: "",
			Key:      gocui.KeyPgdn,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollDownMain,
		}, {
			ViewName: "",
			Key:      gocui.KeyCtrlU,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollUpMain,
		}, {
			ViewName: "",
			Key:      gocui.KeyCtrlD,
			Modifier: gocui.ModNone,
			Handler:  gui.scrollDownMain,
		}, {
			ViewName: "",
			Key:      gocui.KeyEnd,
			Modifier: gocui.ModNone,
			Handler:  gui.autoScrollMain,
		}, {
			ViewName: "",
			Key:      'x',
			Modifier: gocui.ModNone,
			Handler:  gui.handleCreateOptionsMenu,
		}, {
			ViewName:    "status",
			Key:         'e',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleEditConfig,
			Description: gui.Tr.SLocalize("EditConfig"),
		}, {
			ViewName:    "status",
			Key:         'o',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleOpenConfig,
			Description: gui.Tr.SLocalize("OpenConfig"),
		}, {
			ViewName:    "status",
			Key:         'u',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleCheckForUpdate,
			Description: gui.Tr.SLocalize("checkForUpdate"),
		},
		{
			ViewName: "menu",
			Key:      gocui.KeyEsc,
			Modifier: gocui.ModNone,
			Handler:  gui.handleMenuClose,
		}, {
			ViewName: "menu",
			Key:      'q',
			Modifier: gocui.ModNone,
			Handler:  gui.handleMenuClose,
		}, {
			ViewName: "information",
			Key:      gocui.MouseLeft,
			Modifier: gocui.ModNone,
			Handler:  gui.handleDonate,
		}, {
			ViewName:    "containers",
			Key:         '[',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersPrevContext,
			Description: gui.Tr.SLocalize("previousContext"),
		}, {
			ViewName:    "containers",
			Key:         ']',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersNextContext,
			Description: gui.Tr.SLocalize("nextContext"),
		},
		{
			ViewName:    "containers",
			Key:         'd',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainersRemoveMenu,
			Description: gui.Tr.SLocalize("removeContainer"),
		},
		{
			ViewName:    "containers",
			Key:         's',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerStop,
			Description: gui.Tr.SLocalize("stopContainer"),
		},
		{
			ViewName:    "containers",
			Key:         'r',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerRestart,
			Description: gui.Tr.SLocalize("restartContainer"),
		},
		{
			ViewName:    "containers",
			Key:         'a',
			Modifier:    gocui.ModNone,
			Handler:     gui.handleContainerAttach,
			Description: gui.Tr.SLocalize("attachContainer"),
		},
	}

	// TODO: add more views here
	for _, viewName := range []string{"status", "containers", "menu"} {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Key: gocui.KeyTab, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: viewName, Key: gocui.KeyArrowLeft, Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: viewName, Key: gocui.KeyArrowRight, Modifier: gocui.ModNone, Handler: gui.nextView},
			{ViewName: viewName, Key: 'h', Modifier: gocui.ModNone, Handler: gui.previousView},
			{ViewName: viewName, Key: 'l', Modifier: gocui.ModNone, Handler: gui.nextView},
		}...)
	}

	listPanelMap := map[string]struct {
		prevLine func(*gocui.Gui, *gocui.View) error
		nextLine func(*gocui.Gui, *gocui.View) error
		focus    func(*gocui.Gui, *gocui.View) error
	}{
		"menu":       {prevLine: gui.handleMenuPrevLine, nextLine: gui.handleMenuNextLine, focus: gui.handleMenuSelect},
		"containers": {prevLine: gui.handleContainersPrevLine, nextLine: gui.handleContainersNextLine, focus: gui.handleContainersFocus},
	}

	for viewName, functions := range listPanelMap {
		bindings = append(bindings, []*Binding{
			{ViewName: viewName, Key: 'k', Modifier: gocui.ModNone, Handler: functions.prevLine},
			{ViewName: viewName, Key: gocui.KeyArrowUp, Modifier: gocui.ModNone, Handler: functions.prevLine},
			{ViewName: viewName, Key: gocui.MouseWheelUp, Modifier: gocui.ModNone, Handler: functions.prevLine},
			{ViewName: viewName, Key: 'j', Modifier: gocui.ModNone, Handler: functions.nextLine},
			{ViewName: viewName, Key: gocui.KeyArrowDown, Modifier: gocui.ModNone, Handler: functions.nextLine},
			{ViewName: viewName, Key: gocui.MouseWheelDown, Modifier: gocui.ModNone, Handler: functions.nextLine},
			{ViewName: viewName, Key: gocui.MouseLeft, Modifier: gocui.ModNone, Handler: functions.focus},
		}...)
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
	return nil
}
