package gui

import (
	"github.com/jesseduffield/gocui"
)

func (gui *Gui) handleOpenSearch() error {
	panel, ok := gui.currentListPanel()
	if !ok {
		return nil
	}

	gui.State.Searching.isSearching = true
	gui.State.Searching.view = panel.View()
	gui.State.Searching.panel = panel

	return gui.switchFocus(gui.Views.Search)
}

func (gui *Gui) onNewSearchString(value string) error {
	// need to refresh the right list panel.
	gui.State.Searching.searchString = value
	return gui.State.Searching.panel.RerenderList()
}

func (gui *Gui) wrapEditor(f func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool) func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	return func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
		matched := f(v, key, ch, mod)
		if matched {
			// TODO: handle error
			_ = gui.onNewSearchString(v.TextArea.GetContent())
		}
		return matched
	}
}

func (gui *Gui) escapeSearchPrompt() error {
	if err := gui.clearSearch(); err != nil {
		return err
	}

	return gui.returnFocus()
}

func (gui *Gui) clearSearch() error {
	gui.State.Searching.searchString = ""
	gui.State.Searching.isSearching = false
	gui.State.Searching.view = nil
	panel := gui.State.Searching.panel
	gui.State.Searching.panel = nil
	gui.Views.Search.ClearTextArea()

	return panel.RerenderList()
}

// returns to the list view with the filter still applied
func (gui *Gui) commitSearch() error {
	return gui.returnFocus()
}
