//go:build !remote

package libimage

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
)

// NormalizeName normalizes the provided name according to the conventions by
// Podman and Buildah.  If tag and digest are missing, the "latest" tag will be
// used.  If it's a short name, it will be prefixed with "localhost/".
//
// References to docker.io are normalized according to the Docker conventions.
// For instance, "docker.io/foo" turns into "docker.io/library/foo".
func NormalizeName(name string) (reference.Named, error) {
	// NOTE: this code is in symmetrie with containers/image/pkg/shortnames.
	ref, err := reference.Parse(name)
	if err != nil {
		return nil, fmt.Errorf("normalizing name %q: %w", name, err)
	}

	named, ok := ref.(reference.Named)
	if !ok {
		return nil, fmt.Errorf("%q is not a named reference", name)
	}

	// Enforce "localhost" if needed.
	registry := reference.Domain(named)
	if !strings.ContainsAny(registry, ".:") && registry != "localhost" {
		name = toLocalImageName(ref.String())
	}

	// Another parse which also makes sure that docker.io references are
	// correctly normalized (e.g., docker.io/alpine to
	// docker.io/library/alpine).
	named, err = reference.ParseNormalizedNamed(name)
	if err != nil {
		return nil, err
	}

	if _, hasTag := named.(reference.NamedTagged); hasTag {
		// Strip off the tag of a tagged and digested reference.
		named, err = normalizeTaggedDigestedNamed(named)
		if err != nil {
			return nil, err
		}
		return named, nil
	}
	if _, hasDigest := named.(reference.Digested); hasDigest {
		return named, nil
	}

	// Make sure to tag "latest".
	return reference.TagNameOnly(named), nil
}

// prefix the specified name with "localhost/".
func toLocalImageName(name string) string {
	return "localhost/" + strings.TrimLeft(name, "/")
}

// NameTagPair represents a RepoTag of an image.
type NameTagPair struct {
	// Name of the RepoTag. Maybe "<none>".
	Name string
	// Tag of the RepoTag. Maybe "<none>".
	Tag string

	// for internal use
	named reference.Named
}

// ToNameTagPairs splits repoTags into name&tag pairs.
// Guaranteed to return at least one pair.
func ToNameTagPairs(repoTags []reference.Named) ([]NameTagPair, error) {
	none := "<none>"

	pairs := make([]NameTagPair, 0, len(repoTags))
	for i, named := range repoTags {
		pair := NameTagPair{
			Name:  named.Name(),
			Tag:   none,
			named: repoTags[i],
		}

		if tagged, isTagged := named.(reference.NamedTagged); isTagged {
			pair.Tag = tagged.Tag()
		}
		pairs = append(pairs, pair)
	}

	if len(pairs) == 0 {
		pairs = append(pairs, NameTagPair{Name: none, Tag: none})
	}
	return pairs, nil
}

// normalizeTaggedDigestedString strips the tag off the specified string iff it
// is tagged and digested. Note that the tag is entirely ignored to match
// Docker behavior.
func normalizeTaggedDigestedString(s string) (string, reference.Named, error) {
	// Note that the input string is not expected to be parseable, so we
	// return it verbatim in error cases.
	ref, err := reference.Parse(s)
	if err != nil {
		return "", nil, err
	}
	named, ok := ref.(reference.Named)
	if !ok {
		return s, nil, nil
	}
	named, err = normalizeTaggedDigestedNamed(named)
	if err != nil {
		return "", nil, err
	}
	return named.String(), named, nil
}

// normalizeTaggedDigestedNamed strips the tag off the specified named
// reference iff it is tagged and digested. Note that the tag is entirely
// ignored to match Docker behavior.
func normalizeTaggedDigestedNamed(named reference.Named) (reference.Named, error) {
	_, isTagged := named.(reference.NamedTagged)
	if !isTagged {
		return named, nil
	}
	digested, isDigested := named.(reference.Digested)
	if !isDigested {
		return named, nil
	}

	// Now strip off the tag.
	newNamed := reference.TrimNamed(named)
	// And re-add the digest.
	newNamed, err := reference.WithDigest(newNamed, digested.Digest())
	if err != nil {
		return named, err
	}
	logrus.Debugf("Stripped off tag from tagged and digested reference %q", named.String())
	return newNamed, nil
}
