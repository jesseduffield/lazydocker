package reference

// Return true if the specified string fully matches `IdentifierRegexp`.
func IsFullIdentifier(s string) bool {
	return anchoredIdentifierRegexp.MatchString(s)
}
