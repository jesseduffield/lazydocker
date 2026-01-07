//go:build !linux && !freebsd

package events

import "errors"

// NewEventer creates an eventer based on the eventer type
func NewEventer(_ EventerOptions) (Eventer, error) {
	return nil, errors.New("this function is not available for your platform")
}
