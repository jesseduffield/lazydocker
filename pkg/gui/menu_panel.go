package gui

import (
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

type MenuItem struct {
	Label string

	// alternative to Label. Allows specifying columns which will be auto-aligned
	LabelColumns []string

	OnPress func() error

	// Only applies when Label is used
	OpensMenu bool
}

type CreateMenuOptions struct {
	Title      string
	Items      []*MenuItem
	HideCancel bool
}

func (gui *Gui) getMenuPanel() *SideListPanel[*MenuItem] {
	return &SideListPanel[*MenuItem]{
		ListPanel: ListPanel[*MenuItem]{
			List: NewFilteredList[*MenuItem](),
			view: gui.Views.Menu,
		},
		NoItemsMessage: "",
		gui:            gui.intoInterface(),
		OnClick:        gui.onMenuPress,
		Sort:           nil,
		GetDisplayStrings: func(menuItem *MenuItem) []string {
			return menuItem.LabelColumns
		},
		OnRerender: func() error {
			return gui.resizePopupPanel(gui.Views.Menu)
		},
		// so that we can avoid some UI trickiness, the menu will not have filtering
		// abillity yet. To support it, we would need to have filter state against
		// each panel (e.g. for when you filter the images panel, then bring up
		// the options menu, then try to filter that too.
		DisableFilter: true,
	}
}

func (gui *Gui) onMenuPress(menuItem *MenuItem) error {
	if err := gui.handleMenuClose(); err != nil {
		return err
	}

	if menuItem.OnPress != nil {
		return menuItem.OnPress()
	}

	return nil
}

func (gui *Gui) handleMenuPress() error {
	selectedMenuItem, err := gui.Panels.Menu.GetSelectedItem()
	if err != nil {
		return nil
	}

	return gui.onMenuPress(selectedMenuItem)
}

func (gui *Gui) Menu(opts CreateMenuOptions) error {
	if !opts.HideCancel {
		// this is mutative but I'm okay with that for now
		opts.Items = append(opts.Items, &MenuItem{
			LabelColumns: []string{gui.Tr.Cancel},
			OnPress: func() error {
				return nil
			},
		})
	}

	maxColumnSize := 1

	for _, item := range opts.Items {
		if item.LabelColumns == nil {
			item.LabelColumns = []string{item.Label}
		}

		if item.OpensMenu {
			item.LabelColumns[0] = utils.OpensMenuStyle(item.LabelColumns[0])
		}

		maxColumnSize = utils.Max(maxColumnSize, len(item.LabelColumns))
	}

	for _, item := range opts.Items {
		if len(item.LabelColumns) < maxColumnSize {
			// we require that each item has the same number of columns so we're padding out with blank strings
			// if this item has too few
			item.LabelColumns = append(item.LabelColumns, make([]string, maxColumnSize-len(item.LabelColumns))...)
		}
	}

	gui.Panels.Menu.SetItems(opts.Items)
	gui.Panels.Menu.setSelectedLineIdx(0)

	if err := gui.Panels.Menu.RerenderList(); err != nil {
		return err
	}

	gui.Views.Menu.Title = opts.Title
	gui.Views.Menu.Visible = true

	return gui.switchFocus(gui.Views.Menu)
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

func (gui *Gui) handleMenuClose() error {
	gui.Views.Menu.Visible = false

	// this code is here for when we do add filter ability to the menu panel,
	// though it's currently disabled
	if gui.State.Filter.panel == gui.Panels.Menu {
		if err := gui.clearFilter(); err != nil {
			return err
		}

		// we need to remove the view from the view stack because we're about to
		// return focus and don't want to land in the search view when it was searching
		// the menu in the first place
		gui.removeViewFromStack(gui.Views.Filter)
	}

	return gui.returnFocus()
}
