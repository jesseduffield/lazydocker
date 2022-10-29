package panels

import (
	"fmt"
	"strings"

	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

type ISideListPanel interface {
	SetContextIndex(int)
	HandleSelect() error
	GetView() *gocui.View
	Refocus()
	RerenderList() error
	IsFilterDisabled() bool
	IsHidden() bool
	HandleNextLine() error
	HandlePrevLine() error
	HandleClick() error
	HandlePrevContext() error
	HandleNextContext() error
}

// list panel at the side of the screen that renders content to the main panel
type SideListPanel[T comparable] struct {
	ContextState *ContextState[T]

	ListPanel[T]

	// message to render in the main view if there are no items in the panel
	// and it has focus. Leave empty if you don't want to render anything
	NoItemsMessage string

	Gui IGui

	// this Filter is applied on top of additional default filters
	Filter func(T) bool
	Sort   func(a, b T) bool

	OnClick func(T) error

	GetDisplayStrings func(T) []string

	// function to be called after re-rendering list. Can be nil
	OnRerender func() error

	// set this to true if you don't want to allow manual filtering via '/'
	DisableFilter bool

	// This can be nil if you want to always show the panel
	Hide func() bool
}

var _ ISideListPanel = &SideListPanel[int]{}

type ContextState[T any] struct {
	contextIdx int
	// contexts    []ContextConfig[T]
	GetContexts func() []ContextConfig[T]
	// this tells us whether we need to re-render to the main panel
	GetContextCacheKey func(item T) string
}

type ContextConfig[T any] struct {
	Key    string
	Title  string
	Render func(item T) error
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

func (self *SideListPanel[T]) HandleClick() error {
	itemCount := self.List.Len()
	handleSelect := self.HandleSelect
	selectedLine := &self.SelectedIdx

	if err := self.Gui.HandleClick(self.View, itemCount, selectedLine, handleSelect); err != nil {
		return err
	}

	if self.OnClick != nil {
		selectedItem, err := self.GetSelectedItem()
		if err == nil {
			return self.OnClick(selectedItem)
		}
	}

	return nil
}

func (self *SideListPanel[T]) GetView() *gocui.View {
	return self.View
}

func (self *SideListPanel[T]) HandleSelect() error {
	item, err := self.GetSelectedItem()
	if err != nil {
		if err.Error() != self.NoItemsMessage {
			return err
		}

		if self.NoItemsMessage != "" {
			return self.Gui.RenderStringMain(self.NoItemsMessage)
		}

		return nil
	}

	self.Refocus()

	return self.renderContext(item)
}

func (self *SideListPanel[T]) renderContext(item T) error {
	if self.ContextState == nil {
		return nil
	}

	key := self.ContextState.GetCurrentContextKey(item)
	if !self.Gui.ShouldRefresh(key) {
		return nil
	}

	mainView := self.Gui.GetMainView()
	mainView.Tabs = self.ContextState.GetContextTitles()
	mainView.TabIndex = self.ContextState.contextIdx

	return self.ContextState.GetCurrentContext().Render(item)
}

func (self *ContextState[T]) GetContextTitles() []string {
	return lo.Map(self.GetContexts(), func(context ContextConfig[T], _ int) string {
		return context.Title
	})
}

func (self *ContextState[T]) GetCurrentContextKey(item T) string {
	return self.GetContextCacheKey(item) + "-" + self.GetCurrentContext().Key
}

func (self *ContextState[T]) GetCurrentContext() ContextConfig[T] {
	return self.GetContexts()[self.contextIdx]
}

func (self *SideListPanel[T]) GetSelectedItem() (T, error) {
	var zero T

	item, ok := self.List.TryGet(self.SelectedIdx)
	if !ok {
		// could probably have a better error here
		return zero, errors.New(self.NoItemsMessage)
	}

	return item, nil
}

func (self *SideListPanel[T]) HandleNextLine() error {
	self.SelectNextLine()

	return self.HandleSelect()
}

func (self *SideListPanel[T]) HandlePrevLine() error {
	self.SelectPrevLine()

	return self.HandleSelect()
}

func (self *ContextState[T]) HandleNextContext() {
	contexts := self.GetContexts()

	if len(contexts) == 0 {
		return
	}

	self.contextIdx = (self.contextIdx + 1) % len(contexts)
}

func (self *ContextState[T]) HandlePrevContext() {
	contexts := self.GetContexts()

	if len(contexts) == 0 {
		return
	}

	self.contextIdx = (self.contextIdx - 1 + len(contexts)) % len(contexts)
}

func (self *SideListPanel[T]) HandleNextContext() error {
	if self.ContextState == nil {
		return nil
	}

	self.ContextState.HandleNextContext()

	return self.HandleSelect()
}

func (self *SideListPanel[T]) HandlePrevContext() error {
	if self.ContextState == nil {
		return nil
	}

	self.ContextState.HandlePrevContext()

	return self.HandleSelect()
}

func (self *SideListPanel[T]) Refocus() {
	self.Gui.FocusY(self.SelectedIdx, self.List.Len(), self.View)
}

func (self *SideListPanel[T]) SetItems(items []T) {
	self.List.SetItems(items)
	self.FilterAndSort()
}

func (self *SideListPanel[T]) FilterAndSort() {
	filterString := self.Gui.FilterString(self.View)

	self.List.Filter(func(item T, index int) bool {
		if self.Filter != nil && !self.Filter(item) {
			return false
		}

		if lo.SomeBy(self.Gui.IgnoreStrings(), func(ignore string) bool {
			return lo.SomeBy(self.GetDisplayStrings(item), func(searchString string) bool {
				return strings.Contains(searchString, ignore)
			})
		}) {
			return false
		}

		if filterString != "" {
			return lo.SomeBy(self.GetDisplayStrings(item), func(searchString string) bool {
				return strings.Contains(searchString, filterString)
			})
		}

		return true
	})

	self.List.Sort(self.Sort)

	self.clampSelectedLineIdx()
}

func (self *SideListPanel[T]) RerenderList() error {
	self.FilterAndSort()

	self.Gui.Update(func() error {
		self.View.Clear()
		table := lo.Map(self.List.GetItems(), func(item T, index int) []string {
			return self.GetDisplayStrings(item)
		})
		renderedTable, err := utils.RenderTable(table)
		if err != nil {
			return err
		}
		fmt.Fprint(self.View, renderedTable)

		if self.OnRerender != nil {
			if err := self.OnRerender(); err != nil {
				return err
			}
		}

		if self.View == self.Gui.CurrentView() {
			return self.HandleSelect()
		}
		return nil
	})

	return nil
}

func (self *SideListPanel[T]) SetContextIndex(index int) {
	if self.ContextState == nil {
		return
	}

	self.ContextState.SetContextIndex(index)
}

func (self *ContextState[T]) SetContextIndex(index int) {
	self.contextIdx = index
}

func (self *SideListPanel[T]) IsFilterDisabled() bool {
	return self.DisableFilter
}

func (self *SideListPanel[T]) IsHidden() bool {
	if self.Hide == nil {
		return false
	}

	return self.Hide()
}
