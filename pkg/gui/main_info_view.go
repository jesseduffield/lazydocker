package gui

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/gui/types"
)

func (gui *Gui) renderMainInfoView() error {
	str := ""
	keyColorSprintFn := color.New(color.FgMagenta).SprintFunc()
	valueColorSprintFn := color.New(color.FgGreen).SprintFunc()

	keyVal := func(key, value string) string {
		return fmt.Sprintf("%s:%s  ", keyColorSprintFn(key), valueColorSprintFn(value))
	}
	str += keyVal("Tail", blankToNil(gui.State.LogConfig.Tail))
	str += keyVal("Since", blankToNil(gui.State.LogConfig.Since))
	str += keyVal("Timestamps", boolToStr(gui.State.LogConfig.Timestamps))
	str += keyVal("Wrap", boolToStr(gui.State.LogConfig.Wrap))
	str += keyVal("Autoscroll", boolToStr(gui.Views.Main.Autoscroll))

	return gui.renderString(gui.g, "mainInfo", str)
}

func boolToStr(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

func blankToNil(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (gui *Gui) reselectSideListItem() error {
	currentSidePanel, ok := gui.currentSidePanel()
	if ok {
		if err := currentSidePanel.HandleSelect(); err != nil {
			return err
		}
	}
	return nil
}

func (gui *Gui) handleOpenLogMenu() error {
	return gui.Menu(CreateMenuOptions{
		Title: "Log Options",
		Items: []*types.MenuItem{
			{
				Label: "Set tail",
				OnPress: func() error {
					return gui.Prompt("Set tail value (e.g. 200). Unset with empty value", func(input string) error {
						gui.State.LogConfig.Tail = input
						return gui.reselectSideListItem()
					})
				},
			},
			{
				Label: "Set since",
				OnPress: func() error {
					return gui.Prompt("Set since value (e.g. 60m). Unset with empty value", func(input string) error {
						gui.State.LogConfig.Since = input
						return gui.reselectSideListItem()
					})
				},
			},
			{
				Label: "Toggle timestamps",
				OnPress: func() error {
					gui.State.LogConfig.Timestamps = !gui.State.LogConfig.Timestamps
					return gui.reselectSideListItem()
				},
			},
			{
				Label: "Toggle wrap",
				OnPress: func() error {
					gui.State.LogConfig.Wrap = !gui.State.LogConfig.Wrap
					return gui.reselectSideListItem()
				},
			},
			{
				Label: "Toggle autoscroll",
				OnPress: func() error {
					gui.Views.Main.Autoscroll = !gui.Views.Main.Autoscroll
					// the view will refresh automatically
					return nil
				},
			},
		},
	})
}

func (gui *Gui) logArgsKey() string {
	// not including autoscroll because that doesn't require refetching the logs
	return gui.State.LogConfig.Since + gui.State.LogConfig.Tail + boolToStr(gui.State.LogConfig.Timestamps) + boolToStr(gui.State.LogConfig.Wrap)
}
