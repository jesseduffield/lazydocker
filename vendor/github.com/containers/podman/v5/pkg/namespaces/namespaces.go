package namespaces

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	bridgeType    = "bridge"
	containerType = "container"
	defaultType   = "default"
	hostType      = "host"
	noneType      = "none"
	nsType        = "ns"
	podType       = "pod"
	privateType   = "private"
	shareableType = "shareable"
	slirpType     = "slirp4netns"
	pastaType     = "pasta"
)

// KeepIDUserNsOptions defines how to create a user namespace using keep-id.
type KeepIDUserNsOptions struct {
	// UID is the target uid in the user namespace.
	UID *uint32
	// GID is the target uid in the user namespace.
	GID *uint32
	// MaxSize is the maximum size of the user namespace.
	MaxSize *uint32
}

// UsernsMode represents userns mode in the container.
type UsernsMode string

// IsHost indicates whether the container uses the host's userns.
func (n UsernsMode) IsHost() bool {
	return n == hostType
}

// IsKeepID indicates whether container uses a mapping where the (uid, gid) on the host is kept inside of the namespace.
func (n UsernsMode) IsKeepID() bool {
	parts := strings.Split(string(n), ":")
	return parts[0] == "keep-id"
}

// IsNoMap indicates whether container uses a mapping where the (uid, gid) on the host is not present in the namespace.
func (n UsernsMode) IsNoMap() bool {
	return n == "nomap"
}

// IsAuto indicates whether container uses the "auto" userns mode.
func (n UsernsMode) IsAuto() bool {
	parts := strings.Split(string(n), ":")
	return parts[0] == "auto"
}

// IsDefaultValue indicates whether the user namespace has the default value.
func (n UsernsMode) IsDefaultValue() bool {
	return n == "" || n == defaultType
}

// GetKeepIDOptions returns a KeepIDUserNsOptions with the settings to keepIDmatically set up
// a user namespace.
func (n UsernsMode) GetKeepIDOptions() (*KeepIDUserNsOptions, error) {
	nsmode, nsopts, hasOpts := strings.Cut(string(n), ":")
	if nsmode != "keep-id" {
		return nil, fmt.Errorf("wrong user namespace mode")
	}
	options := KeepIDUserNsOptions{}
	if !hasOpts {
		return &options, nil
	}
	for o := range strings.SplitSeq(nsopts, ",") {
		opt, val, hasVal := strings.Cut(o, "=")
		if !hasVal {
			return nil, fmt.Errorf("invalid option specified: %q", o)
		}
		switch opt {
		case "uid":
			s, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				return nil, err
			}
			v := uint32(s)
			options.UID = &v
		case "gid":
			s, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				return nil, err
			}
			v := uint32(s)
			options.GID = &v
		case "size":
			s, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				return nil, err
			}
			v := uint32(s)
			options.MaxSize = &v
		default:
			return nil, fmt.Errorf("unknown option specified: %q", opt)
		}
	}
	return &options, nil
}

// IsPrivate indicates whether the container uses the a private userns.
func (n UsernsMode) IsPrivate() bool {
	return !n.IsHost() && !n.IsContainer()
}

// Valid indicates whether the userns is valid.
func (n UsernsMode) Valid() bool {
	parts := strings.Split(string(n), ":")
	switch mode := parts[0]; mode {
	case "", privateType, hostType, "keep-id", nsType, "auto", "nomap":
	case containerType:
		if len(parts) != 2 || parts[1] == "" {
			return false
		}
	default:
		return false
	}
	return true
}

// IsNS indicates a userns namespace passed in by path (ns:<path>)
func (n UsernsMode) IsNS() bool {
	return strings.HasPrefix(string(n), "ns:")
}

// NS gets the path associated with a ns:<path> userns ns
func (n UsernsMode) NS() string {
	_, path, _ := strings.Cut(string(n), ":")
	return path
}

// IsContainer indicates whether container uses a container userns.
func (n UsernsMode) IsContainer() bool {
	typ, _, hasName := strings.Cut(string(n), ":")
	return hasName && typ == containerType
}

// Container is the id of the container which network this container is connected to.
func (n UsernsMode) Container() string {
	typ, name, hasName := strings.Cut(string(n), ":")
	if hasName && typ == containerType {
		return name
	}
	return ""
}

// NetworkMode represents the container network stack.
type NetworkMode string

// IsNone indicates whether container isn't using a network stack.
func (n NetworkMode) IsNone() bool {
	return n == noneType
}

// IsHost indicates whether the container uses the host's network stack.
func (n NetworkMode) IsHost() bool {
	return n == hostType
}

// IsDefault indicates whether container uses the default network stack.
func (n NetworkMode) IsDefault() bool {
	return n == defaultType
}

// IsPrivate indicates whether container uses its private network stack.
func (n NetworkMode) IsPrivate() bool {
	return !n.IsHost() && !n.IsContainer()
}

// IsContainer indicates whether container uses a container network stack.
func (n NetworkMode) IsContainer() bool {
	typ, _, hasName := strings.Cut(string(n), ":")
	return hasName && typ == containerType
}

// Container is the id of the container which network this container is connected to.
func (n NetworkMode) Container() string {
	typ, name, hasName := strings.Cut(string(n), ":")
	if hasName && typ == containerType {
		return name
	}
	return ""
}

// UserDefined indicates user-created network
func (n NetworkMode) UserDefined() string {
	if n.IsUserDefined() {
		return string(n)
	}
	return ""
}

// IsBridge indicates whether container uses the bridge network stack
func (n NetworkMode) IsBridge() bool {
	return n == bridgeType
}

// IsSlirp4netns indicates if we are running a rootless network stack
func (n NetworkMode) IsSlirp4netns() bool {
	return n == slirpType || strings.HasPrefix(string(n), slirpType+":")
}

// IsPasta indicates if we are running a rootless network stack using pasta
func (n NetworkMode) IsPasta() bool {
	return n == pastaType || strings.HasPrefix(string(n), pastaType+":")
}

// IsNS indicates a network namespace passed in by path (ns:<path>)
func (n NetworkMode) IsNS() bool {
	return strings.HasPrefix(string(n), nsType)
}

// NS gets the path associated with a ns:<path> network ns
func (n NetworkMode) NS() string {
	_, path, _ := strings.Cut(string(n), ":")
	return path
}

// IsPod returns whether the network refers to pod networking
func (n NetworkMode) IsPod() bool {
	return n == podType
}

// IsUserDefined indicates user-created network
func (n NetworkMode) IsUserDefined() bool {
	return !n.IsDefault() && !n.IsBridge() && !n.IsHost() && !n.IsNone() && !n.IsContainer() && !n.IsSlirp4netns() && !n.IsPasta() && !n.IsNS()
}
