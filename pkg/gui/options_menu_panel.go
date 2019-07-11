package gui

import (
	"strings"

	"github.com/go-errors/errors"

	"github.com/jesseduffield/gocui"
)

func (gui *Gui) getBindings(v *gocui.View) []*Binding {
	var (
		bindingsGlobal, bindingsPanel []*Binding
	)

	bindings := gui.GetInitialKeybindings()

	for _, binding := range bindings {
		if binding.GetKey() != "" && binding.Description != "" {
			switch binding.ViewName {
			case "":
				bindingsGlobal = append(bindingsGlobal, binding)
			case v.Name():
				bindingsPanel = append(bindingsPanel, binding)
			}
		}
	}

	// check if we have any keybindings from our parent view to add
	if v.ParentView != nil {
	L:
		for _, binding := range bindings {
			if binding.GetKey() != "" && binding.Description != "" {
				if binding.ViewName == v.ParentView.Name() {
					// if we haven't got a conflict we will display the binding
					for _, ownBinding := range bindingsPanel {
						if ownBinding.GetKey() == binding.GetKey() {
							continue L
						}
					}
					bindingsPanel = append(bindingsPanel, binding)
				}
			}
		}
	}

	// } else if v.ParentView != nil && binding.ViewName == v.ParentView.Name() {
	// 	// only add this if we don't have our own matching binding
	// 	bindingsPanel = append(bindingsPanel, binding)

	// append dummy element to have a separator between
	// panel and global keybindings
	bindingsPanel = append(bindingsPanel, &Binding{})
	return append(bindingsPanel, bindingsGlobal...)
}

func (gui *Gui) handleCreateOptionsMenu(g *gocui.Gui, v *gocui.View) error {
	if v.Name() == "menu" || v.Name() == "confirmation" {
		return nil
	}

	bindings := gui.getBindings(v)

	handleMenuPress := func(index int) error {
		if bindings[index].Key == nil {
			return nil
		}
		if index >= len(bindings) {
			return errors.New("Index is greater than size of bindings")
		}
		err := gui.handleMenuClose(g, v)
		if err != nil {
			return err
		}
		return bindings[index].Handler(g, v)
	}

	return gui.createMenu(strings.Title(gui.Tr.Menu), bindings, len(bindings), handleMenuPress)
}
