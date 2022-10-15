package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// list panel functions

func (gui *Gui) handleMenuSelect(g *gocui.Gui, v *gocui.View) error {
	gui.focusY(gui.State.Panels.Menu.SelectedLine, gui.State.MenuItemCount, v)
	return nil
}

func (gui *Gui) handleMenuNextLine(g *gocui.Gui, v *gocui.View) error {
	panelState := gui.State.Panels.Menu
	gui.changeSelectedLine(&panelState.SelectedLine, v.LinesHeight(), false)

	return gui.handleMenuSelect(g, v)
}

func (gui *Gui) handleMenuClick(g *gocui.Gui, v *gocui.View) error {
	itemCount := gui.State.MenuItemCount
	handleSelect := gui.handleMenuSelect
	selectedLine := &gui.State.Panels.Menu.SelectedLine

	if err := gui.handleClick(v, itemCount, selectedLine, handleSelect); err != nil {
		return err
	}

	return gui.State.Panels.Menu.OnPress(g, v)
}

func (gui *Gui) handleMenuPrevLine(g *gocui.Gui, v *gocui.View) error {
	panelState := gui.State.Panels.Menu
	gui.changeSelectedLine(&panelState.SelectedLine, v.LinesHeight(), true)

	return gui.handleMenuSelect(g, v)
}

// specific functions

func (gui *Gui) renderMenuOptions() error {
	optionsMap := map[string]string{
		"esc/q": gui.Tr.Close,
		"↑ ↓":   gui.Tr.Navigate,
		"enter": gui.Tr.Execute,
	}
	return gui.renderOptionsMap(optionsMap)
}

func (gui *Gui) handleMenuClose(g *gocui.Gui, v *gocui.View) error {
	for _, key := range []gocui.Key{gocui.KeySpace, gocui.KeyEnter, 'y'} {
		if err := g.DeleteKeybinding("menu", key, gocui.ModNone); err != nil {
			return err
		}
	}
	gui.Views.Menu.Visible = false
	return gui.returnFocus()
}

func (gui *Gui) createMenu(title string, items interface{}, itemCount int, handlePress func(int) error) error {
	isFocused := gui.g.CurrentView().Name() == "menu"
	gui.State.MenuItemCount = itemCount
	list, err := utils.RenderList(items, utils.IsFocused(isFocused))
	if err != nil {
		return err
	}

	x0, y0, x1, y1 := gui.getConfirmationPanelDimensions(gui.g, false, list)
	_, _ = gui.g.SetView("menu", x0, y0, x1, y1, 0)
	menuView := gui.Views.Menu
	menuView.Title = title
	menuView.FgColor = gocui.ColorDefault
	menuView.Clear()
	fmt.Fprint(menuView, list)
	gui.State.Panels.Menu.SelectedLine = 0

	wrappedHandlePress := func(g *gocui.Gui, v *gocui.View) error {
		selectedLine := gui.State.Panels.Menu.SelectedLine

		menuView.Visible = false
		err := gui.returnFocus()
		if err != nil {
			return err
		}

		if err := handlePress(selectedLine); err != nil {
			return err
		}

		return nil
	}

	gui.State.Panels.Menu.OnPress = wrappedHandlePress

	for _, key := range []gocui.Key{gocui.KeySpace, gocui.KeyEnter, 'y'} {
		_ = gui.g.DeleteKeybinding("menu", key, gocui.ModNone)

		if err := gui.g.SetKeybinding("menu", key, gocui.ModNone, wrappedHandlePress); err != nil {
			return err
		}
	}

	gui.g.Update(func(g *gocui.Gui) error {
		menuView.Visible = true
		return gui.switchFocus(menuView)
	})
	return nil
}
