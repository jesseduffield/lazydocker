package gui

import "github.com/jesseduffield/gocui"

func (gui *Gui) handleOpenImageSearch() error {
	return gui.handleOpenSearch(gui.Views.Images)
}

func (gui *Gui) handleOpenSearch(view *gocui.View) error {
	gui.State.Searching.isSearching = true
	gui.State.Searching.view = view

	gui.Views.Search.ClearTextArea()

	return gui.switchFocus(gui.Views.Search)
}

func (gui *Gui) onNewSearchString(value string) error {
	// need to refresh the right list panel.
	gui.State.Searching.searchString = value
	return gui.refreshImages()
}

func (gui *Gui) wrapEditor(f func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool) func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	return func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
		matched := f(v, key, ch, mod)
		if matched {
			gui.onNewSearchString(v.TextArea.GetContent())
		}
		return matched
	}
}
