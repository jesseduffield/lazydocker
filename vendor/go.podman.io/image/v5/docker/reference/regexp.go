package reference

import (
	"regexp"
	"strings"

	storageRegexp "go.podman.io/storage/pkg/regexp"
)

const (
	// alphaNumeric defines the alpha numeric atom, typically a
	// component of names. This only allows lower case characters and digits.
	alphaNumeric = `[a-z0-9]+`

	// separator defines the separators allowed to be embedded in name
	// components. This allow one period, one or two underscore and multiple
	// dashes. Repeated dashes and underscores are intentionally treated
	// differently. In order to support valid hostnames as name components,
	// supporting repeated dash was added. Additionally double underscore is
	// now allowed as a separator to loosen the restriction for previously
	// supported names.
	separator = `(?:[._]|__|[-]*)`

	// repository name to start with a component as defined by DomainRegexp
	// and followed by an optional port.
	domainComponent = `(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])`

	// The string counterpart for TagRegexp.
	tag = `[\w][\w.-]{0,127}`

	// The string counterpart for DigestRegexp.
	digestPat = `[A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}`

	// The string counterpart for IdentifierRegexp.
	identifier = `([a-f0-9]{64})`

	// The string counterpart for ShortIdentifierRegexp.
	shortIdentifier = `([a-f0-9]{6,64})`
)

var (
	// nameComponent restricts registry path component names to start
	// with at least one letter or number, with following parts able to be
	// separated by one period, one or two underscore and multiple dashes.
	nameComponent = expression(
		alphaNumeric,
		optional(repeated(separator, alphaNumeric)))

	domain = expression(
		domainComponent,
		optional(repeated(literal(`.`), domainComponent)),
		optional(literal(`:`), `[0-9]+`))
	// DomainRegexp defines the structure of potential domain components
	// that may be part of image names. This is purposely a subset of what is
	// allowed by DNS to ensure backwards compatibility with Docker image
	// names.
	DomainRegexp = re(domain)

	// TagRegexp matches valid tag names. From docker/docker:graph/tags.go.
	TagRegexp = re(tag)

	anchoredTag = anchored(tag)
	// anchoredTagRegexp matches valid tag names, anchored at the start and
	// end of the matched string.
	anchoredTagRegexp = storageRegexp.Delayed(anchoredTag)

	// DigestRegexp matches valid digests.
	DigestRegexp = re(digestPat)

	anchoredDigest = anchored(digestPat)
	// anchoredDigestRegexp matches valid digests, anchored at the start and
	// end of the matched string.
	anchoredDigestRegexp = storageRegexp.Delayed(anchoredDigest)

	namePat = expression(
		optional(domain, literal(`/`)),
		nameComponent,
		optional(repeated(literal(`/`), nameComponent)))
	// NameRegexp is the format for the name component of references. The
	// regexp has capturing groups for the domain and name part omitting
	// the separating forward slash from either.
	NameRegexp = re(namePat)

	anchoredName = anchored(
		optional(capture(domain), literal(`/`)),
		capture(nameComponent,
			optional(repeated(literal(`/`), nameComponent))))
	// anchoredNameRegexp is used to parse a name value, capturing the
	// domain and trailing components.
	anchoredNameRegexp = storageRegexp.Delayed(anchoredName)

	referencePat = anchored(capture(namePat),
		optional(literal(":"), capture(tag)),
		optional(literal("@"), capture(digestPat)))
	// ReferenceRegexp is the full supported format of a reference. The regexp
	// is anchored and has capturing groups for name, tag, and digest
	// components.
	ReferenceRegexp = re(referencePat)

	// IdentifierRegexp is the format for string identifier used as a
	// content addressable identifier using sha256. These identifiers
	// are like digests without the algorithm, since sha256 is used.
	IdentifierRegexp = re(identifier)

	// ShortIdentifierRegexp is the format used to represent a prefix
	// of an identifier. A prefix may be used to match a sha256 identifier
	// within a list of trusted identifiers.
	ShortIdentifierRegexp = re(shortIdentifier)

	anchoredIdentifier = anchored(identifier)
	// anchoredIdentifierRegexp is used to check or match an
	// identifier value, anchored at start and end of string.
	anchoredIdentifierRegexp = storageRegexp.Delayed(anchoredIdentifier)
)

// re compiles the string to a regular expression.
var re = regexp.MustCompile

// literal compiles s into a literal regular expression, escaping any regexp
// reserved characters.
func literal(s string) string {
	return regexp.QuoteMeta(s)
}

// expression defines a full expression, where each regular expression must
// follow the previous.
func expression(res ...string) string {
	return strings.Join(res, "")
}

// optional wraps the expression in a non-capturing group and makes the
// production optional.
func optional(res ...string) string {
	return group(expression(res...)) + `?`
}

// repeated wraps the regexp in a non-capturing group to get one or more
// matches.
func repeated(res ...string) string {
	return group(expression(res...)) + `+`
}

// group wraps the regexp in a non-capturing group.
func group(res ...string) string {
	return `(?:` + expression(res...) + `)`
}

// capture wraps the expression in a capturing group.
func capture(res ...string) string {
	return `(` + expression(res...) + `)`
}

// anchored anchors the regular expression by adding start and end delimiters.
func anchored(res ...string) string {
	return `^` + expression(res...) + `$`
}
