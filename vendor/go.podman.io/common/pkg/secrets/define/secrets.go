package define

import (
	"errors"
)

var (
	// ErrNoSuchSecret indicates that the secret does not exist.
	ErrNoSuchSecret = errors.New("no such secret")

	// ErrSecretIDExists indicates that there is secret data already associated with an id.
	ErrSecretIDExists = errors.New("secret data with ID already exists")

	// ErrInvalidKey indicates that something about your key is wrong.
	ErrInvalidKey = errors.New("invalid key")
)
