package gui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/docker/docker/api/types/registry"
	"github.com/docker/go-units"
	"github.com/fatih/color"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

type imageSearchMode int

const (
	imageSearchModeRepositories imageSearchMode = iota
	imageSearchModeTags
)

const (
	imageSearchResultsViewName = "imageSearchResults"
	imageSearchInputViewName   = "imageSearchInput"
	imageSearchDetailsViewName = "imageSearchDetails"
	imageSearchDebounce        = 250 * time.Millisecond
)

type imageSearchState struct {
	Mode          imageSearchMode
	Active        bool
	Query         string
	Results       []registry.SearchResult
	Tags          []commands.DockerHubTag
	SelectedImage registry.SearchResult
	SelectedLine  int
	RequestID     uint64
	Searching     bool
	SearchError   string
	WorkerRunning bool
	mu            sync.Mutex
}

func (gui *Gui) handleSearchImages(g *gocui.Gui, v *gocui.View) error {
	gui.onNewPopupPanel()
	state := gui.State.ImageSearch
	state.mu.Lock()
	state.Mode = imageSearchModeRepositories
	state.Active = true
	state.Query = ""
	state.Results = nil
	state.Tags = nil
	state.SelectedImage = registry.SearchResult{}
	state.SelectedLine = 0
	state.RequestID++
	state.Searching = false
	state.SearchError = ""
	state.mu.Unlock()

	gui.g.Update(func(g *gocui.Gui) error {
		if err := gui.layoutImageSearchPanel(g); err != nil {
			return err
		}

		inputView, err := g.View(imageSearchInputViewName)
		if err != nil {
			return err
		}
		inputView.ClearTextArea()
		_ = inputView.SetCursor(0, 0)
		_ = inputView.SetOrigin(0, 0)
		inputView.Editor = gocui.EditorFunc(func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
			matched := gocui.DefaultEditor.Edit(v, key, ch, mod)
			if matched {
				gui.handleImageSearchQueryChange(v.Buffer())
			}
			return matched
		})

		gui.bindImageSearchKeys(g)
		gui.renderImageSearchPanel(g)
		return gui.switchFocus(inputView)
	})

	return nil
}

func (gui *Gui) layoutImageSearchPanel(g *gocui.Gui) error {
	width, height := g.Size()
	x0 := width / 14
	x1 := width - x0 - 1
	y0 := height / 16
	y1 := height - y0 - 3

	if height < 15 {
		y0 = 0
		y1 = height - 2
	}

	resultsBottom := y0 + (y1-y0)/3
	if resultsBottom < y0+3 {
		resultsBottom = y0 + 3
	}
	inputTop := resultsBottom + 1
	inputBottom := inputTop + 2
	detailsTop := inputBottom + 1
	detailsBottom := y1
	if detailsTop >= y1 {
		detailsTop = height + 1000
		detailsBottom = detailsTop + 2
	}
	inputTitle, detailsTitle := gui.imageSearchPanelTitles()

	resultsView, err := g.SetView(imageSearchResultsViewName, x0, y0, x1, resultsBottom, 0)
	if err != nil && err.Error() != UNKNOWN_VIEW_ERROR_MSG {
		return err
	}
	resultsView.FgColor = gocui.ColorDefault
	resultsView.Wrap = false

	inputView, err := g.SetView(imageSearchInputViewName, x0, inputTop, x1, inputBottom, 0)
	if err != nil && err.Error() != UNKNOWN_VIEW_ERROR_MSG {
		return err
	}
	inputView.Title = inputTitle
	inputView.FgColor = gocui.ColorDefault
	inputView.Editable = true
	inputView.Wrap = false

	detailsView, err := g.SetView(imageSearchDetailsViewName, x0, detailsTop, x1, detailsBottom, 0)
	if err != nil && err.Error() != UNKNOWN_VIEW_ERROR_MSG {
		return err
	}
	detailsView.Title = detailsTitle
	detailsView.FgColor = gocui.ColorDefault
	detailsView.Wrap = true

	_, _ = g.SetViewOnTop(imageSearchResultsViewName)
	_, _ = g.SetViewOnTop(imageSearchDetailsViewName)
	_, _ = g.SetViewOnTop(imageSearchInputViewName)
	return nil
}

func (gui *Gui) bindImageSearchKeys(g *gocui.Gui) {
	g.DeleteViewKeybindings(imageSearchInputViewName)
	bindings := []struct {
		key     interface{}
		handler func(*gocui.Gui, *gocui.View) error
	}{
		{gocui.KeyArrowUp, gui.handleImageSearchPrevious},
		{gocui.KeyArrowDown, gui.handleImageSearchNext},
		{gocui.KeyCtrlP, gui.handleImageSearchPrevious},
		{gocui.KeyCtrlN, gui.handleImageSearchNext},
		{gocui.KeyEnter, gui.handleImageSearchSelect},
		{gocui.KeyEsc, gui.closeImageSearchPanel},
	}

	for _, binding := range bindings {
		_ = g.SetKeybinding(imageSearchInputViewName, binding.key, gocui.ModNone, binding.handler)
	}
}

func (gui *Gui) handleImageSearchQueryChange(rawQuery string) {
	query := strings.TrimSpace(rawQuery)
	state := gui.State.ImageSearch
	state.mu.Lock()
	if !state.Active || query == state.Query {
		state.mu.Unlock()
		return
	}

	state.Query = query
	state.RequestID++
	state.SelectedLine = 0
	state.Results = nil
	state.Tags = nil
	state.SearchError = ""
	shouldSearch := query != "" || state.Mode == imageSearchModeTags
	state.Searching = shouldSearch
	startWorker := shouldSearch && !state.WorkerRunning
	if startWorker {
		state.WorkerRunning = true
	}
	state.mu.Unlock()

	gui.renderImageSearchPanel(gui.g)
	if startWorker {
		go gui.runImageSearchWorker()
	}
}

func (gui *Gui) runImageSearchWorker() {
	state := gui.State.ImageSearch
	for {
		time.Sleep(imageSearchDebounce)

		state.mu.Lock()
		if !state.Active || (state.Mode == imageSearchModeRepositories && state.Query == "") {
			state.WorkerRunning = false
			state.mu.Unlock()
			return
		}
		query := state.Query
		requestID := state.RequestID
		mode := state.Mode
		imageName := state.SelectedImage.Name
		state.mu.Unlock()

		var (
			results []registry.SearchResult
			tags    []commands.DockerHubTag
			err     error
		)
		if mode == imageSearchModeTags {
			tags, err = gui.DockerCommand.SearchImageTags(context.Background(), imageName, query)
		} else {
			results, err = gui.DockerCommand.SearchImages(context.Background(), query)
		}

		state.mu.Lock()
		if !state.Active {
			state.WorkerRunning = false
			state.mu.Unlock()
			return
		}
		if state.RequestID != requestID {
			state.mu.Unlock()
			continue
		}

		state.Searching = false
		state.WorkerRunning = false
		if err != nil {
			state.SearchError = truncateImageSearchText(imageSearchOneLine(err.Error()), 160)
			state.Results = nil
			state.Tags = nil
		} else {
			state.SearchError = ""
			if mode == imageSearchModeTags {
				state.Tags = tags
			} else {
				state.Results = results
			}
		}
		state.mu.Unlock()

		gui.g.Update(func(g *gocui.Gui) error {
			gui.renderImageSearchPanel(g)
			return nil
		})
		return
	}
}

func (gui *Gui) handleImageSearchPrevious(g *gocui.Gui, v *gocui.View) error {
	return gui.moveImageSearchSelection(g, -1)
}

func (gui *Gui) handleImageSearchNext(g *gocui.Gui, v *gocui.View) error {
	return gui.moveImageSearchSelection(g, 1)
}

func (gui *Gui) moveImageSearchSelection(g *gocui.Gui, change int) error {
	state := gui.State.ImageSearch
	state.mu.Lock()
	defer state.mu.Unlock()
	itemCount := len(state.Results)
	if state.Mode == imageSearchModeTags {
		itemCount = len(state.Tags)
	}
	if itemCount == 0 {
		return nil
	}

	state.SelectedLine += change
	if state.SelectedLine < 0 {
		state.SelectedLine = 0
	}
	if state.SelectedLine >= itemCount {
		state.SelectedLine = itemCount - 1
	}
	gui.renderImageSearchPanelLocked(g, state)
	return nil
}

func (gui *Gui) handleImageSearchSelect(g *gocui.Gui, v *gocui.View) error {
	state := gui.State.ImageSearch
	state.mu.Lock()
	if state.Mode == imageSearchModeRepositories {
		if len(state.Results) == 0 || state.SelectedLine < 0 || state.SelectedLine >= len(state.Results) {
			state.mu.Unlock()
			return nil
		}
		result := state.Results[state.SelectedLine]
		state.mu.Unlock()
		return gui.beginImageTagSearch(g, result)
	}

	if len(state.Tags) == 0 || state.SelectedLine < 0 || state.SelectedLine >= len(state.Tags) {
		state.mu.Unlock()
		return nil
	}
	tag := state.Tags[state.SelectedLine]
	imageName := state.SelectedImage.Name
	state.mu.Unlock()

	if err := gui.closeImageSearchPanel(g, v); err != nil {
		return err
	}
	return gui.pullImageReference(imageName + ":" + tag.Name)
}

func (gui *Gui) beginImageTagSearch(g *gocui.Gui, result registry.SearchResult) error {
	state := gui.State.ImageSearch
	state.mu.Lock()
	state.Mode = imageSearchModeTags
	state.SelectedImage = result
	state.Query = ""
	state.Results = nil
	state.Tags = nil
	state.SelectedLine = 0
	state.RequestID++
	state.Searching = true
	state.SearchError = ""
	startWorker := !state.WorkerRunning
	if startWorker {
		state.WorkerRunning = true
	}
	state.mu.Unlock()

	inputView, err := g.View(imageSearchInputViewName)
	if err != nil {
		return err
	}
	inputView.ClearTextArea()
	_ = inputView.SetCursor(0, 0)
	_ = inputView.SetOrigin(0, 0)
	if err := gui.layoutImageSearchPanel(g); err != nil {
		return err
	}
	gui.renderImageSearchPanel(g)
	if err := gui.renderImageSearchOptions(); err != nil {
		return err
	}
	if startWorker {
		go gui.runImageSearchWorker()
	}
	return nil
}

func (gui *Gui) closeImageSearchPanel(g *gocui.Gui, v *gocui.View) error {
	state := gui.State.ImageSearch
	state.mu.Lock()
	state.Active = false
	state.RequestID++
	state.mu.Unlock()

	g.DeleteViewKeybindings(imageSearchInputViewName)
	for _, viewName := range []string{imageSearchInputViewName, imageSearchResultsViewName, imageSearchDetailsViewName} {
		_ = g.DeleteView(viewName)
	}
	return gui.returnFocus()
}

func (gui *Gui) renderImageSearchPanel(g *gocui.Gui) {
	state := gui.State.ImageSearch
	state.mu.Lock()
	defer state.mu.Unlock()
	gui.renderImageSearchPanelLocked(g, state)
}

func (gui *Gui) renderImageSearchPanelLocked(g *gocui.Gui, state *imageSearchState) {
	resultsView, resultsErr := g.View(imageSearchResultsViewName)
	detailsView, detailsErr := g.View(imageSearchDetailsViewName)
	if resultsErr != nil || detailsErr != nil {
		return
	}

	resultsView.Title = gui.imageSearchResultsTitle(state)
	resultsView.Clear()
	if state.Mode == imageSearchModeTags {
		gui.renderImageTagResultsLocked(resultsView, state)
	} else {
		gui.renderImageRepositoryResultsLocked(resultsView, state)
	}

	detailsView.Clear()
	_ = detailsView.SetOrigin(0, 0)
	_ = detailsView.SetCursor(0, 0)
	if state.Mode == imageSearchModeTags && len(state.Tags) > 0 && state.SelectedLine >= 0 && state.SelectedLine < len(state.Tags) {
		fmt.Fprint(detailsView, gui.imageTagSearchDetails(state.SelectedImage.Name, state.Tags[state.SelectedLine]))
	} else if state.Mode == imageSearchModeRepositories && len(state.Results) > 0 && state.SelectedLine >= 0 && state.SelectedLine < len(state.Results) {
		fmt.Fprint(detailsView, gui.imageSearchDetails(state.Results[state.SelectedLine]))
	}
}

func (gui *Gui) renderImageRepositoryResultsLocked(resultsView *gocui.View, state *imageSearchState) {
	if state.Query == "" {
		fmt.Fprint(resultsView, gui.Tr.ImageSearchHint)
	} else if state.Searching {
		fmt.Fprintf(resultsView, "%s %s...", gui.Tr.SearchingImagesStatus, utils.ColoredString(state.Query, color.FgCyan))
	} else if state.SearchError != "" {
		fmt.Fprint(resultsView, utils.ColoredString(gui.Tr.ImageSearchFailed+": "+state.SearchError, color.FgRed))
	} else if len(state.Results) == 0 {
		fmt.Fprintf(resultsView, "%s: %s", gui.Tr.NoImageSearchResults, utils.ColoredString(state.Query, color.FgCyan))
	} else {
		rows := make([][]string, 0, len(state.Results))
		for index, result := range state.Results {
			marker := "  "
			nameColor := color.FgGreen
			if index == state.SelectedLine {
				marker = "> "
				nameColor = color.FgCyan
			}

			official := ""
			if result.IsOfficial {
				official = gui.Tr.OfficialImage
			}
			rows = append(rows, []string{
				marker + utils.ColoredString(result.Name, nameColor),
				utils.ColoredString(fmt.Sprintf("★ %d", result.StarCount), color.FgMagenta),
				official,
				truncateImageSearchText(imageSearchOneLine(result.Description), 72),
			})
		}
		renderImageSearchRows(resultsView, rows, state.SelectedLine)
	}
}

func (gui *Gui) renderImageTagResultsLocked(resultsView *gocui.View, state *imageSearchState) {
	if state.Searching {
		if state.Query == "" {
			fmt.Fprintf(resultsView, "%s...", gui.Tr.SearchingImageTagsStatus)
		} else {
			fmt.Fprintf(resultsView, "%s %s...", gui.Tr.SearchingImageTagsStatus, utils.ColoredString(state.Query, color.FgCyan))
		}
	} else if state.SearchError != "" {
		fmt.Fprint(resultsView, utils.ColoredString(gui.Tr.ImageTagSearchFailed+": "+state.SearchError, color.FgRed))
	} else if len(state.Tags) == 0 {
		fmt.Fprint(resultsView, gui.Tr.NoImageTagsFound)
	} else {
		rows := make([][]string, 0, len(state.Tags))
		for index, tag := range state.Tags {
			marker := "  "
			nameColor := color.FgGreen
			if index == state.SelectedLine {
				marker = "> "
				nameColor = color.FgCyan
			}
			rows = append(rows, []string{
				marker + utils.ColoredString(tag.Name, nameColor),
				units.HumanSize(float64(tag.FullSize)),
				fmt.Sprintf("%d platforms", len(imageTagPlatforms(tag))),
				formatImageTagDate(tag.TagLastPushed),
			})
		}
		renderImageSearchRows(resultsView, rows, state.SelectedLine)
	}
}

func renderImageSearchRows(resultsView *gocui.View, rows [][]string, selectedLine int) {
	rendered, err := utils.RenderTable(rows)
	if err == nil {
		fmt.Fprint(resultsView, rendered)
	}
	resultsView.FocusPoint(0, selectedLine)
}

func (gui *Gui) imageSearchResultsTitle(state *imageSearchState) string {
	title := gui.Tr.ImagesTitle
	itemCount := len(state.Results)
	if state.Mode == imageSearchModeTags {
		title = gui.Tr.ImageTagsTitle
		itemCount = len(state.Tags)
	}
	if itemCount == 0 {
		return title
	}
	return fmt.Sprintf("%s — %d of %d", title, state.SelectedLine+1, itemCount)
}

func (gui *Gui) imageSearchDetails(result registry.SearchResult) string {
	lines := []string{
		fmt.Sprintf("Name: %s", utils.ColoredString(result.Name, color.FgCyan)),
		fmt.Sprintf("Stars: %s", utils.ColoredString(fmt.Sprintf("%d", result.StarCount), color.FgMagenta)),
	}
	if result.IsOfficial {
		lines = append(lines, fmt.Sprintf("Type: %s", gui.Tr.OfficialImage))
	}
	if result.Description != "" {
		lines = append(lines, fmt.Sprintf("Description: %s", imageSearchOneLine(result.Description)))
	}
	return strings.Join(lines, "\n")
}

func (gui *Gui) imageTagSearchDetails(imageName string, tag commands.DockerHubTag) string {
	reference := imageName + ":" + tag.Name
	lines := []string{
		fmt.Sprintf("Reference: %s", utils.ColoredString(reference, color.FgGreen)),
		fmt.Sprintf("Size: %s", utils.ColoredString(units.HumanSize(float64(tag.FullSize)), color.FgMagenta)),
	}
	if pushed := formatImageTagDate(tag.TagLastPushed); pushed != "" {
		lines = append(lines, "Last pushed: "+pushed)
	}
	if platforms := imageTagPlatforms(tag); len(platforms) > 0 {
		lines = append(lines, "Platforms: "+strings.Join(platforms, ", "))
	}
	if tag.Digest != "" {
		lines = append(lines, "Digest: "+tag.Digest)
	}
	if tag.MediaType != "" {
		lines = append(lines, "Media type: "+tag.MediaType)
	}
	return strings.Join(lines, "\n")
}

func (gui *Gui) imageSearchPanelTitles() (string, string) {
	state := gui.State.ImageSearch
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Mode == imageSearchModeTags {
		return fmt.Sprintf("%s %s", gui.Tr.SearchImageTagsTitle, state.SelectedImage.Name), gui.Tr.ImageTagDetailsTitle
	}
	return gui.Tr.SearchImagesTitle, gui.Tr.ImageSearchDetailsTitle
}

func (gui *Gui) renderImageSearchOptions() error {
	state := gui.State.ImageSearch
	state.mu.Lock()
	enterAction := gui.Tr.SelectImage
	if state.Mode == imageSearchModeTags {
		enterAction = gui.Tr.PullImage
	}
	state.mu.Unlock()

	return gui.renderOptionsMap(map[string]string{
		"↑/↓":   gui.Tr.Navigate,
		"enter": enterAction,
		"esc":   gui.Tr.Close,
	})
}

func (gui *Gui) pullImageReference(reference string) error {
	status := fmt.Sprintf("%s %s", gui.Tr.PullingImageStatus, reference)
	return gui.WithWaitingStatus(status, func() error {
		if err := gui.DockerCommand.PullImage(reference); err != nil {
			return err
		}
		return gui.reloadImages()
	})
}

func imageTagPlatforms(tag commands.DockerHubTag) []string {
	platformSet := map[string]struct{}{}
	for _, image := range tag.Images {
		if image.OS == "" || image.OS == "unknown" || image.Architecture == "" || image.Architecture == "unknown" {
			continue
		}
		platform := image.OS + "/" + image.Architecture
		if image.Variant != "" {
			platform += "/" + image.Variant
		}
		platformSet[platform] = struct{}{}
	}
	platforms := make([]string, 0, len(platformSet))
	for platform := range platformSet {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	return platforms
}

func formatImageTagDate(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

func imageSearchOneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateImageSearchText(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	if limit <= 3 {
		return strings.Repeat(".", limit)
	}
	runes := []rune(value)
	return string(runes[:limit-3]) + "..."
}
