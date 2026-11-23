package gui

import (
	"fmt"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
)

type filterConfig struct {
	active *bool
	panel  *panels.ISideListPanel
	needle *string
	view   *gocui.View
	isLogs bool
}

func (gui *Gui) listFilterConfig() filterConfig {
	return filterConfig{
		active: &gui.State.Filter.active,
		panel:  &gui.State.Filter.panel,
		needle: &gui.State.Filter.needle,
		view:   gui.Views.Filter,
		isLogs: false,
	}
}

func (gui *Gui) logsFilterConfig() filterConfig {
	return filterConfig{
		active: &gui.State.LogsFilter.active,
		panel:  &gui.State.LogsFilter.panel,
		needle: &gui.State.LogsFilter.needle,
		view:   gui.Views.LogsFilter,
		isLogs: true,
	}
}

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

func (gui *Gui) handleOpenLogsFilter() error {
	panel, ok := gui.currentSidePanel()

	// If not focused (e.g. we are in 'main'), try to get the side panel from the view stack
	if !ok {
		p, ok := gui.sidePanelByViewName(gui.currentSideViewName())
		if !ok {
			return nil
		}
		panel = p
	}

	if !gui.IsCurrentView(gui.GetMainView()) {
		return nil
	}

	if !gui.isMainTabActive("-logs") {
		return nil
	}

	gui.State.LogsFilter.active = true
	gui.State.LogsFilter.panel = panel
	gui.State.LogsFilter.needle = ""

	return gui.switchFocus(gui.Views.LogsFilter)
}

func (gui *Gui) onNewFilterNeedle(value string) error {
	return gui.handleNewFilterNeedle(gui.listFilterConfig(), value)
}

func (gui *Gui) onNewLogsFilterNeedle(value string) error {
	return gui.handleNewFilterNeedle(gui.logsFilterConfig(), value)
}

func (gui *Gui) handleNewFilterNeedle(config filterConfig, value string) error {
	if config.isLogs && *config.panel == nil {
		return nil
	}

	*config.needle = value

	panel := *config.panel
	if panel == nil {
		return nil
	}

	if config.isLogs {
		gui.State.Panels.Main.ObjectKey = ""
		return panel.HandleSelect()
	}

	gui.ResetOrigin(panel.GetView())
	return panel.RerenderList()
}

func (gui *Gui) wrapEditorBase(
	f func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool,
	onNeedleChange func(string) error,
) func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	return func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
		matched := f(v, key, ch, mod)
		if matched {
			if err := onNeedleChange(v.TextArea.GetContent()); err != nil {
				gui.Log.Error(err)
			}
		}
		return matched
	}
}

func (gui *Gui) wrapEditor(f func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool) func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	return gui.wrapEditorBase(f, gui.onNewFilterNeedle)
}

func (gui *Gui) wrapLogsEditor(f func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool) func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	return gui.wrapEditorBase(f, gui.onNewLogsFilterNeedle)
}

func (gui *Gui) escapeFilterPrompt() error {
	return gui.escapeFilterPromptWithConfig(gui.listFilterConfig())
}

func (gui *Gui) escapeLogsFilterPrompt() error {
	return gui.escapeFilterPromptWithConfig(gui.logsFilterConfig())
}

func (gui *Gui) escapeFilterPromptWithConfig(config filterConfig) error {
	if err := gui.clearFilterWithConfig(config); err != nil {
		return err
	}

	if config.isLogs {
		gui.removeViewFromStack(config.view)
	}

	return gui.returnFocus()
}

func (gui *Gui) clearFilter() error {
	return gui.clearFilterWithConfig(gui.listFilterConfig())
}

func (gui *Gui) clearLogsFilter() error {
	return gui.clearFilterWithConfig(gui.logsFilterConfig())
}

func (gui *Gui) clearFilterWithConfig(config filterConfig) error {
	*config.needle = ""
	*config.active = false
	panel := *config.panel
	*config.panel = nil
	config.view.ClearTextArea()

	if panel == nil {
		return nil
	}

	if config.isLogs {
		gui.State.Panels.Main.ObjectKey = ""
		return panel.HandleSelect()
	}

	gui.ResetOrigin(panel.GetView())
	return panel.RerenderList()
}

// returns to the list view with the filter still applied
func (gui *Gui) commitFilter() error {
	return gui.commitFilterWithConfig(gui.listFilterConfig())
}

// returns to the main view with the filter still applied
func (gui *Gui) commitLogsFilter() error {
	return gui.commitFilterWithConfig(gui.logsFilterConfig())
}

func (gui *Gui) commitFilterWithConfig(config filterConfig) error {
	if *config.needle == "" {
		if err := gui.clearFilterWithConfig(config); err != nil {
			return err
		}
	}

	if config.isLogs {
		gui.removeViewFromStack(config.view)
	}

	return gui.returnFocus()
}

func (gui *Gui) filterPrompt() string {
	return fmt.Sprintf("%s: ", gui.Tr.FilterPrompt)
}

func (gui *Gui) logsFilterPrompt() string {
	return fmt.Sprintf("%s: ", gui.Tr.LogsFilterPrompt)
}
