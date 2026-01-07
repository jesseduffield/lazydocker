//go:build !linux && !windows

package specgen

func shouldResolveWinPaths() bool {
	return false
}

func shouldResolveUnixWinVariant(_ string) bool {
	return false
}

func resolveRelativeOnWindows(path string) string {
	return path
}

func winPathExists(_ string) bool {
	return false
}
