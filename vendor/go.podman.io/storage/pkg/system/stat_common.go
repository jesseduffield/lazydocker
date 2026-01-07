//go:build !freebsd

package system

type platformStatT struct{}

// Flags return file flags if supported or zero otherwise
func (s StatT) Flags() uint32 {
	_ = s.platformStatT // Silence warnings that StatT.platformStatT is unused (on these platforms)
	return 0
}
