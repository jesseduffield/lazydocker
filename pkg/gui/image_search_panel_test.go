package gui

import (
	"testing"

	"github.com/docker/docker/api/types/registry"
	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func newImageSearchTestGui(t *testing.T) (*Gui, *gocui.Gui) {
	t.Helper()

	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:      gocui.OutputTrue,
		SupportOverlaps: true,
		Headless:        true,
		Width:           140,
		Height:          48,
	})
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	t.Cleanup(func() { g.Close() })

	log := logrus.New().WithField("test", true)
	return &Gui{
		g:     g,
		Log:   log,
		Tr:    i18n.NewTranslationSet(log, "en"),
		State: guiState{ImageSearch: &imageSearchState{}},
	}, g
}

func TestImageSearchPanelUsesResultsInputDetailsLayout(t *testing.T) {
	gui, g := newImageSearchTestGui(t)

	assert.NoError(t, gui.layoutImageSearchPanel(g))

	results, err := g.View(imageSearchResultsViewName)
	assert.NoError(t, err)
	input, err := g.View(imageSearchInputViewName)
	assert.NoError(t, err)
	details, err := g.View(imageSearchDetailsViewName)
	assert.NoError(t, err)

	_, _, _, resultsBottom := results.Dimensions()
	_, inputTop, _, inputBottom := input.Dimensions()
	_, detailsTop, _, _ := details.Dimensions()
	assert.Less(t, resultsBottom, inputTop)
	assert.Equal(t, inputTop+2, inputBottom)
	assert.Less(t, inputBottom, detailsTop)
}

func TestImageSearchPanelRendersSelectionAndDetails(t *testing.T) {
	gui, g := newImageSearchTestGui(t)
	assert.NoError(t, gui.layoutImageSearchPanel(g))

	gui.State.ImageSearch.Query = "nginx"
	gui.State.ImageSearch.Results = []registry.SearchResult{
		{Name: "nginx", Description: "Official NGINX image", StarCount: 21000, IsOfficial: true},
		{Name: "bitnami/nginx", Description: "Bitnami NGINX image", StarCount: 200},
	}
	gui.renderImageSearchPanel(g)

	results, _ := g.View(imageSearchResultsViewName)
	details, _ := g.View(imageSearchDetailsViewName)
	assert.Contains(t, results.Buffer(), "> ")
	assert.Contains(t, results.Buffer(), "nginx")
	assert.Contains(t, details.Buffer(), "Name:")
	assert.Contains(t, details.Buffer(), "Official NGINX image")
}

func TestImageSearchPanelRendersTagMetadata(t *testing.T) {
	gui, g := newImageSearchTestGui(t)
	gui.State.ImageSearch.Mode = imageSearchModeTags
	gui.State.ImageSearch.SelectedImage = registry.SearchResult{Name: "nginx"}
	assert.NoError(t, gui.layoutImageSearchPanel(g))

	gui.State.ImageSearch.Tags = []commands.DockerHubTag{
		{
			Name:          "1.27-alpine",
			FullSize:      75270706,
			TagLastPushed: "2026-07-15T12:30:00Z",
			Digest:        "sha256:abc123",
			MediaType:     "application/vnd.oci.image.index.v1+json",
			Images: []commands.DockerHubTagVariant{
				{OS: "linux", Architecture: "amd64", Size: 75270706},
				{OS: "linux", Architecture: "arm64", Variant: "v8", Size: 70000000},
				{OS: "unknown", Architecture: "unknown"},
			},
		},
	}
	gui.renderImageSearchPanel(g)

	results, _ := g.View(imageSearchResultsViewName)
	details, _ := g.View(imageSearchDetailsViewName)
	input, _ := g.View(imageSearchInputViewName)
	assert.Contains(t, results.Buffer(), "1.27-alpine")
	assert.Contains(t, results.Buffer(), "2 platforms")
	assert.Contains(t, details.Buffer(), "nginx:1.27-alpine")
	assert.Contains(t, details.Buffer(), "linux/amd64")
	assert.Contains(t, details.Buffer(), "linux/arm64/v8")
	assert.Contains(t, details.Buffer(), "sha256:abc123")
	assert.Contains(t, input.Title, "nginx")
}

func TestImageTagPlatformsAreUniqueAndSorted(t *testing.T) {
	tag := commands.DockerHubTag{Images: []commands.DockerHubTagVariant{
		{OS: "linux", Architecture: "arm64", Variant: "v8"},
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "amd64"},
		{OS: "unknown", Architecture: "unknown"},
	}}

	assert.Equal(t, []string{"linux/amd64", "linux/arm64/v8"}, imageTagPlatforms(tag))
}

func TestTruncateImageSearchTextPreservesUnicode(t *testing.T) {
	assert.Equal(t, "конт...", truncateImageSearchText("контейнер", 7))
}
