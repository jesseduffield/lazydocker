package annotations

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
)

// regexErrorMsg returns a string explanation of a regex validation failure.
func regexErrorMsg(msg string, fmt string, examples ...string) string {
	if len(examples) == 0 {
		return msg + " (regex used for validation is '" + fmt + "')"
	}
	msg += " (e.g. "
	for i := range examples {
		if i > 0 {
			msg += " or "
		}
		msg += "'" + examples[i] + "', "
	}
	msg += "regex used for validation is '" + fmt + "')"
	return msg
}

const dns1123LabelFmt string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"
const dns1123SubdomainFmt string = dns1123LabelFmt + "(\\." + dns1123LabelFmt + ")*"
const dns1123SubdomainErrorMsg string = "annotations must be formatted as a valid lowercase RFC1123 subdomain of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"

// DNS1123SubdomainMaxLength is a subdomain's max length in DNS (RFC 1123)
const DNS1123SubdomainMaxLength int = 253

var dns1123SubdomainRegexp = regexp.MustCompile("^" + dns1123SubdomainFmt + "$")

// isDNS1123Subdomain tests for a string that conforms to the definition of a
// subdomain in DNS (RFC 1123).
func isDNS1123Subdomain(value string) error {
	if len(value) > DNS1123SubdomainMaxLength {
		return fmt.Errorf("prefix part must be no more than %d characters", DNS1123SubdomainMaxLength)
	}

	if !dns1123SubdomainRegexp.MatchString(value) {
		return errors.New(regexErrorMsg(dns1123SubdomainErrorMsg, dns1123SubdomainFmt, "example.com"))
	}

	return nil
}

const qnameCharFmt string = "[A-Za-z0-9]"
const qnameExtCharFmt string = "[-A-Za-z0-9_.]"
const qualifiedNameFmt string = "(" + qnameCharFmt + qnameExtCharFmt + "*)?" + qnameCharFmt
const qualifiedNameErrMsg string = "must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character"
const qualifiedNameMaxLength int = 63

var qualifiedNameRegexp = regexp.MustCompile("^" + qualifiedNameFmt + "$")

// isQualifiedName tests whether the value passed is what Kubernetes calls a
// "qualified name".  This is a format used in various places throughout the
// system.  If the value is not valid, a list of error strings is returned.
// Otherwise an empty list (or nil) is returned.
func isQualifiedName(value string) error {
	parts := strings.Split(value, "/")
	var name string

	switch len(parts) {
	case 1:
		name = parts[0]
	case 2:
		var prefix string
		prefix, name = parts[0], parts[1]
		if len(prefix) == 0 {
			return fmt.Errorf("prefix part of %s must be non-empty", value)
		} else if err := isDNS1123Subdomain(prefix); err != nil {
			return err
		}
	default:
		return fmt.Errorf("a qualified name of %s "+
			regexErrorMsg(qualifiedNameErrMsg, qualifiedNameFmt, "MyName", "my.name", "123-abc")+
			" with an optional DNS subdomain prefix and '/' (e.g. 'example.com/MyName')", value)
	}

	if len(name) == 0 {
		return fmt.Errorf("name part of %s must be non-empty", value)
	} else if len(name) > qualifiedNameMaxLength {
		return fmt.Errorf("name part of %s must be no more than %d characters", value, qualifiedNameMaxLength)
	}

	if !qualifiedNameRegexp.MatchString(name) {
		return fmt.Errorf("name part of %s "+
			regexErrorMsg(qualifiedNameErrMsg, qualifiedNameFmt, "MyName", "my.name", "123-abc"), value)
	}

	return nil
}

func validateAnnotationsSize(annotations map[string]string) error {
	var totalSize int64
	for k, v := range annotations {
		totalSize += (int64)(len(k)) + (int64)(len(v))
	}
	if totalSize > (int64)(define.TotalAnnotationSizeLimitB) {
		return fmt.Errorf("annotations size %d is larger than limit %d", totalSize, define.TotalAnnotationSizeLimitB)
	}
	return nil
}

// ValidateAnnotations validates that a set of annotations are correctly
// defined.
func ValidateAnnotations(annotations map[string]string) error {
	for k := range annotations {
		// The rule is QualifiedName except that case doesn't matter,
		// so convert to lowercase before checking.
		if err := isQualifiedName(strings.ToLower(k)); err != nil {
			return err
		}
	}

	if err := validateAnnotationsSize(annotations); err != nil {
		return err
	}

	return nil
}
