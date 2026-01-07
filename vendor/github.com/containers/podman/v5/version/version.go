package version

import (
	"github.com/blang/semver/v4"
	"github.com/containers/podman/v5/version/rawversion"
)

type (
	// Tree determines which API endpoint tree for version
	Tree int
	// Level determines which API level, current or something from the past
	Level int
)

const (
	// Libpod supports Libpod endpoints
	Libpod = Tree(iota)
	// Compat supports Libpod endpoints
	Compat

	// CurrentAPI announces what is the current API level
	CurrentAPI = Level(iota)
	// MinimalAPI announces what is the oldest API level supported
	MinimalAPI
)

// Version is the version of the build.
var Version = semver.MustParse(rawversion.RawVersion)

// See https://docs.docker.com/engine/api/v1.40/
// libpod compat handlers are expected to honor docker API versions

// APIVersion provides the current and minimal API versions for compat and libpod endpoint trees
// Note: GET|HEAD /_ping is never versioned and provides the API-Version and Libpod-API-Version headers to allow
//
//	clients to shop for the Version they wish to support
var APIVersion = map[Tree]map[Level]semver.Version{
	Libpod: {
		CurrentAPI: Version,
		MinimalAPI: semver.MustParse("4.0.0"),
	},
	Compat: {
		CurrentAPI: semver.MustParse("1.41.0"),
		MinimalAPI: semver.MustParse("1.24.0"),
	},
}
