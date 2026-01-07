package internal

// CheckRequestedWidth checks that requested width doesn't overflow
// available width
func CheckRequestedWidth(requested, available int) int {
	if requested < 1 || requested > available {
		return available
	}
	return requested
}
