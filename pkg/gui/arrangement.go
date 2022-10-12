package gui

import (
	"github.com/jesseduffield/lazycore/pkg/boxlayout"
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

	showInfoSection := gui.Config.UserConfig.Gui.ShowBottomLine
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

func MergeMaps[K comparable, V any](maps ...map[K]V) map[K]V {
	result := map[K]V{}
	for _, currMap := range maps {
		for key, value := range currMap {
			result[key] = value
		}
	}

	return result
}

func (gui *Gui) getMidSectionWeights() (int, int) {
	currentWindow := gui.currentWindow()

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
	if gui.DockerCommand.InDockerComposeProject {
		return []string{"project", "services", "containers", "images", "volumes"}
	} else {
		return []string{"project", "containers", "images", "volumes"}
	}
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

// TODO: reintroduce
// func (gui *Gui) currentSideWindowName() string {
// 	// there is always one and only one cyclable context in the context stack. We'll look from top to bottom
// 	gui.State.ContextManager.RLock()
// 	defer gui.State.ContextManager.RUnlock()

// 	for idx := range gui.State.ContextManager.ContextStack {
// 		reversedIdx := len(gui.State.ContextManager.ContextStack) - 1 - idx
// 		context := gui.State.ContextManager.ContextStack[reversedIdx]

// 		if context.GetKind() == types.SIDE_CONTEXT {
// 			return context.GetWindowName()
// 		}
// 	}

// 	return "files" // default
// }

func (gui *Gui) currentNonPopupWindowName() string {
	return gui.peekPreviousView()
}

// TODO: do this better.
func (gui *Gui) currentSideWindowName() string {
	windowName := gui.currentWindow()
	if !lo.Contains(gui.sideViewNames(), windowName) {
		return gui.peekPreviousView()
	}

	return windowName
}
