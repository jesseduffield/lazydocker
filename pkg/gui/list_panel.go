package gui

import (
	"fmt"
	"strings"

	"github.com/go-errors/errors"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
)

type ListPanel[T comparable] struct {
	toColumns func(T) []string

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

	noItemsMessge string

	gui IGui

	contexts []ContextConfig[T]

	// returns strings that can be filtered on
	getSearchStrings func(item T) []string
	getId            func(item T) string

	sort func(a, b T) bool
}

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
	PopupPanelFocused() bool
	// TODO: replace with IsCurrentView() bool
	CurrentView() *gocui.View
	FilterString(view *gocui.View) string
	IgnoreStrings() []string
	Update(func() error)
}

func (gui *Gui) intoInterface() IGui {
	return gui
}

func (gui *Gui) getImagePanel() *SideListPanel[*commands.Image] {
	noneLabel := "<none>"

	return &SideListPanel[*commands.Image]{
		contextKeyPrefix: "images",
		ListPanel: ListPanel[*commands.Image]{
			toColumns: func(image *commands.Image) []string {
				return []string{
					image.Name,
					image.Tag,
					utils.FormatDecimalBytes(int(image.Image.Size)),
				}
			},
			list: NewFilteredList[*commands.Image](),
			view: gui.Views.Images,
		},
		contextIdx:    0,
		noItemsMessge: gui.Tr.NoImages,
		gui:           gui.intoInterface(),
		contexts: []ContextConfig[*commands.Image]{
			{
				key:   "config",
				title: gui.Tr.ConfigTitle,
				render: func(image *commands.Image) error {
					return gui.renderImageConfig(image)
				},
			},
		},
		getSearchStrings: func(image *commands.Image) []string {
			return []string{image.Name, image.Tag}
		},
		getId: func(image *commands.Image) string {
			return image.ID
		},
		sort: func(a *commands.Image, b *commands.Image) bool {
			if a.Name == noneLabel && b.Name != noneLabel {
				return false
			}

			if a.Name != noneLabel && b.Name == noneLabel {
				return true
			}

			return a.Name < b.Name
		},
	}
}

func (self *SideListPanel[T]) OnClick() error {
	itemCount := self.list.Len()
	handleSelect := self.HandleSelect
	selectedLine := &self.selectedIdx

	return self.gui.HandleClick(self.view, itemCount, selectedLine, handleSelect)
}

func (self *SideListPanel[T]) HandleSelect() error {
	item, err := self.GetSelectedItem()
	if err != nil {
		if err.Error() != self.noItemsMessge {
			return err
		}

		return self.gui.RenderStringMain(self.noItemsMessge)
	}

	self.Refocus()

	key := self.contextKeyPrefix + "-" + self.getId(item) + "-" + self.contexts[self.contextIdx].key
	if !self.gui.ShouldRefresh(key) {
		return nil
	}

	mainView := self.gui.GetMainView()
	mainView.Tabs = self.GetContextTitles()
	mainView.TabIndex = self.contextIdx

	// now I have an item. What do I do with it?
	return self.contexts[self.contextIdx].render(item)
}

func (self *SideListPanel[T]) GetContextTitles() []string {
	return lo.Map(self.contexts, func(context ContextConfig[T], _ int) string {
		return context.title
	})
}

func (self *SideListPanel[T]) GetSelectedItem() (T, error) {
	var zero T

	item, ok := self.list.TryGet(self.selectedIdx)
	if !ok {
		// could probably have a better error here
		return zero, errors.New(self.noItemsMessge)
	}

	return item, nil
}

func (self *SideListPanel[T]) OnNextLine() error {
	if self.ignoreKeypress() {
		return nil
	}

	self.SelectNextLine()

	return self.HandleSelect()
}

func (self *SideListPanel[T]) OnPrevLine() error {
	if self.ignoreKeypress() {
		return nil
	}

	self.SelectPrevLine()

	return self.HandleSelect()
}

func (self *SideListPanel[T]) ignoreKeypress() bool {
	return self.gui.PopupPanelFocused() || self.gui.CurrentView() != self.view
}

func (self *SideListPanel[T]) OnNextContext() error {
	self.contextIdx = (self.contextIdx + 1) % len(self.contexts)

	return self.HandleSelect()
}

func (self *SideListPanel[T]) OnPrevContext() error {
	self.contextIdx = (self.contextIdx - 1 + len(self.contexts)) % len(self.contexts)

	return self.HandleSelect()
}

func (self *SideListPanel[T]) Refocus() {
	self.gui.FocusY(self.selectedIdx, self.list.Len(), self.view)
}

func (self *SideListPanel[T]) RerenderList() error {
	filterString := self.gui.FilterString(self.view)

	self.list.Filter(func(item T, index int) bool {
		if lo.SomeBy(self.gui.IgnoreStrings(), func(ignore string) bool {
			return lo.SomeBy(self.getSearchStrings(item), func(searchString string) bool {
				return strings.Contains(searchString, ignore)
			})
		}) {
			return false
		}

		if filterString != "" {
			return lo.SomeBy(self.getSearchStrings(item), func(searchString string) bool {
				return strings.Contains(searchString, filterString)
			})
		}

		return true
	})

	self.list.Sort(self.sort)

	// TODO: use clamp?
	if self.list.Len()-1 < self.selectedIdx {
		self.selectedIdx = self.list.Len() - 1
	}

	self.gui.Update(func() error {
		self.view.Clear()
		isFocused := self.gui.CurrentView() == self.view
		list, err := utils.RenderList(self.list.GetItems(), utils.IsFocused(isFocused))
		if err != nil {
			return err
		}
		fmt.Fprint(self.view, list)

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
