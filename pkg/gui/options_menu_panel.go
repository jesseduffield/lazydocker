package gui

import (
	"github.com/samber/lo"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
)

func (gui *Gui) getBindings(v *gocui.View) []*Binding {
	var bindingsGlobal, bindingsPanel []*Binding

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

	// append dummy element to have a separator between
	// panel and global keybindings
	bindingsPanel = append(bindingsPanel, &Binding{})
	return append(bindingsPanel, bindingsGlobal...)
}

func (gui *Gui) handleCreateOptionsMenu(g *gocui.Gui, v *gocui.View) error {
	if gui.isPopupPanel(v.Name()) {
		return nil
	}

	menuItems := lo.Map(gui.getBindings(v), func(binding *Binding, _ int) *types.MenuItem {
		return &types.MenuItem{
			LabelColumns: []string{binding.GetKey(), binding.Description},
			OnPress: func() error {
				if binding.Key == nil {
					return nil
				}

				return binding.Handler(g, v)
			},
		}
	})

	return gui.Menu(CreateMenuOptions{
		Title:      gui.Tr.MenuTitle,
		Items:      menuItems,
		HideCancel: true,
	})
}
