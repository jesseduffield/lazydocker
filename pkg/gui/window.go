package gui

func (gui *Gui) currentWindow() string {
	// at the moment, we only have one view per window in lazydocker, so we
	// are using the view name as the window name
	return gui.currentViewName()
}
