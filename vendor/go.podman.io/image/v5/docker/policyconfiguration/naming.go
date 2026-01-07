package policyconfiguration

import (
	"errors"
	"fmt"
	"strings"

	"go.podman.io/image/v5/docker/reference"
)

// DockerReferenceIdentity returns a string representation of the reference, suitable for policy lookup,
// as a backend for ImageReference.PolicyConfigurationIdentity.
// The reference must satisfy !reference.IsNameOnly().
func DockerReferenceIdentity(ref reference.Named) (string, error) {
	res := ref.Name()
	tagged, isTagged := ref.(reference.NamedTagged)
	digested, isDigested := ref.(reference.Canonical)
	switch {
	case isTagged && isDigested: // Note that this CAN actually happen.
		return "", fmt.Errorf("Unexpected Docker reference %s with both a name and a digest", reference.FamiliarString(ref))
	case !isTagged && !isDigested: // This should not happen, the caller is expected to ensure !reference.IsNameOnly()
		return "", fmt.Errorf("Internal inconsistency: Docker reference %s with neither a tag nor a digest", reference.FamiliarString(ref))
	case isTagged:
		res = res + ":" + tagged.Tag()
	case isDigested:
		res = res + "@" + digested.Digest().String()
	default: // Coverage: The above was supposed to be exhaustive.
		return "", errors.New("Internal inconsistency, unexpected default branch")
	}
	return res, nil
}

// DockerReferenceNamespaces returns a list of other policy configuration namespaces to search,
// as a backend for ImageReference.PolicyConfigurationIdentity.
// The reference must satisfy !reference.IsNameOnly().
func DockerReferenceNamespaces(ref reference.Named) []string {
	// Look for a match of the repository, and then of the possible parent
	// namespaces. Note that this only happens on the expanded host names
	// and repository names, i.e. "busybox" is looked up as "docker.io/library/busybox",
	// then in its parent "docker.io/library"; in none of "busybox",
	// un-namespaced "library" nor in "" supposedly implicitly representing "library/".
	//
	// ref.Name() == ref.Domain() + "/" + ref.Path(), so the last
	// iteration matches the host name (for any namespace).
	res := []string{}
	name := ref.Name()
	for {
		res = append(res, name)

		lastSlash := strings.LastIndex(name, "/")
		if lastSlash == -1 {
			break
		}
		name = name[:lastSlash]
	}

	// Strip port number if any, before appending to res slice.
	// Currently, the most compatible behavior is to return
	// example.com:8443/ns, example.com:8443, *.com.
	// If a port number is not specified, the expected behavior would be
	// example.com/ns, example.com, *.com
	portNumColon := strings.Index(name, ":")
	if portNumColon != -1 {
		name = name[:portNumColon]
	}

	// Append wildcarded domains to res slice
	for {
		firstDot := strings.Index(name, ".")
		if firstDot == -1 {
			break
		}
		name = name[firstDot+1:]

		res = append(res, "*."+name)
	}
	return res
}
