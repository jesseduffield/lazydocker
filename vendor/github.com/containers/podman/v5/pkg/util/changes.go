package util

import (
	"strings"
	"unicode"
)

// DecodeChanges reads one or more changes from a slice and cleans them up,
// since what we've advertised as being acceptable in the past isn't really.
func DecodeChanges(changes []string) []string {
	result := make([]string, 0, len(changes))
	for _, possiblyMultilineChange := range changes {
		for change := range strings.SplitSeq(possiblyMultilineChange, "\n") {
			// In particular, we document that we accept values
			// like "CMD=/bin/sh", which is not valid Dockerfile
			// syntax, so we can't just pass such a value directly
			// to a parser that's going to rightfully reject it.
			// If we trim the string of whitespace at both ends,
			// and the first occurrence of "=" is before the first
			// whitespace, replace that "=" with whitespace.
			change = strings.TrimSpace(change)
			if change == "" {
				continue
			}
			firstEqualIndex := strings.Index(change, "=")
			firstSpaceIndex := strings.IndexFunc(change, unicode.IsSpace)
			if firstEqualIndex != -1 && (firstSpaceIndex == -1 || firstEqualIndex < firstSpaceIndex) {
				change = change[:firstEqualIndex] + " " + change[firstEqualIndex+1:]
			}
			result = append(result, change)
		}
	}
	return result
}
