package gui

import (
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

type ListPanel[T comparable] struct {
	toColumns func(T) []string

	selectedIdx int
	list        FilteredList[T]
	view        *gocui.View
}

func (self *ListPanel[T]) setSelectedLineIdx(value int) {
	clampedValue := -1
	if self.list.Len() > 0 {
		clampedValue = utils.Clamp(value, 0, self.list.Len()-1)
	}

	self.selectedIdx = clampedValue
}

// moves the cursor up or down by the given amount (up for negative values)
func (self *ListPanel[T]) moveSelectedLine(delta int) {
	self.setSelectedLineIdx(self.selectedIdx + delta)
}

func (self *ListPanel[T]) SelectNextLine() {
	self.moveSelectedLine(1)
}

func (self *ListPanel[T]) SelectPrevLine() {
	self.moveSelectedLine(-1)
}

// list panel at the side of the screen that renders content to the main panel
type SideListPanel[T comparable] struct {
	contextKeyPrefix string

	ListPanel[T]

	contextIdx int
}
