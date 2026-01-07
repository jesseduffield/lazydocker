//go:build !remote

package libimage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	filtersPkg "go.podman.io/common/pkg/filters"
	"go.podman.io/common/pkg/timetype"
	"go.podman.io/image/v5/docker/reference"
)

// filterFunc is a prototype for a positive image filter.  Returning `true`
// indicates that the image matches the criteria.
type filterFunc func(*Image, *layerTree) (bool, error)

type compiledFilters map[string][]filterFunc

// Apply the specified filters.  All filters of each key must apply.
// tree must be provided if compileImageFilters indicated it is necessary.
// WARNING: Application of filterReferences sets the image names to matched names, but this only affects the values in memory, they are not written to storage.
func (i *Image) applyFilters(ctx context.Context, filters compiledFilters, tree *layerTree) (bool, error) {
	for key := range filters {
		for _, filter := range filters[key] {
			matches, err := filter(i, tree)
			if err != nil {
				// Some images may have been corrupted in the
				// meantime, so do an extra check and make the
				// error non-fatal (see containers/podman/issues/12582).
				if errCorrupted := i.isCorrupted(ctx, ""); errCorrupted != nil {
					logrus.Error(errCorrupted.Error())
					return false, nil
				}
				return false, err
			}
			// If any filter within a group doesn't match, return false
			if !matches {
				return false, nil
			}
		}
	}
	return true, nil
}

// filterImages returns a slice of images which are passing all specified
// filters.
// tree must be provided if compileImageFilters indicated it is necessary.
// WARNING: Application of filterReferences sets the image names to matched names, but this only affects the values in memory, they are not written to storage.
func (r *Runtime) filterImages(ctx context.Context, images []*Image, filters compiledFilters, tree *layerTree) ([]*Image, error) {
	result := []*Image{}
	for i := range images {
		match, err := images[i].applyFilters(ctx, filters, tree)
		if err != nil {
			return nil, err
		}
		if match {
			result = append(result, images[i])
		}
	}
	return result, nil
}

// compileImageFilters creates `filterFunc`s for the specified filters.  The
// required format is `key=value` with the following supported keys:
//
//	after, since, before, containers, dangling, id, label, readonly, reference, intermediate
//
// compileImageFilters returns: compiled filters, if LayerTree is needed, error.
func (r *Runtime) compileImageFilters(ctx context.Context, options *ListImagesOptions) (compiledFilters, bool, error) {
	logrus.Tracef("Parsing image filters %s", options.Filters)
	if len(options.Filters) == 0 {
		return nil, false, nil
	}

	filterInvalidValue := `invalid image filter %q: must be in the format "filter=value or filter!=value"`

	var wantedReferenceMatches, unwantedReferenceMatches []string
	filters := compiledFilters{}
	needsLayerTree := false
	duplicate := map[string]string{}
	for _, f := range options.Filters {
		var key, value string
		var filter filterFunc
		negate := false
		key, value, ok := strings.Cut(f, "!=")
		if ok {
			negate = true
		} else {
			key, value, ok = strings.Cut(f, "=")
			if !ok {
				return nil, false, fmt.Errorf(filterInvalidValue, f)
			}
		}

		switch key {
		case "after", "since":
			img, err := r.time(key, value)
			if err != nil {
				return nil, false, err
			}
			key = "since"
			filter = filterAfter(img.Created())

		case "before":
			img, err := r.time(key, value)
			if err != nil {
				return nil, false, err
			}
			filter = filterBefore(img.Created())

		case "containers":
			if err := r.containers(duplicate, key, value, options.IsExternalContainerFunc); err != nil {
				return nil, false, err
			}
			filter = filterContainers(value, options.IsExternalContainerFunc)

		case "dangling":
			dangling, err := r.bool(duplicate, key, value)
			if err != nil {
				return nil, false, err
			}
			needsLayerTree = true
			filter = filterDangling(ctx, dangling)

		case "id":
			filter = filterID(value)

		case "digest":
			f, err := filterDigest(value)
			if err != nil {
				return nil, false, err
			}
			filter = f

		case "intermediate":
			intermediate, err := r.bool(duplicate, key, value)
			if err != nil {
				return nil, false, err
			}
			needsLayerTree = true
			filter = filterIntermediate(ctx, intermediate)

		case "label":
			filter = filterLabel(ctx, value)
		case "readonly":
			readOnly, err := r.bool(duplicate, key, value)
			if err != nil {
				return nil, false, err
			}
			filter = filterReadOnly(readOnly)

		case "manifest":
			manifest, err := r.bool(duplicate, key, value)
			if err != nil {
				return nil, false, err
			}
			filter = filterManifest(ctx, manifest)

		case "reference":
			if negate {
				unwantedReferenceMatches = append(unwantedReferenceMatches, value)
			} else {
				wantedReferenceMatches = append(wantedReferenceMatches, value)
			}
			continue

		case "until":
			until, err := r.until(value)
			if err != nil {
				return nil, false, err
			}
			filter = filterBefore(until)

		default:
			return nil, false, fmt.Errorf(filterInvalidValue, key)
		}
		if negate {
			filter = negateFilter(filter)
		}
		filters[key] = append(filters[key], filter)
	}

	// reference filters is a special case as it does an OR for positive matches
	// and an AND logic for negative matches
	filter := filterReferences(r, wantedReferenceMatches, unwantedReferenceMatches)
	filters["reference"] = append(filters["reference"], filter)

	return filters, needsLayerTree, nil
}

func negateFilter(f filterFunc) filterFunc {
	return func(img *Image, tree *layerTree) (bool, error) {
		b, err := f(img, tree)
		return !b, err
	}
}

func (r *Runtime) containers(duplicate map[string]string, key, value string, externalFunc IsExternalContainerFunc) error {
	if exists, ok := duplicate[key]; ok && exists != value {
		return fmt.Errorf("specifying %q filter more than once with different values is not supported", key)
	}
	duplicate[key] = value
	switch value {
	case "false", "true":
	case "external":
		if externalFunc == nil {
			return errors.New("libimage error: external containers filter without callback")
		}
	default:
		return fmt.Errorf("unsupported value %q for containers filter", value)
	}
	return nil
}

func (r *Runtime) until(value string) (time.Time, error) {
	var until time.Time
	ts, err := timetype.GetTimestamp(value, time.Now())
	if err != nil {
		return until, err
	}
	seconds, nanoseconds, err := timetype.ParseTimestamps(ts, 0)
	if err != nil {
		return until, err
	}
	return time.Unix(seconds, nanoseconds), nil
}

func (r *Runtime) time(key, value string) (*Image, error) {
	img, _, err := r.LookupImage(value, nil)
	if err != nil {
		return nil, fmt.Errorf("could not find local image for filter %q=%q: %w", key, value, err)
	}
	return img, nil
}

func (r *Runtime) bool(duplicate map[string]string, key, value string) (bool, error) {
	if exists, ok := duplicate[key]; ok && exists != value {
		return false, fmt.Errorf("specifying %q filter more than once with different values is not supported", key)
	}
	duplicate[key] = value
	set, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("non-boolean value %q for %s filter: %w", key, value, err)
	}
	return set, nil
}

// filterManifest filters whether or not the image is a manifest list.
func filterManifest(ctx context.Context, value bool) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		isManifestList, err := img.IsManifestList(ctx)
		if err != nil {
			return false, err
		}
		return isManifestList == value, nil
	}
}

// filterReferences creates a reference filter for matching the specified wantedReferenceMatches value (OR logic)
// and for matching the unwantedReferenceMatches values (AND logic).
func filterReferences(r *Runtime, wantedReferenceMatches, unwantedReferenceMatches []string) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		// Empty reference filters, return true
		if len(wantedReferenceMatches) == 0 && len(unwantedReferenceMatches) == 0 {
			return true, nil
		}

		// Go through the unwanted matches first
		for _, value := range unwantedReferenceMatches {
			matches, err := imageMatchesReferenceFilter(r, img, value)
			if err != nil {
				return false, err
			}
			if matches {
				return false, nil
			}
		}

		// If there are no wanted match filters, then return false for the image
		// that matched the unwanted value otherwise return true
		if len(wantedReferenceMatches) == 0 {
			return true, nil
		}

		matchedReference := ""
		for _, value := range wantedReferenceMatches {
			matches, err := imageMatchesReferenceFilter(r, img, value)
			if err != nil {
				return false, err
			}
			if matches {
				matchedReference = value
				break
			}
		}

		if matchedReference == "" {
			return false, nil
		}

		// If there is exactly one wanted reference match and no unwanted matches,
		// the filter is treated as a query, so it sets the matching names to
		// the image in memory.
		if len(wantedReferenceMatches) == 1 && len(unwantedReferenceMatches) == 0 {
			ref, ok := isFullyQualifiedReference(matchedReference)
			if !ok {
				return true, nil
			}
			namesThatMatch := []string{}
			for _, name := range img.Names() {
				if nameMatchesReference(name, ref) {
					namesThatMatch = append(namesThatMatch, name)
				}
			}
			img.setEphemeralNames(namesThatMatch)
		}
		return true, nil
	}
}

// isFullyQualifiedReference checks if the provided string is a fully qualified
// reference (i.e., it contains a domain, path, and tag or digest).
// It returns a reference.Named and a boolean indicating whether the
// reference is fully qualified. If the reference is not fully qualified,
// it returns nil and false.
func isFullyQualifiedReference(r string) (reference.Named, bool) {
	ref, err := reference.ParseNamed(r)
	// If there is an error parsing the reference, it is not a valid reference
	if err != nil {
		return nil, false
	}
	// If it's name-only (no tag/digest), it's not fully qualified
	if reference.IsNameOnly(ref) {
		return nil, false
	}
	return ref, true
}

func nameMatchesReference(name string, ref reference.Named) bool {
	_, containsDigest := ref.(reference.Digested)
	if containsDigest {
		nameRef, err := reference.ParseNamed(name)
		if err != nil {
			return false
		}
		return nameRef.Name() == ref.Name()
	}
	return name == ref.String()
}

// imageMatchesReferenceFilter returns true if an image matches the filter value given.
func imageMatchesReferenceFilter(r *Runtime, img *Image, value string) (bool, error) {
	lookedUp, _, _ := r.LookupImage(value, nil)
	if lookedUp != nil {
		if lookedUp.ID() == img.ID() {
			return true, nil
		}
	}

	refs, err := img.NamesReferences()
	if err != nil {
		return false, err
	}

	for _, ref := range refs {
		refString := ref.String() // FQN with tag/digest
		candidates := []string{refString}

		// Split the reference into 3 components (twice if digested/tagged):
		// 1) Fully-qualified reference
		// 2) Without domain
		// 3) Without domain and path
		if named, isNamed := ref.(reference.Named); isNamed {
			candidates = append(candidates,
				reference.Path(named),                           // path/name without tag/digest (Path() removes it)
				refString[strings.LastIndex(refString, "/")+1:]) // name with tag/digest

			trimmedString := reference.TrimNamed(named).String()
			if refString != trimmedString {
				tagOrDigest := refString[len(trimmedString):]
				candidates = append(candidates,
					trimmedString,                     // FQN without tag/digest
					reference.Path(named)+tagOrDigest, // path/name with tag/digest
					trimmedString[strings.LastIndex(trimmedString, "/")+1:]) // name without tag/digest
			}
		}

		for _, candidate := range candidates {
			// path.Match() is also used by Docker's reference.FamiliarMatch().
			matched, _ := path.Match(value, candidate)
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

// filterLabel creates a label for matching the specified value.
func filterLabel(ctx context.Context, value string) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		labels, err := img.Labels(ctx)
		if err != nil {
			return false, err
		}
		return filtersPkg.MatchLabelFilters([]string{value}, labels), nil
	}
}

// filterAfter creates an after filter for matching the specified value.
func filterAfter(value time.Time) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		return img.Created().After(value), nil
	}
}

// filterBefore creates a before filter for matching the specified value.
func filterBefore(value time.Time) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		return img.Created().Before(value), nil
	}
}

// filterReadOnly creates a readonly filter for matching the specified value.
func filterReadOnly(value bool) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		return img.IsReadOnly() == value, nil
	}
}

// filterContainers creates a container filter for matching the specified value.
func filterContainers(value string, fn IsExternalContainerFunc) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		ctrs, err := img.Containers()
		if err != nil {
			return false, err
		}
		if value != "external" {
			boolValue := value == "true"
			return (len(ctrs) > 0) == boolValue, nil
		}

		// Check whether all associated containers are external ones.
		for _, c := range ctrs {
			isExternal, err := fn(c)
			if err != nil {
				return false, fmt.Errorf("checking if %s is an external container in filter: %w", c, err)
			}
			if !isExternal {
				return isExternal, nil
			}
		}
		return true, nil
	}
}

// filterDangling creates a dangling filter for matching the specified value.
func filterDangling(ctx context.Context, value bool) filterFunc {
	return func(img *Image, tree *layerTree) (bool, error) {
		isDangling, err := img.isDangling(ctx, tree)
		if err != nil {
			return false, err
		}
		return isDangling == value, nil
	}
}

// filterID creates an image-ID filter for matching the specified value.
func filterID(value string) filterFunc {
	return func(img *Image, _ *layerTree) (bool, error) {
		return strings.HasPrefix(img.ID(), value), nil
	}
}

// filterDigest creates a digest filter for matching the specified value.
func filterDigest(value string) (filterFunc, error) {
	if !strings.HasPrefix(value, "sha256:") {
		return nil, fmt.Errorf("invalid value %q for digest filter", value)
	}
	return func(img *Image, _ *layerTree) (bool, error) {
		return img.containsDigestPrefix(value), nil
	}, nil
}

// filterIntermediate creates an intermediate filter for images.  An image is
// considered to be an intermediate image if it is dangling (i.e., no tags) and
// has no children (i.e., no other image depends on it).
func filterIntermediate(ctx context.Context, value bool) filterFunc {
	return func(img *Image, tree *layerTree) (bool, error) {
		isIntermediate, err := img.isIntermediate(ctx, tree)
		if err != nil {
			return false, err
		}
		return isIntermediate == value, nil
	}
}
