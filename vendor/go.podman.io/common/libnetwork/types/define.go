package types

import (
	"errors"
	"fmt"

	"go.podman.io/storage/pkg/regexp"
)

var (
	// ErrNoSuchNetwork indicates the requested network does not exist.
	ErrNoSuchNetwork = errors.New("network not found")

	// ErrInvalidArg indicates that an invalid argument was passed.
	ErrInvalidArg = errors.New("invalid argument")

	// ErrNetworkExists indicates that a network with the given name already
	// exists.
	ErrNetworkExists = errors.New("network already exists")

	// ErrNotRootlessNetns indicates the rootless netns can only be used as root.
	ErrNotRootlessNetns = errors.New("rootless netns cannot be used as root")

	// NameRegex is a regular expression to validate names.
	// This must NOT be changed.
	NameRegex = regexp.Delayed("^[a-zA-Z0-9][a-zA-Z0-9_.-]*$")
	// ErrInvalidName is thrown in presence of an invalid name.
	ErrInvalidName = fmt.Errorf("names must match [a-zA-Z0-9][a-zA-Z0-9_.-]*: %w", ErrInvalidArg)
	// Deprecated: use [ErrInvalidName] instead.
	RegexError = ErrInvalidName

	// NotHexRegex is a regular expression to check if a string is
	// a hexadecimal string.
	NotHexRegex = regexp.Delayed(`[^0-9a-fA-F]`)

	// MaxInterfaceNameLength is the maximum length of a network interface name.
	MaxInterfaceNameLength = 15
)
