//go:build !linux || !libsubid || !cgo

package idtools

func readSubuid(username string) (ranges, error) {
	return parseSubidFile(subuidFileName, username)
}

func readSubgid(username string) (ranges, error) {
	return parseSubidFile(subgidFileName, username)
}
