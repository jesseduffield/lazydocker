package signer

import "go.podman.io/image/v5/internal/signer"

// Signer is an object, possibly carrying state, that can be used by copy.Image to sign one or more container images.
// It can only be created from within the containers/image package; it can’t be implemented externally.
//
// The owner of a Signer must call Close() when done.
type Signer = signer.Signer
