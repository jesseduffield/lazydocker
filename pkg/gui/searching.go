package gui

import (
	"github.com/jesseduffield/gocui"
)

func (gui *Gui) handleOpenImageSearch() error {
	return gui.handleOpenSearch(gui.Views.Images)
}

func (gui *Gui) handleOpenSearch(view *gocui.View) error {
	gui.State.Searching.isSearching = true
	gui.State.Searching.view = view

	return gui.switchFocus(gui.Views.Search)
}

func (gui *Gui) onNewSearchString(value string) error {
	// need to refresh the right list panel.
	gui.State.Searching.searchString = value
	return gui.Panels.Images.RerenderList()
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
	gui.Views.Search.ClearTextArea()

	return gui.Panels.Images.RerenderList()
}

// returns to the list view with the filter still applied
func (gui *Gui) commitSearch() error {
	return gui.returnFocus()
}
