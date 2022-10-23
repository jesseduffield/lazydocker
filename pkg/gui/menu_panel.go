package gui

import (
	"github.com/jesseduffield/gocui"
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
			list: NewFilteredList[*MenuItem](),
			view: gui.Views.Menu,
		},
		noItemsMessage: "",
		gui:            gui.intoInterface(),
		onClick:        gui.onMenuPress,
		sort:           nil,
		getDisplayStrings: func(menuItem *MenuItem) []string {
			return menuItem.LabelColumns
		},
		onRerender: func() error {
			return gui.resizePopupPanel(gui.Views.Menu)
		},

		// the menu panel doesn't actually have any contexts to display on the main view
		// so what follows are all dummy values
		contextKeyPrefix: "menu",
		contextIdx:       0,
		getContextCacheKey: func(menuItem *MenuItem) string {
			return ""
		},
		getContexts: func() []ContextConfig[*MenuItem] {
			return []ContextConfig[*MenuItem]{}
		},
	}
}

func (gui *Gui) onMenuPress(menuItem *MenuItem) error {
	gui.Views.Menu.Visible = false
	err := gui.returnFocus()
	if err != nil {
		return err
	}

	if menuItem.OnPress == nil {
		return nil
	}

	return menuItem.OnPress()
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

func (gui *Gui) handleMenuClose(g *gocui.Gui, v *gocui.View) error {
	gui.Views.Menu.Visible = false
	return gui.returnFocus()
}
