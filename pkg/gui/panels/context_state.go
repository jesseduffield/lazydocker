package panels

import (
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/samber/lo"
)

// A 'context' generally corresponds to an item and the tab in the main panel that we're
// displaying. So if we switch to a new item, or change the tab in the panel panel
// for the current item, we end up with a new context. When we have a new context,
// we render new content to the main panel.
type ContextState[T any] struct {
	// index of the currently selected tab in the main view.
	mainTabIdx int
	// this function returns the tabs that we can display for an item (the tabs
	// are shown on the main view)
	GetMainTabs func() []MainTab[T]
	// This tells us whether we need to re-render to the main panel for a given item.
	// This should include the item's ID and if you want to invalidate the cache for
	// some other reason, you can add that to the key as well (e.g. the container's state).
	GetItemContextCacheKey func(item T) string
}

type MainTab[T any] struct {
	// key used as part of the context cache key
	Key string
	// title of the tab, rendered in the main view
	Title string
	// function to render the content of the tab
	Render func(item T) tasks.TaskFunc
}

func (self *ContextState[T]) GetMainTabTitles() []string {
	return lo.Map(self.GetMainTabs(), func(tab MainTab[T], _ int) string {
		return tab.Title
	})
}

func (self *ContextState[T]) GetCurrentContextKey(item T) string {
	return self.GetItemContextCacheKey(item) + "-" + self.GetCurrentMainTab().Key
}

func (self *ContextState[T]) GetCurrentMainTab() MainTab[T] {
	return self.GetMainTabs()[self.mainTabIdx]
}

func (self *ContextState[T]) HandleNextMainTab() {
	tabs := self.GetMainTabs()

	if len(tabs) == 0 {
		return
	}

	self.mainTabIdx = (self.mainTabIdx + 1) % len(tabs)
}

func (self *ContextState[T]) HandlePrevMainTab() {
	tabs := self.GetMainTabs()

	if len(tabs) == 0 {
		return
	}

	self.mainTabIdx = (self.mainTabIdx - 1 + len(tabs)) % len(tabs)
}

func (self *ContextState[T]) SetMainTabIndex(index int) {
	self.mainTabIdx = index
}
