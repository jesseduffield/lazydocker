package chrootuser

import (
	"errors"
	"fmt"
	"os/user"
	"strconv"
	"strings"
)

// ErrNoSuchUser indicates that the user provided by the caller does not
// exist in /etc/passws
var ErrNoSuchUser = errors.New("user does not exist in /etc/passwd")

// GetUser will return the uid, gid of the user specified in the userspec
// it will use the /etc/passwd and /etc/group files inside of the rootdir
// to return this information.
// userspec format [user | user:group | uid | uid:gid | user:gid | uid:group ]
func GetUser(rootdir, userspec string) (uint32, uint32, string, error) {
	var gid64 uint64
	var gerr error = user.UnknownGroupError("error looking up group")

	spec := strings.SplitN(userspec, ":", 2)
	userspec = spec[0]
	groupspec := ""

	if userspec == "" {
		userspec = "0"
	}

	if len(spec) > 1 {
		groupspec = spec[1]
	}

	uid64, uerr := strconv.ParseUint(userspec, 10, 32)
	if uerr == nil && groupspec == "" {
		// We parsed the user name as a number, and there's no group
		// component, so try to look up the primary GID of the user who
		// has this UID.
		var name string
		name, gid64, gerr = lookupGroupForUIDInContainer(rootdir, uid64)
		if gerr == nil {
			userspec = name
		} else {
			// Leave userspec alone, but swallow the error and just
			// use GID 0.
			gid64 = 0
			gerr = nil
		}
	}
	if uerr != nil {
		// The user ID couldn't be parsed as a number, so try to look
		// up the user's UID and primary GID.
		uid64, gid64, uerr = lookupUserInContainer(rootdir, userspec)
		gerr = uerr
	}

	if groupspec != "" {
		// We have a group name or number, so parse it.
		gid64, gerr = strconv.ParseUint(groupspec, 10, 32)
		if gerr != nil {
			// The group couldn't be parsed as a number, so look up
			// the group's GID.
			gid64, gerr = lookupGroupInContainer(rootdir, groupspec)
		}
	}

	homedir, err := lookupHomedirInContainer(rootdir, uid64)
	if err != nil {
		homedir = "/"
	}

	if uerr == nil && gerr == nil {
		return uint32(uid64), uint32(gid64), homedir, nil
	}

	err = fmt.Errorf("determining run uid: %w", uerr)
	if uerr == nil {
		err = fmt.Errorf("determining run gid: %w", gerr)
	}

	return 0, 0, homedir, err
}

// GetGroup returns the gid by looking it up in the /etc/group file
// groupspec format [ group | gid ]
func GetGroup(rootdir, groupspec string) (uint32, error) {
	gid64, gerr := strconv.ParseUint(groupspec, 10, 32)
	if gerr != nil {
		// The group couldn't be parsed as a number, so look up
		// the group's GID.
		gid64, gerr = lookupGroupInContainer(rootdir, groupspec)
	}
	if gerr != nil {
		return 0, fmt.Errorf("looking up group for gid %q: %w", groupspec, gerr)
	}
	return uint32(gid64), nil
}

// GetAdditionalGroupsForUser returns a list of gids that userid is associated with
func GetAdditionalGroupsForUser(rootdir string, userid uint64) ([]uint32, error) {
	gids, err := lookupAdditionalGroupsForUIDInContainer(rootdir, userid)
	if err != nil {
		return nil, fmt.Errorf("looking up supplemental groups for uid %d: %w", userid, err)
	}
	return gids, nil
}

// LookupUIDInContainer returns username and gid associated with a UID in a container
// it will use the /etc/passwd files inside of the rootdir
// to return this information.
func LookupUIDInContainer(rootdir string, uid uint64) (user string, gid uint64, err error) {
	return lookupUIDInContainer(rootdir, uid)
}
