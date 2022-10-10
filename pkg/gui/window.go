package gui

// func (gui *Gui) currentWindow() string {
// 	// at the moment, we only have one view per window in lazydocker, so we
// 	// are using the view name as the window name
// 	return gui.currentViewName()
// }

// excludes popups
func (gui *Gui) currentStaticWindowName() string {
	return gui.currentStaticViewName()
}

func (gui *Gui) currentSideWindowName() string {
	return gui.currentSideViewName()
}
