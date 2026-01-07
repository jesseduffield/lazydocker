//go:build !remote

package specgen

import "go.podman.io/common/libimage"

type cacheLibImage struct {
	image             *libimage.Image `json:"-"`
	resolvedImageName string          `json:"-"`
}

// SetImage sets the associated for the generator.
func (s *SpecGenerator) SetImage(image *libimage.Image, resolvedImageName string) {
	s.image = image
	s.resolvedImageName = resolvedImageName
}

// Image returns the associated image for the generator.
// May be nil if no image has been set yet.
func (s *SpecGenerator) GetImage() (*libimage.Image, string) {
	return s.image, s.resolvedImageName
}
