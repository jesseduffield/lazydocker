package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
)

func (gui *Gui) handleOpenFilter() error {
	panel, ok := gui.currentListPanel()
	if !ok {
		return nil
	}

	if panel.IsFilterDisabled() {
		return nil
	}

	gui.State.Filter.active = true
	gui.State.Filter.panel = panel

	return gui.switchFocus(gui.Views.Filter)
}

func (gui *Gui) onNewFilterNeedle(value string) error {
	gui.State.Filter.needle = value
	gui.ResetOrigin(gui.State.Filter.panel.GetView())
	return gui.State.Filter.panel.RerenderList()
}

func (gui *Gui) wrapEditor(f func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool) func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	return func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
		matched := f(v, key, ch, mod)
		if matched {
			if err := gui.onNewFilterNeedle(v.TextArea.GetContent()); err != nil {
				gui.Log.Error(err)
			}
		}
		return matched
	}
}

func (gui *Gui) escapeFilterPrompt() error {
	if err := gui.clearFilter(); err != nil {
		return err
	}

	return gui.returnFocus()
}

func (gui *Gui) clearFilter() error {
	gui.State.Filter.needle = ""
	gui.State.Filter.active = false
	panel := gui.State.Filter.panel
	gui.State.Filter.panel = nil
	gui.Views.Filter.ClearTextArea()

	if panel == nil {
		return nil
	}

	gui.ResetOrigin(panel.GetView())

	return panel.RerenderList()
}

// returns to the list view with the filter still applied
func (gui *Gui) commitFilter() error {
	if gui.State.Filter.needle == "" {
		if err := gui.clearFilter(); err != nil {
			return err
		}
	}

	return gui.returnFocus()
}

func (gui *Gui) filterPrompt() string {
	return fmt.Sprintf("%s: ", gui.Tr.FilterPrompt)
}
