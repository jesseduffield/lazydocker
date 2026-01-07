//go:build !linux && !freebsd

package chrootuser

import (
	"errors"
)

func lookupUserInContainer(rootdir, username string) (uint64, uint64, error) {
	return 0, 0, errors.New("user lookup not supported")
}

func lookupGroupInContainer(rootdir, groupname string) (uint64, error) {
	return 0, errors.New("group lookup not supported")
}

func lookupGroupForUIDInContainer(rootdir string, userid uint64) (string, uint64, error) {
	return "", 0, errors.New("primary group lookup by uid not supported")
}

func lookupAdditionalGroupsForUIDInContainer(rootdir string, userid uint64) (gid []uint32, err error) {
	return nil, errors.New("supplemental groups list lookup by uid not supported")
}

func lookupUIDInContainer(rootdir string, uid uint64) (string, uint64, error) {
	return "", 0, errors.New("UID lookup not supported")
}

func lookupHomedirInContainer(rootdir string, uid uint64) (string, error) {
	return "", errors.New("Home directory lookup not supported")
}
