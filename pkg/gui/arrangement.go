package gui

import (
	"github.com/jesseduffield/lazycore/pkg/boxlayout"
	"github.com/jesseduffield/lazydocker/pkg/gui/panels"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/mattn/go-runewidth"
	"github.com/samber/lo"
)

// In this file we use the boxlayout package, along with knowledge about the app's state,
// to arrange the windows (i.e. panels) on the screen.

const INFO_SECTION_PADDING = " "

func (gui *Gui) getWindowDimensions(informationStr string, appStatus string) map[string]boxlayout.Dimensions {
	minimumHeight := 9
	minimumWidth := 10
	width, height := gui.g.Size()
	if width < minimumWidth || height < minimumHeight {
		return boxlayout.ArrangeWindows(&boxlayout.Box{Window: "limit"}, 0, 0, width, height)
	}

	sideSectionWeight, mainSectionWeight := gui.getMidSectionWeights()

	sidePanelsDirection := boxlayout.COLUMN
	portraitMode := width <= 84 && height > 45
	if portraitMode {
		sidePanelsDirection = boxlayout.ROW
	}

	showInfoSection := gui.Config.UserConfig.Gui.ShowBottomLine || gui.State.Filter.active
	infoSectionSize := 0
	if showInfoSection {
		infoSectionSize = 1
	}

	root := &boxlayout.Box{
		Direction: boxlayout.ROW,
		Children: []*boxlayout.Box{
			{
				Direction: sidePanelsDirection,
				Weight:    1,
				Children: []*boxlayout.Box{
					{
						Direction:           boxlayout.ROW,
						Weight:              sideSectionWeight,
						ConditionalChildren: gui.sidePanelChildren,
					},
					{
						Window: "main",
						Weight: mainSectionWeight,
					},
				},
			},
			{
				Direction: boxlayout.COLUMN,
				Size:      infoSectionSize,
				Children:  gui.infoSectionChildren(informationStr, appStatus),
			},
		},
	}

	return boxlayout.ArrangeWindows(root, 0, 0, width, height)
}

func (gui *Gui) getMidSectionWeights() (int, int) {
	currentWindow := gui.currentStaticWindowName()

	// we originally specified this as a ratio i.e. .20 would correspond to a weight of 1 against 4
	sidePanelWidthRatio := gui.Config.UserConfig.Gui.SidePanelWidth
	// we could make this better by creating ratios like 2:3 rather than always 1:something
	mainSectionWeight := int(1/sidePanelWidthRatio) - 1
	sideSectionWeight := 1

	if currentWindow == "main" && gui.State.ScreenMode == SCREEN_FULL {
		mainSectionWeight = 1
		sideSectionWeight = 0
	} else {
		if gui.State.ScreenMode == SCREEN_HALF {
			mainSectionWeight = 1
		} else if gui.State.ScreenMode == SCREEN_FULL {
			mainSectionWeight = 0
		}
	}

	return sideSectionWeight, mainSectionWeight
}

func (gui *Gui) infoSectionChildren(informationStr string, appStatus string) []*boxlayout.Box {
	result := []*boxlayout.Box{}

	if len(appStatus) > 0 {
		result = append(result,
			&boxlayout.Box{
				Window: "appStatus",
				Size:   runewidth.StringWidth(appStatus) + runewidth.StringWidth(INFO_SECTION_PADDING),
			},
		)
	}

	if gui.State.Filter.active {
		return append(result, []*boxlayout.Box{
			{
				Window: "filterPrefix",
				Size:   runewidth.StringWidth(gui.filterPrompt()),
			},
			{
				Window: "filter",
				Weight: 1,
			},
		}...)
	}

	result = append(result,
		[]*boxlayout.Box{
			{
				Window: "options",
				Weight: 1,
			},
			{
				Window: "information",
				// unlike appStatus, informationStr has various colors so we need to decolorise before taking the length
				Size: runewidth.StringWidth(INFO_SECTION_PADDING) + runewidth.StringWidth(utils.Decolorise(informationStr)),
			},
		}...,
	)

	return result
}

func (gui *Gui) sideViewNames() []string {
	visibleSidePanels := lo.Filter(gui.allSidePanels(), func(panel panels.ISideListPanel, _ int) bool {
		return !panel.IsHidden()
	})

	return lo.Map(visibleSidePanels, func(panel panels.ISideListPanel, _ int) string {
		return panel.GetView().Name()
	})
}

func (gui *Gui) sidePanelChildren(width int, height int) []*boxlayout.Box {
	currentWindow := gui.currentSideWindowName()
	sideWindowNames := gui.sideViewNames()

	if gui.State.ScreenMode == SCREEN_FULL || gui.State.ScreenMode == SCREEN_HALF {
		fullHeightBox := func(window string) *boxlayout.Box {
			if window == currentWindow {
				return &boxlayout.Box{
					Window: window,
					Weight: 1,
				}
			} else {
				return &boxlayout.Box{
					Window: window,
					Size:   0,
				}
			}
		}

		return lo.Map(sideWindowNames, func(window string, _ int) *boxlayout.Box {
			return fullHeightBox(window)
		})

	} else if height >= 28 {
		accordionMode := gui.Config.UserConfig.Gui.ExpandFocusedSidePanel
		accordionBox := func(defaultBox *boxlayout.Box) *boxlayout.Box {
			if accordionMode && defaultBox.Window == currentWindow {
				return &boxlayout.Box{
					Window: defaultBox.Window,
					Weight: 2,
				}
			}

			return defaultBox
		}

		return append([]*boxlayout.Box{
			{
				Window: sideWindowNames[0],
				Size:   3,
			},
		}, lo.Map(sideWindowNames[1:], func(window string, _ int) *boxlayout.Box {
			return accordionBox(&boxlayout.Box{Window: window, Weight: 1})
		})...)
	} else {
		squashedHeight := 1
		if height >= 21 {
			squashedHeight = 3
		}

		squashedSidePanelBox := func(window string) *boxlayout.Box {
			if window == currentWindow {
				return &boxlayout.Box{
					Window: window,
					Weight: 1,
				}
			} else {
				return &boxlayout.Box{
					Window: window,
					Size:   squashedHeight,
				}
			}
		}

		return lo.Map(sideWindowNames, func(window string, _ int) *boxlayout.Box {
			return squashedSidePanelBox(window)
		})
	}
}
