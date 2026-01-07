package chown

import (
	"errors"
)

// ChangeHostPathOwnership changes the uid and gid ownership of a directory or file within the host.
// This is used by the volume U flag to change source volumes ownership
func ChangeHostPathOwnership(path string, recursive bool, uid, gid int) error {
	return errors.New("windows not supported")
}
