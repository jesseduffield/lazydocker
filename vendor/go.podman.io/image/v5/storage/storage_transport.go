//go:build !containers_image_storage_stub

package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/idtools"
)

const (
	minimumTruncatedIDLength = 3
)

func init() {
	transports.Register(Transport)
}

var (
	// Transport is an ImageTransport that uses either a default
	// storage.Store or one that's it's explicitly told to use.
	Transport StoreTransport = &storageTransport{}
	// ErrInvalidReference is returned when ParseReference() is passed an
	// empty reference.
	ErrInvalidReference = errors.New("invalid reference")
	// ErrPathNotAbsolute is returned when a graph root is not an absolute
	// path name.
	ErrPathNotAbsolute = errors.New("path name is not absolute")
)

// StoreTransport is an ImageTransport that uses a storage.Store to parse
// references, either its own default or one that it's told to use.
type StoreTransport interface {
	types.ImageTransport
	// SetStore sets the default store for this transport.
	SetStore(storage.Store)
	// GetStoreIfSet returns the default store for this transport, or nil if not set/determined yet.
	GetStoreIfSet() storage.Store
	// GetImage retrieves the image from the transport's store that's named
	// by the reference.
	// Deprecated: Surprisingly, with a StoreTransport reference which contains an ID,
	// this ignores that ID; and repeated calls of GetStoreImage with the same named reference
	// can return different images, with no way for the caller to "freeze" the storage.Image identity
	// without discarding the name entirely.
	//
	// Use storage.ResolveReference instead; note that if the image is not found, ResolveReference returns
	// c/image/v5/storage.ErrNoSuchImage, not c/storage.ErrImageUnknown.
	GetImage(types.ImageReference) (*storage.Image, error)
	// GetStoreImage retrieves the image from a specified store that's named
	// by the reference.
	//
	// Deprecated: Surprisingly, with a StoreTransport reference which contains an ID,
	// this ignores that ID; and repeated calls of GetStoreImage with the same named reference
	// can return different images, with no way for the caller to "freeze" the storage.Image identity
	// without discarding the name entirely.
	//
	// Also, a StoreTransport reference already contains a store, so providing another one is redundant.
	//
	// Use storage.ResolveReference instead; note that if the image is not found, ResolveReference returns
	// c/image/v5/storage.ErrNoSuchImage, not c/storage.ErrImageUnknown.
	GetStoreImage(storage.Store, types.ImageReference) (*storage.Image, error)
	// ParseStoreReference parses a reference, overriding any store
	// specification that it may contain.
	ParseStoreReference(store storage.Store, reference string) (*storageReference, error)
	// NewStoreReference creates a reference for (named@ID) in store.
	// either of name or ID can be unset; named must not be a reference.IsNameOnly.
	NewStoreReference(store storage.Store, named reference.Named, id string) (*storageReference, error)
	// SetDefaultUIDMap sets the default UID map to use when opening stores.
	SetDefaultUIDMap(idmap []idtools.IDMap)
	// SetDefaultGIDMap sets the default GID map to use when opening stores.
	SetDefaultGIDMap(idmap []idtools.IDMap)
	// DefaultUIDMap returns the default UID map used when opening stores.
	DefaultUIDMap() []idtools.IDMap
	// DefaultGIDMap returns the default GID map used when opening stores.
	DefaultGIDMap() []idtools.IDMap
}

type storageTransport struct {
	store         storage.Store
	defaultUIDMap []idtools.IDMap
	defaultGIDMap []idtools.IDMap
}

func (s *storageTransport) Name() string {
	// Still haven't really settled on a name.
	return "containers-storage"
}

// SetStore sets the Store object which the Transport will use for parsing
// references when information about a Store is not directly specified as part
// of the reference.  If one is not set, the library will attempt to initialize
// one with default settings when a reference needs to be parsed.  Calling
// SetStore does not affect previously parsed references.
func (s *storageTransport) SetStore(store storage.Store) {
	s.store = store
}

// GetStoreIfSet returns the default store for this transport, as set using SetStore() or initialized by default, or nil if not set/determined yet.
func (s *storageTransport) GetStoreIfSet() storage.Store {
	return s.store
}

// SetDefaultUIDMap sets the default UID map to use when opening stores.
func (s *storageTransport) SetDefaultUIDMap(idmap []idtools.IDMap) {
	s.defaultUIDMap = idmap
}

// SetDefaultGIDMap sets the default GID map to use when opening stores.
func (s *storageTransport) SetDefaultGIDMap(idmap []idtools.IDMap) {
	s.defaultGIDMap = idmap
}

// DefaultUIDMap returns the default UID map used when opening stores.
func (s *storageTransport) DefaultUIDMap() []idtools.IDMap {
	return s.defaultUIDMap
}

// DefaultGIDMap returns the default GID map used when opening stores.
func (s *storageTransport) DefaultGIDMap() []idtools.IDMap {
	return s.defaultGIDMap
}

// ParseStoreReference takes a name or an ID, tries to figure out which it is
// relative to the given store, and returns it in a reference object.
func (s storageTransport) ParseStoreReference(store storage.Store, ref string) (*storageReference, error) {
	if ref == "" {
		return nil, fmt.Errorf("%q is an empty reference: %w", ref, ErrInvalidReference)
	}
	if ref[0] == '[' {
		// Ignore the store specifier.
		closeIndex := strings.IndexRune(ref, ']')
		if closeIndex < 1 {
			return nil, fmt.Errorf("store specifier in %q did not end: %w", ref, ErrInvalidReference)
		}
		ref = ref[closeIndex+1:]
	}

	// The reference may end with an image ID.  Image IDs and digests use the same "@" separator;
	// here we only peel away an image ID, and leave digests alone.
	split := strings.LastIndex(ref, "@")
	id := ""
	if split != -1 {
		possibleID := ref[split+1:]
		if possibleID == "" {
			return nil, fmt.Errorf("empty trailing digest or ID in %q: %w", ref, ErrInvalidReference)
		}
		// If it looks like a digest, leave it alone for now.
		if _, err := digest.Parse(possibleID); err != nil {
			// Otherwise…
			if err := validateImageID(possibleID); err == nil {
				id = possibleID // … it is a full ID
			} else if img, err := store.Image(possibleID); err == nil && img != nil && len(possibleID) >= minimumTruncatedIDLength && strings.HasPrefix(img.ID, possibleID) {
				// … it is a truncated version of the ID of an image that's present in local storage,
				// so we might as well use the expanded value.
				id = img.ID
			} else {
				return nil, fmt.Errorf("%q does not look like an image ID or digest: %w", possibleID, ErrInvalidReference)
			}
			// We have recognized an image ID; peel it off.
			ref = ref[:split]
		}
	}

	// If we only have one @-delimited portion, then _maybe_ it's a truncated image ID.  Only check on that if it's
	// at least of what we guess is a reasonable minimum length, because we don't want a really short value
	// like "a" matching an image by ID prefix when the input was actually meant to specify an image name.
	if id == "" && len(ref) >= minimumTruncatedIDLength && !strings.ContainsAny(ref, "@:") {
		if img, err := store.Image(ref); err == nil && img != nil && strings.HasPrefix(img.ID, ref) {
			// It's a truncated version of the ID of an image that's present in local storage;
			// we need to expand it.
			id = img.ID
			ref = ""
		}
	}

	var named reference.Named
	// Unless we have an un-named "ID" or "@ID" reference (where ID might only have been a prefix), which has been
	// completely parsed above, the initial portion should be a name, possibly with a tag and/or a digest..
	if ref != "" {
		var err error
		named, err = reference.ParseNormalizedNamed(ref)
		if err != nil {
			return nil, fmt.Errorf("parsing named reference %q: %w", ref, err)
		}
		named = reference.TagNameOnly(named)
	}

	result, err := s.NewStoreReference(store, named, id)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("parsed reference into %q", result.StringWithinTransport())
	return result, nil
}

// NewStoreReference creates a reference for (named@ID) in store.
// either of name or ID can be unset; named must not be a reference.IsNameOnly.
func (s *storageTransport) NewStoreReference(store storage.Store, named reference.Named, id string) (*storageReference, error) {
	return newReference(storageTransport{store: store, defaultUIDMap: s.defaultUIDMap, defaultGIDMap: s.defaultGIDMap}, named, id)
}

func (s *storageTransport) GetStore() (storage.Store, error) {
	// Return the transport's previously-set store.  If we don't have one
	// of those, initialize one now.
	if s.store == nil {
		options, err := storage.DefaultStoreOptions()
		if err != nil {
			return nil, err
		}
		options.UIDMap = s.defaultUIDMap
		options.GIDMap = s.defaultGIDMap
		store, err := storage.GetStore(options)
		if err != nil {
			return nil, err
		}
		s.store = store
	}
	return s.store, nil
}

// ParseReference takes a name and a tag or digest and/or ID
// ("_name_"/"@_id_"/"_name_:_tag_"/"_name_:_tag_@_id_"/"_name_@_digest_"/"_name_@_digest_@_id_"/"_name_:_tag_@_digest_"/"_name_:_tag_@_digest_@_id_"),
// possibly prefixed with a store specifier in the form "[_graphroot_]" or
// "[_driver_@_graphroot_]" or "[_driver_@_graphroot_+_runroot_]" or
// "[_driver_@_graphroot_:_options_]" or "[_driver_@_graphroot_+_runroot_:_options_]",
// tries to figure out which it is, and returns it in a reference object.
// If _id_ is the ID of an image that's present in local storage, it can be truncated, and
// even be specified as if it were a _name_, value.
func (s *storageTransport) ParseReference(reference string) (types.ImageReference, error) {
	var store storage.Store
	// Check if there's a store location prefix.  If there is, then it
	// needs to match a store that was previously initialized using
	// storage.GetStore(), or be enough to let the storage library fill out
	// the rest using knowledge that it has from elsewhere.
	if len(reference) > 0 && reference[0] == '[' {
		closeIndex := strings.IndexRune(reference, ']')
		if closeIndex < 1 {
			return nil, ErrInvalidReference
		}
		storeSpec := reference[1:closeIndex]
		reference = reference[closeIndex+1:]
		// Peel off a "driver@" from the start.
		driverInfo := ""
		driverPart1, driverPart2, gotDriver := strings.Cut(storeSpec, "@")
		if !gotDriver {
			storeSpec = driverPart1
			if storeSpec == "" {
				return nil, ErrInvalidReference
			}
		} else {
			driverInfo = driverPart1
			if driverInfo == "" {
				return nil, ErrInvalidReference
			}
			storeSpec = driverPart2
			if storeSpec == "" {
				return nil, ErrInvalidReference
			}
		}
		// Peel off a ":options" from the end.
		var options []string
		storeSpec, optionsPart, gotOptions := strings.Cut(storeSpec, ":")
		if gotOptions {
			options = strings.Split(optionsPart, ",")
		}
		// Peel off a "+runroot" from the new end.
		storeSpec, runRootInfo, _ := strings.Cut(storeSpec, "+") // runRootInfo is "" if there is no "+"
		// The rest is our graph root.
		rootInfo := storeSpec
		// Check that any paths are absolute paths.
		if rootInfo != "" && !filepath.IsAbs(rootInfo) {
			return nil, ErrPathNotAbsolute
		}
		if runRootInfo != "" && !filepath.IsAbs(runRootInfo) {
			return nil, ErrPathNotAbsolute
		}
		store2, err := storage.GetStore(storage.StoreOptions{
			GraphDriverName:    driverInfo,
			GraphRoot:          rootInfo,
			RunRoot:            runRootInfo,
			GraphDriverOptions: options,
			UIDMap:             s.defaultUIDMap,
			GIDMap:             s.defaultGIDMap,
		})
		if err != nil {
			return nil, err
		}
		store = store2
	} else {
		// We didn't have a store spec, so use the default.
		store2, err := s.GetStore()
		if err != nil {
			return nil, err
		}
		store = store2
	}
	return s.ParseStoreReference(store, reference)
}

// Deprecated: Surprisingly, with a StoreTransport reference which contains an ID,
// this ignores that ID; and repeated calls of GetStoreImage with the same named reference
// can return different images, with no way for the caller to "freeze" the storage.Image identity
// without discarding the name entirely.
//
// Also, a StoreTransport reference already contains a store, so providing another one is redundant.
//
// Use storage.ResolveReference instead; note that if the image is not found, ResolveReference returns
// c/image/v5/storage.ErrNoSuchImage, not c/storage.ErrImageUnknown.
func (s storageTransport) GetStoreImage(store storage.Store, ref types.ImageReference) (*storage.Image, error) {
	dref := ref.DockerReference()
	if dref != nil {
		if img, err := store.Image(dref.String()); err == nil {
			return img, nil
		}
	}
	if sref, ok := ref.(*storageReference); ok {
		tmpRef := *sref
		if img, err := tmpRef.resolveImage(nil); err == nil {
			return img, nil
		}
	}
	return nil, storage.ErrImageUnknown
}

// Deprecated: Surprisingly, with a StoreTransport reference which contains an ID,
// this ignores that ID; and repeated calls of GetStoreImage with the same named reference
// can return different images, with no way for the caller to "freeze" the storage.Image identity
// without discarding the name entirely.
//
// Use storage.ResolveReference instead; note that if the image is not found, ResolveReference returns
// c/image/v5/storage.ErrNoSuchImage, not c/storage.ErrImageUnknown.
func (s *storageTransport) GetImage(ref types.ImageReference) (*storage.Image, error) {
	store, err := s.GetStore()
	if err != nil {
		return nil, err
	}
	return s.GetStoreImage(store, ref)
}

func (s storageTransport) ValidatePolicyConfigurationScope(scope string) error {
	// Check that there's a store location prefix.  Values we're passed are
	// expected to come from PolicyConfigurationIdentity or
	// PolicyConfigurationNamespaces, so if there's no store location,
	// something's wrong.
	if scope[0] != '[' {
		return ErrInvalidReference
	}
	// Parse the store location prefix.
	closeIndex := strings.IndexRune(scope, ']')
	if closeIndex < 1 {
		return ErrInvalidReference
	}
	storeSpec := scope[1:closeIndex]
	scope = scope[closeIndex+1:]
	if a, b, ok := strings.Cut(storeSpec, "@"); ok && a != "" && b != "" {
		// Two components: the driver type and the graph root.
		if !filepath.IsAbs(b) {
			return ErrPathNotAbsolute
		}
	} else if !ok && a != "" {
		// One component: the graph root.
		if !filepath.IsAbs(storeSpec) {
			return ErrPathNotAbsolute
		}
	} else {
		// Anything else: scope specified in a form we don't
		// recognize.
		return ErrInvalidReference
	}
	// That might be all of it, and that's okay.
	if scope == "" {
		return nil
	}

	fields := strings.SplitN(scope, "@", 3)
	switch len(fields) {
	case 1: // name only
	case 2: // name:tag@ID or name[:tag]@digest
		if idErr := validateImageID(fields[1]); idErr != nil {
			if _, digestErr := digest.Parse(fields[1]); digestErr != nil {
				return fmt.Errorf("%v is neither a valid digest(%s) nor a valid ID(%s)", fields[1], digestErr.Error(), idErr.Error())
			}
		}
	case 3: // name[:tag]@digest@ID
		if _, err := digest.Parse(fields[1]); err != nil {
			return err
		}
		if err := validateImageID(fields[2]); err != nil {
			return err
		}
	default: // Coverage: This should never happen
		return errors.New("Internal error: unexpected number of fields form strings.SplitN")
	}
	// As for field[0], if it is non-empty at all:
	// FIXME? We could be verifying the various character set and length restrictions
	// from docker/distribution/reference.regexp.go, but other than that there
	// are few semantically invalid strings.
	return nil
}

// validateImageID returns nil if id is a valid (full) image ID, or an error
func validateImageID(id string) error {
	_, err := digest.Parse("sha256:" + id)
	return err
}
