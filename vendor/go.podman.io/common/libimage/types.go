package libimage

import "go.podman.io/common/libimage/manifests"

// LookupReferenceFunc return an image reference based on the specified one.
// The returned reference can return custom ImageSource or ImageDestination
// objects which intercept or filter blobs, manifests, and signatures as
// they are read and written.
type LookupReferenceFunc = manifests.LookupReferenceFunc
