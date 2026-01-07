// Package stubs contains trivial stubs for parts of private.ImageSource.
// It can be used from internal/wrapper, so it should not drag in any extra dependencies.
// Compare with imagesource/impl, which might require non-trivial implementation work.
//
// There are two kinds of stubs:
//
// First, there are pure stubs, like ImplementsGetBlobAt. Those can just be included in an ImageSource
//
// implementation:
//
//	type yourSource struct {
//		stubs.ImplementsGetBlobAt
//		…
//	}
//
// Second, there are stubs with a constructor, like NoGetBlobAtInitialize. The Initialize marker
// means that a constructor must be called:
//
//	type yourSource struct {
//		stubs.NoGetBlobAtInitialize
//		…
//	}
//
//	dest := &yourSource{
//		…
//		NoGetBlobAtInitialize: stubs.NoGetBlobAt(ref),
//	}
package stubs
