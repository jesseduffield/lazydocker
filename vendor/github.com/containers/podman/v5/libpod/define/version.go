package define

import (
	"runtime"
	"strconv"
	"time"

	"github.com/containers/podman/v5/version"
)

// Overwritten at build time
var (
	// GitCommit is the commit that the binary is being built from.
	// It will be populated by the Makefile.
	gitCommit string
	// BuildInfo is the time at which the binary was built
	// It will be populated by the Makefile.
	buildInfo string
	// BuildOrigin is the packager of the binary.
	// It will be populated at build-time.
	buildOrigin string
)

// Version is an output struct for API
type Version struct {
	APIVersion  string
	Version     string
	GoVersion   string
	GitCommit   string
	BuiltTime   string
	Built       int64
	BuildOrigin string `json:",omitempty" yaml:",omitempty"`
	OsArch      string
	Os          string
}

// GetVersion returns a VersionOutput struct for API and podman
func GetVersion() (Version, error) {
	var err error
	var buildTime int64
	if buildInfo != "" {
		// Converts unix time from string to int64
		buildTime, err = strconv.ParseInt(buildInfo, 10, 64)

		if err != nil {
			return Version{}, err
		}
	}
	return Version{
		APIVersion:  version.APIVersion[version.Libpod][version.CurrentAPI].String(),
		Version:     version.Version.String(),
		GoVersion:   runtime.Version(),
		GitCommit:   gitCommit,
		BuiltTime:   time.Unix(buildTime, 0).Format(time.ANSIC),
		Built:       buildTime,
		BuildOrigin: buildOrigin,
		OsArch:      runtime.GOOS + "/" + runtime.GOARCH,
		Os:          runtime.GOOS,
	}, nil
}
