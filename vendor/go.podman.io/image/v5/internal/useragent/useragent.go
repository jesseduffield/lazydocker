package useragent

import "go.podman.io/image/v5/version"

// DefaultUserAgent is a value that should be used by User-Agent headers, unless the user specifically instructs us otherwise.
var DefaultUserAgent = "containers/" + version.Version + " (github.com/containers/image)"
