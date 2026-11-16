package panels

import (
	"context"
	"testing"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/tasks"
	"github.com/stretchr/testify/assert"
)

// renders tasks straight away, and caches the main view's context key like the
// real gui
type fakeGui struct {
	contextKey string
	rendered   []string
}

func (gui *fakeGui) HandleClick(*gocui.View, int, *int, func() error) error { return nil }
func (gui *fakeGui) FocusY(int, int, *gocui.View)                           {}
func (gui *fakeGui) GetMainView() *gocui.View                               { return &gocui.View{} }
func (gui *fakeGui) IsCurrentView(*gocui.View) bool                         { return true }
func (gui *fakeGui) FilterString(*gocui.View) string                        { return "" }
func (gui *fakeGui) IgnoreStrings() []string                                { return nil }
func (gui *fakeGui) Update(f func() error)                                  { _ = f() }

func (gui *fakeGui) NewSimpleRenderStringTask(getContent func() string) tasks.TaskFunc {
	return func(context.Context) { gui.rendered = append(gui.rendered, getContent()) }
}

func (gui *fakeGui) QueueTask(f func(ctx context.Context)) error {
	f(context.Background())
	return nil
}

func (gui *fakeGui) ShouldRefresh(key string) bool {
	if gui.contextKey == key {
		return false
	}
	gui.contextKey = key
	return true
}

func TestSideListPanelNoItemsMessage(t *testing.T) {
	gui := &fakeGui{}
	panel := &SideListPanel[string]{
		ContextState: &ContextState[string]{
			GetMainTabs: func() []MainTab[string] {
				return []MainTab[string]{{Key: "info", Render: func(item string) tasks.TaskFunc {
					return gui.NewSimpleRenderStringTask(func() string { return item })
				}}}
			},
			GetItemContextCacheKey: func(item string) string { return "items-" + item },
		},
		ListPanel:      ListPanel[string]{List: NewFilteredList[string](), View: &gocui.View{}},
		NoItemsMessage: "No items",
		Gui:            gui,
		GetTableCells:  func(item string) []string { return []string{item} },
	}

	panel.SetItems([]string{"a"})
	assert.NoError(t, panel.HandleSelect())

	// e.g. a filter that matches nothing. The message is rendered once, not
	// on every refresh
	panel.SetItems(nil)
	assert.NoError(t, panel.HandleSelect())
	assert.NoError(t, panel.HandleSelect())

	// the same item is selected again, so it has to be rendered again
	panel.SetItems([]string{"a"})
	assert.NoError(t, panel.HandleSelect())

	assert.Equal(t, []string{"a", "No items", "a"}, gui.rendered)
}
