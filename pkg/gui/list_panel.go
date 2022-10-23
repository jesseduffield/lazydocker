package gui

import (
	"fmt"
	"strings"

	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

type ListPanel[T comparable] struct {
	selectedIdx int
	list        *FilteredList[T]
	view        *gocui.View
}

func (self *ListPanel[T]) setSelectedLineIdx(value int) {
	clampedValue := 0
	if self.list.Len() > 0 {
		clampedValue = utils.Clamp(value, 0, self.list.Len()-1)
	}

	self.selectedIdx = clampedValue
}

func (self *ListPanel[T]) clampSelectedLineIdx() {
	clamped := utils.Clamp(self.selectedIdx, 0, self.list.Len()-1)

	if clamped != self.selectedIdx {
		self.selectedIdx = clamped
	}
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
	contextIdx       int
	// contexts    []ContextConfig[T]
	getContexts func() []ContextConfig[T]
	// this tells us whether we need to re-render to the main panel
	getContextCacheKey func(item T) string

	ListPanel[T]

	// message to render in the main view if there are no items in the panel
	// and it has focus. Leave empty if you don't want to render anything
	noItemsMessage string

	gui IGui

	// this filter is applied on top of additional default filters
	filter func(T) bool
	sort   func(a, b T) bool

	onClick func(T) error

	getDisplayStrings func(T) []string

	// function to be called after re-rendering list. Can be nil
	onRerender func() error
}

type ISideListPanel interface {
	SetContextIndex(int)
	HandleSelect() error
	View() *gocui.View
	Refocus()
	RerenderList() error
}

var _ ISideListPanel = &SideListPanel[int]{}

type ContextConfig[T any] struct {
	key    string
	title  string
	render func(item T) error
}

type IGui interface {
	HandleClick(v *gocui.View, itemCount int, selectedLine *int, handleSelect func() error) error
	RenderStringMain(message string) error
	FocusY(selectedLine int, itemCount int, view *gocui.View)
	ShouldRefresh(key string) bool
	GetMainView() *gocui.View
	// TODO: replace with IsCurrentView() bool
	CurrentView() *gocui.View
	FilterString(view *gocui.View) string
	IgnoreStrings() []string
	Update(func() error)
}

func (gui *Gui) intoInterface() IGui {
	return gui
}

func (self *SideListPanel[T]) OnClick() error {
	itemCount := self.list.Len()
	handleSelect := self.HandleSelect
	selectedLine := &self.selectedIdx

	if err := self.gui.HandleClick(self.view, itemCount, selectedLine, handleSelect); err != nil {
		return err
	}

	if self.onClick != nil {
		selectedItem, err := self.GetSelectedItem()
		if err == nil {
			return self.onClick(selectedItem)
		}
	}

	return nil
}

func (self *SideListPanel[T]) View() *gocui.View {
	return self.view
}

func (self *SideListPanel[T]) HandleSelect() error {
	item, err := self.GetSelectedItem()
	if err != nil {
		if err.Error() != self.noItemsMessage {
			return err
		}

		if self.noItemsMessage != "" {
			return self.gui.RenderStringMain(self.noItemsMessage)
		}

		return nil
	}

	self.Refocus()

	return self.renderContext(item)
}

func (self *SideListPanel[T]) renderContext(item T) error {
	contexts := self.getContexts()

	if len(contexts) == 0 {
		return nil
	}

	key := self.contextKeyPrefix + "-" + self.getContextCacheKey(item) + "-" + contexts[self.contextIdx].key
	if !self.gui.ShouldRefresh(key) {
		return nil
	}

	mainView := self.gui.GetMainView()
	mainView.Tabs = self.GetContextTitles()
	mainView.TabIndex = self.contextIdx

	return contexts[self.contextIdx].render(item)
}

func (self *SideListPanel[T]) GetContextTitles() []string {
	return lo.Map(self.getContexts(), func(context ContextConfig[T], _ int) string {
		return context.title
	})
}

func (self *SideListPanel[T]) GetSelectedItem() (T, error) {
	var zero T

	item, ok := self.list.TryGet(self.selectedIdx)
	if !ok {
		// could probably have a better error here
		return zero, errors.New(self.noItemsMessage)
	}

	return item, nil
}

func (self *SideListPanel[T]) OnNextLine() error {
	self.SelectNextLine()

	return self.HandleSelect()
}

func (self *SideListPanel[T]) OnPrevLine() error {
	self.SelectPrevLine()

	return self.HandleSelect()
}

func (self *SideListPanel[T]) OnNextContext() error {
	contexts := self.getContexts()

	if len(contexts) == 0 {
		return nil
	}

	self.contextIdx = (self.contextIdx + 1) % len(contexts)

	return self.HandleSelect()
}

func (self *SideListPanel[T]) OnPrevContext() error {
	contexts := self.getContexts()

	if len(contexts) == 0 {
		return nil
	}

	self.contextIdx = (self.contextIdx - 1 + len(contexts)) % len(contexts)

	return self.HandleSelect()
}

func (self *SideListPanel[T]) Refocus() {
	self.gui.FocusY(self.selectedIdx, self.list.Len(), self.view)
}

func (self *SideListPanel[T]) SetItems(items []T) {
	self.list.SetItems(items)
	self.FilterAndSort()
}

func (self *SideListPanel[T]) FilterAndSort() {
	filterString := self.gui.FilterString(self.view)

	self.list.Filter(func(item T, index int) bool {
		if self.filter != nil && !self.filter(item) {
			return false
		}

		if lo.SomeBy(self.gui.IgnoreStrings(), func(ignore string) bool {
			return lo.SomeBy(self.getDisplayStrings(item), func(searchString string) bool {
				return strings.Contains(searchString, ignore)
			})
		}) {
			return false
		}

		if filterString != "" {
			return lo.SomeBy(self.getDisplayStrings(item), func(searchString string) bool {
				return strings.Contains(searchString, filterString)
			})
		}

		return true
	})

	self.list.Sort(self.sort)

	self.clampSelectedLineIdx()
}

func (self *SideListPanel[T]) RerenderList() error {
	self.FilterAndSort()

	self.gui.Update(func() error {
		self.view.Clear()
		table := lo.Map(self.list.GetItems(), func(item T, index int) []string {
			return self.getDisplayStrings(item)
		})
		renderedTable, err := utils.RenderTable(table)
		if err != nil {
			return err
		}
		fmt.Fprint(self.view, renderedTable)

		if self.onRerender != nil {
			if err := self.onRerender(); err != nil {
				return err
			}
		}

		if self.view == self.gui.CurrentView() {
			return self.HandleSelect()
		}
		return nil
	})

	return nil
}

func (self *SideListPanel[T]) SetContextIndex(index int) {
	self.contextIdx = index
}
