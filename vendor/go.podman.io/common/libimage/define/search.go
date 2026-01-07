package define

const (
	// SearchFilterAutomated is the key for filtering images by their automated attribute.
	SearchFilterAutomated = "is-automated"
	// SearchFilterOfficial is the key for filtering images by their official attribute.
	SearchFilterOfficial = "is-official"
	// SearchFilterStars is the key for filtering images by stars.
	SearchFilterStars = "stars"
)

// SearchFilters includes all supported search filters.
var SearchFilters = []string{SearchFilterAutomated, SearchFilterOfficial, SearchFilterStars}
