package gui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jesseduffield/gocui"
)

func (gui *Gui) refreshStatus(g *gocui.Gui) error {
	v, err := g.View("status")
	if err != nil {
		panic(err)
	}
	// for some reason if this isn't wrapped in an update the clear seems to
	// be applied after the other things or something like that; the panel's
	// contents end up cleared
	g.Update(func(*gocui.Gui) error {
		v.Clear()
		fmt.Fprint(v, "lazydocker")
		return nil
	})

	return nil
}

func (gui *Gui) handleStatusSelect(g *gocui.Gui, v *gocui.View) error {
	if gui.popupPanelFocused() {
		return nil
	}

	if _, err := gui.g.SetCurrentView(v.Name()); err != nil {
		return err
	}

	dashboardString := strings.Join(
		[]string{
			lazydockerTitle(),
			"Copyright (c) 2019 Jesse Duffield",
			"Keybindings: https://github.com/jesseduffield/lazydocker/blob/master/docs/keybindings",
			"Config Options: https://github.com/jesseduffield/lazydocker/blob/master/docs/Config.md",
			"Raise an Issue: https://github.com/jesseduffield/lazydocker/issues",
		}, "\n\n")

	return gui.renderString(g, "main", dashboardString)
}

func (gui *Gui) handleOpenConfig(g *gocui.Gui, v *gocui.View) error {
	filename := filepath.Join(gui.Config.ConfigDir, "config.yml")
	return gui.openFile(filename)
}

func (gui *Gui) handleEditConfig(g *gocui.Gui, v *gocui.View) error {
	filename := filepath.Join(gui.Config.ConfigDir, "config.yml")
	return gui.editFile(filename)
}

func lazydockerTitle() string {
	return `
   _                     _            _
  | |                   | |          | |
  | | __ _ _____   _  __| | ___   ___| | _____ _ __
  | |/ _` + "`" + ` |_  / | | |/ _` + "`" + ` |/ _ \ / __| |/ / _ \ '__|
  | | (_| |/ /| |_| | (_| | (_) | (__|   <  __/ |
  |_|\__,_/___|\__, |\__,_|\___/ \___|_|\_\___|_|
                __/ |
               |___/
`
}
