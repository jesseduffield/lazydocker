package transports

import (
	"fmt"
	"sort"
	"sync"

	"go.podman.io/image/v5/internal/set"
	"go.podman.io/image/v5/types"
)

// knownTransports is a registry of known ImageTransport instances.
type knownTransports struct {
	transports map[string]types.ImageTransport
	mu         sync.Mutex
}

func (kt *knownTransports) Get(k string) types.ImageTransport {
	kt.mu.Lock()
	t := kt.transports[k]
	kt.mu.Unlock()
	return t
}

func (kt *knownTransports) Remove(k string) {
	kt.mu.Lock()
	delete(kt.transports, k)
	kt.mu.Unlock()
}

func (kt *knownTransports) Add(t types.ImageTransport) {
	kt.mu.Lock()
	defer kt.mu.Unlock()
	name := t.Name()
	if t := kt.transports[name]; t != nil {
		panic(fmt.Sprintf("Duplicate image transport name %s", name))
	}
	kt.transports[name] = t
}

var kt *knownTransports

func init() {
	kt = &knownTransports{
		transports: make(map[string]types.ImageTransport),
	}
}

// Get returns the transport specified by name or nil when unavailable.
func Get(name string) types.ImageTransport {
	return kt.Get(name)
}

// Delete deletes a transport from the registered transports.
func Delete(name string) {
	kt.Remove(name)
}

// Register registers a transport.
func Register(t types.ImageTransport) {
	kt.Add(t)
}

// ImageName converts a types.ImageReference into an URL-like image name, which MUST be such that
// ParseImageName(ImageName(reference)) returns an equivalent reference.
//
// This is the generally recommended way to refer to images in the UI.
//
// NOTE: The returned string is not promised to be equal to the original input to ParseImageName;
// e.g. default attribute values omitted by the user may be filled in the return value, or vice versa.
func ImageName(ref types.ImageReference) string {
	return ref.Transport().Name() + ":" + ref.StringWithinTransport()
}

var deprecatedTransports = set.NewWithValues("atomic", "ostree")

// ListNames returns a list of non deprecated transport names.
// Deprecated transports can be used, but are not presented to users.
func ListNames() []string {
	kt.mu.Lock()
	defer kt.mu.Unlock()
	var names []string
	for _, transport := range kt.transports {
		if !deprecatedTransports.Contains(transport.Name()) {
			names = append(names, transport.Name())
		}
	}
	sort.Strings(names)
	return names
}
