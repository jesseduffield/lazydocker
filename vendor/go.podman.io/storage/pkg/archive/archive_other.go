//go:build !linux

package archive

func GetWhiteoutConverter(format WhiteoutFormat, data any) TarWhiteoutConverter {
	return nil
}

func GetFileOwner(path string) (uint32, uint32, uint32, error) {
	return 0, 0, 0, nil
}
