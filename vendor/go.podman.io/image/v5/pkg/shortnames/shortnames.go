package shortnames

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/multierr"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
	"golang.org/x/term"
)

// IsShortName returns true if the specified input is a "short name".  A "short
// name" refers to a container image without a fully-qualified reference, and
// is hence missing a registry (or domain).  Names including a digest are not
// short names.
//
// Examples:
//   - short names: "image:tag", "library/fedora"
//   - not short names: "quay.io/image", "localhost/image:tag",
//     "server.org:5000/lib/image", "image@sha256:..."
func IsShortName(input string) bool {
	isShort, _, _ := parseUnnormalizedShortName(input)
	return isShort
}

// parseUnnormalizedShortName parses the input and returns if it's short name,
// the unnormalized reference.Named, and a parsing error.
func parseUnnormalizedShortName(input string) (bool, reference.Named, error) {
	ref, err := reference.Parse(input)
	if err != nil {
		return false, nil, fmt.Errorf("cannot parse input: %q: %w", input, err)
	}

	named, ok := ref.(reference.Named)
	if !ok {
		return true, nil, fmt.Errorf("%q is not a named reference", input)
	}

	registry := reference.Domain(named)
	if strings.ContainsAny(registry, ".:") || registry == "localhost" {
		// A final parse to make sure that docker.io references are correctly
		// normalized (e.g., docker.io/alpine to docker.io/library/alpine.
		named, err = reference.ParseNormalizedNamed(input)
		if err != nil {
			return false, nil, fmt.Errorf("cannot normalize input: %q: %w", input, err)
		}
		return false, named, nil
	}

	return true, named, nil
}

// splitUserInput parses the user-specified reference.  Namely, it strips off
// the tag or digest and stores it in the return values so that both can be
// re-added to a possible resolved alias' or USRs at a later point.
func splitUserInput(named reference.Named) (isTagged bool, isDigested bool, normalized reference.Named, tag string, digest digest.Digest) {
	if tagged, ok := named.(reference.NamedTagged); ok {
		isTagged = true
		tag = tagged.Tag()
	}

	if digested, ok := named.(reference.Digested); ok {
		isDigested = true
		digest = digested.Digest()
	}

	// Strip off tag/digest if present.
	normalized = reference.TrimNamed(named)

	return
}

// Add records the specified name-value pair as a new short-name alias to the
// user-specific aliases.conf.  It may override an existing alias for `name`.
func Add(ctx *types.SystemContext, name string, value reference.Named) error {
	isShort, _, err := parseUnnormalizedShortName(name)
	if err != nil {
		return err
	}
	if !isShort {
		return fmt.Errorf("%q is not a short name", name)
	}
	return sysregistriesv2.AddShortNameAlias(ctx, name, value.String())
}

// Remove clears the short-name alias for the specified name.  It throws an
// error in case name does not exist in the machine-generated
// short-name-alias.conf.  In such case, the alias must be specified in one of
// the registries.conf files, which is the users' responsibility.
func Remove(ctx *types.SystemContext, name string) error {
	isShort, _, err := parseUnnormalizedShortName(name)
	if err != nil {
		return err
	}
	if !isShort {
		return fmt.Errorf("%q is not a short name", name)
	}
	return sysregistriesv2.RemoveShortNameAlias(ctx, name)
}

// Resolved encapsulates all data for a resolved image name.
type Resolved struct {
	PullCandidates []PullCandidate

	userInput         reference.Named
	systemContext     *types.SystemContext
	rationale         rationale
	originDescription string
}

func (r *Resolved) addCandidate(named reference.Named) {
	named = reference.TagNameOnly(named) // Make sure to add ":latest" if needed
	r.PullCandidates = append(r.PullCandidates, PullCandidate{named, false, r})
}

func (r *Resolved) addCandidateToRecord(named reference.Named) {
	r.PullCandidates = append(r.PullCandidates, PullCandidate{named, true, r})
}

// Allows to reason over pull errors and add some context information.
// Used in (*Resolved).WrapPullError.
type rationale int

const (
	// No additional context.
	rationaleNone rationale = iota
	// Resolved value is a short-name alias.
	rationaleAlias
	// Resolved value has been completed with an Unqualified Search Registry.
	rationaleUSR
	// Resolved value has been selected by the user (via the prompt).
	rationaleUserSelection
	// Resolved value has been enforced to use Docker Hub (via SystemContext).
	rationaleEnforcedDockerHub
)

// Description returns a human-readable description about the resolution
// process (e.g., short-name alias, unqualified-search registries, etc.).
// It is meant to be printed before attempting to pull the pull candidates
// to make the short-name resolution more transparent to user.
//
// If the returned string is empty, it is not meant to be printed.
func (r *Resolved) Description() string {
	switch r.rationale {
	case rationaleAlias:
		return fmt.Sprintf("Resolved %q as an alias (%s)", r.userInput, r.originDescription)
	case rationaleUSR:
		return fmt.Sprintf("Resolving %q using unqualified-search registries (%s)", r.userInput, r.originDescription)
	case rationaleEnforcedDockerHub:
		return fmt.Sprintf("Resolving %q to docker.io (%s)", r.userInput, r.originDescription)
	case rationaleUserSelection, rationaleNone:
		fallthrough
	default:
		return ""
	}
}

// FormatPullErrors is a convenience function to format errors that occurred
// while trying to pull all of the resolved pull candidates.
//
// Note that nil is returned if len(pullErrors) == 0.  Otherwise, the amount of
// pull errors must equal the amount of pull candidates.
func (r *Resolved) FormatPullErrors(pullErrors []error) error {
	if len(pullErrors) == 0 {
		return nil
	}

	if len(pullErrors) != len(r.PullCandidates) {
		pullErrors = append(slices.Clone(pullErrors),
			fmt.Errorf("internal error: expected %d instead of %d errors for %d pull candidates",
				len(r.PullCandidates), len(pullErrors), len(r.PullCandidates)))
	}

	return multierr.Format(fmt.Sprintf("%d errors occurred while pulling:\n * ", len(pullErrors)), "\n * ", "", pullErrors)
}

// PullCandidate is a resolved name.  Once the Value has been used
// successfully, users MUST call `(*PullCandidate).Record(..)` to possibly
// record it as a new short-name alias.
type PullCandidate struct {
	// Fully-qualified reference with tag or digest.
	Value reference.Named
	// Control whether to record it permanently as an alias.
	record bool

	// Backwards pointer to the Resolved "parent".
	resolved *Resolved
}

// Record may store a short-name alias for the PullCandidate.
func (c *PullCandidate) Record() error {
	if !c.record {
		return nil
	}

	// Strip off tags/digests from name/value.
	name := reference.TrimNamed(c.resolved.userInput)
	value := reference.TrimNamed(c.Value)

	if err := Add(c.resolved.systemContext, name.String(), value); err != nil {
		return fmt.Errorf("recording short-name alias (%q=%q): %w", c.resolved.userInput, c.Value, err)
	}
	return nil
}

// Resolve resolves the specified name to either one or more fully-qualified
// image references that the short name may be *pulled* from.  If the specified
// name is already a fully-qualified reference (i.e., not a short name), it is
// returned as is.  In case, it's a short name, it's resolved according to the
// ShortNameMode in the SystemContext (if specified) or in the registries.conf.
//
// Note that tags and digests are stripped from the specified name before
// looking up an alias. Stripped off tags and digests are later on appended to
// all candidates.  If neither tag nor digest is specified, candidates are
// normalized with the "latest" tag.  An error is returned if there is no
// matching alias and no unqualified-search registries are configured.
//
// Note that callers *must* call `(PullCandidate).Record` after a returned
// item has been pulled successfully; this callback will record a new
// short-name alias (depending on the specified short-name mode).
//
// Furthermore, before attempting to pull callers *should* call
// `(Resolved).Description` and afterwards use
// `(Resolved).FormatPullErrors` in case of pull errors.
func Resolve(ctx *types.SystemContext, name string) (*Resolved, error) {
	resolved := &Resolved{}

	// Create a copy of the system context to make it usable beyond this
	// function call.
	if ctx != nil {
		copy := *ctx
		ctx = &copy
	}
	resolved.systemContext = ctx

	// Detect which mode we're running in.
	mode, err := sysregistriesv2.GetShortNameMode(ctx)
	if err != nil {
		return nil, err
	}

	// Sanity check the short-name mode.
	switch mode {
	case types.ShortNameModeDisabled, types.ShortNameModePermissive, types.ShortNameModeEnforcing:
		// We're good.
	default:
		return nil, fmt.Errorf("unsupported short-name mode (%v)", mode)
	}

	isShort, shortRef, err := parseUnnormalizedShortName(name)
	if err != nil {
		return nil, err
	}
	if !isShort { // no short name
		resolved.addCandidate(shortRef)
		return resolved, nil
	}

	// Resolve to docker.io only if enforced by the caller (e.g., Podman's
	// Docker-compatible REST API).
	if ctx != nil && ctx.PodmanOnlyShortNamesIgnoreRegistriesConfAndForceDockerHub {
		named, err := reference.ParseNormalizedNamed(name)
		if err != nil {
			return nil, fmt.Errorf("cannot normalize input: %q: %w", name, err)
		}
		resolved.addCandidate(named)
		resolved.rationale = rationaleEnforcedDockerHub
		resolved.originDescription = "enforced by caller"
		return resolved, nil
	}

	// Strip off the tag to normalize the short name for looking it up in
	// the config files.
	isTagged, isDigested, shortNameRepo, tag, digest := splitUserInput(shortRef)
	resolved.userInput = shortNameRepo

	// If there's already an alias, use it.
	namedAlias, aliasOriginDescription, err := sysregistriesv2.ResolveShortNameAlias(ctx, shortNameRepo.String())
	if err != nil {
		return nil, err
	}

	// Always use an alias if present.
	if namedAlias != nil {
		if isTagged {
			namedAlias, err = reference.WithTag(namedAlias, tag)
			if err != nil {
				return nil, err
			}
		}
		if isDigested {
			namedAlias, err = reference.WithDigest(namedAlias, digest)
			if err != nil {
				return nil, err
			}
		}
		resolved.addCandidate(namedAlias)
		resolved.rationale = rationaleAlias
		resolved.originDescription = aliasOriginDescription
		return resolved, nil
	}

	resolved.rationale = rationaleUSR

	// Query the registry for unqualified-search registries.
	unqualifiedSearchRegistries, usrConfig, err := sysregistriesv2.UnqualifiedSearchRegistriesWithOrigin(ctx)
	if err != nil {
		return nil, err
	}
	// Error out if there's no matching alias and no search registries.
	if len(unqualifiedSearchRegistries) == 0 {
		if usrConfig != "" {
			return nil, fmt.Errorf("short-name %q did not resolve to an alias and no unqualified-search registries are defined in %q", name, usrConfig)
		}
		return nil, fmt.Errorf("short-name %q did not resolve to an alias and no containers-registries.conf(5) was found", name)
	}
	resolved.originDescription = usrConfig

	for _, reg := range unqualifiedSearchRegistries {
		named, err := reference.ParseNormalizedNamed(fmt.Sprintf("%s/%s", reg, name))
		if err != nil {
			return nil, fmt.Errorf("creating reference with unqualified-search registry %q: %w", reg, err)
		}
		resolved.addCandidate(named)
	}

	// If we're running in disabled, return the candidates without
	// prompting (and without recording).
	if mode == types.ShortNameModeDisabled {
		return resolved, nil
	}

	// If we have only one candidate, there's no ambiguity.
	if len(resolved.PullCandidates) == 1 {
		return resolved, nil
	}

	// If we don't have a TTY, act according to the mode.
	if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
		switch mode {
		case types.ShortNameModePermissive:
			// Permissive falls back to using all candidates.
			return resolved, nil
		case types.ShortNameModeEnforcing:
			// Enforcing errors out without a prompt.
			return nil, errors.New("short-name resolution enforced but cannot prompt without a TTY")
		default:
			// We should not end up here.
			return nil, fmt.Errorf("unexpected short-name mode (%v) during resolution", mode)
		}
	}

	// We have a TTY, and can prompt the user with a selection of all
	// possible candidates.
	strCandidates := []string{}
	for _, candidate := range resolved.PullCandidates {
		strCandidates = append(strCandidates, candidate.Value.String())
	}
	prompt := promptui.Select{
		Label:    "Please select an image",
		Items:    strCandidates,
		HideHelp: true, // do not show navigation help
	}

	_, selection, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	named, err := reference.ParseNormalizedNamed(selection)
	if err != nil {
		return nil, fmt.Errorf("selection %q is not a valid reference: %w", selection, err)
	}

	resolved.PullCandidates = nil
	resolved.addCandidateToRecord(named)
	resolved.rationale = rationaleUserSelection

	return resolved, nil
}

// ResolveLocally resolves the specified name to either one or more local
// images.  If the specified name is already a fully-qualified reference (i.e.,
// not a short name), it is returned as is.  In case, it's a short name, the
// returned slice of named references looks as follows:
//
//  1. If present, the short-name alias
//  2. "localhost/" as used by many container engines such as Podman and Buildah
//  3. Unqualified-search registries from the registries.conf files
//
// Note that tags and digests are stripped from the specified name before
// looking up an alias. Stripped off tags and digests are later on appended to
// all candidates.  If neither tag nor digest is specified, candidates are
// normalized with the "latest" tag. The returned slice contains at least one
// item.
func ResolveLocally(ctx *types.SystemContext, name string) ([]reference.Named, error) {
	isShort, shortRef, err := parseUnnormalizedShortName(name)
	if err != nil {
		return nil, err
	}
	if !isShort { // no short name
		named := reference.TagNameOnly(shortRef) // Make sure to add ":latest" if needed
		return []reference.Named{named}, nil
	}

	var candidates []reference.Named

	// Complete the candidates with the specified registries.
	completeCandidates := func(registries []string) ([]reference.Named, error) {
		for _, reg := range registries {
			named, err := reference.ParseNormalizedNamed(fmt.Sprintf("%s/%s", reg, name))
			if err != nil {
				return nil, fmt.Errorf("creating reference with unqualified-search registry %q: %w", reg, err)
			}
			named = reference.TagNameOnly(named) // Make sure to add ":latest" if needed
			candidates = append(candidates, named)
		}
		return candidates, nil
	}

	if ctx != nil && ctx.PodmanOnlyShortNamesIgnoreRegistriesConfAndForceDockerHub {
		return completeCandidates([]string{"docker.io"})
	}

	// Strip off the tag to normalize the short name for looking it up in
	// the config files.
	isTagged, isDigested, shortNameRepo, tag, digest := splitUserInput(shortRef)

	// If there's already an alias, use it.
	namedAlias, _, err := sysregistriesv2.ResolveShortNameAlias(ctx, shortNameRepo.String())
	if err != nil {
		return nil, err
	}
	if namedAlias != nil {
		if isTagged {
			namedAlias, err = reference.WithTag(namedAlias, tag)
			if err != nil {
				return nil, err
			}
		}
		if isDigested {
			namedAlias, err = reference.WithDigest(namedAlias, digest)
			if err != nil {
				return nil, err
			}
		}
		namedAlias = reference.TagNameOnly(namedAlias) // Make sure to add ":latest" if needed
		candidates = append(candidates, namedAlias)
	}

	// Query the registry for unqualified-search registries.
	unqualifiedSearchRegistries, err := sysregistriesv2.UnqualifiedSearchRegistries(ctx)
	if err != nil {
		return nil, err
	}

	// Note that "localhost" has precedence over the unqualified-search registries.
	return completeCandidates(append([]string{"localhost"}, unqualifiedSearchRegistries...))
}
