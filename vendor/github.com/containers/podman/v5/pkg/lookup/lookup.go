package lookup

import (
	"os"
	"strconv"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/moby/sys/user"
	"github.com/sirupsen/logrus"
)

const (
	etcpasswd = "/etc/passwd"
	etcgroup  = "/etc/group"
)

// Overrides allows you to override defaults in GetUserGroupInfo.
type Overrides struct {
	DefaultUser            *user.ExecUser
	ContainerEtcPasswdPath string
	ContainerEtcGroupPath  string
}

// GetUserGroupInfo takes string forms of the container's mount path and the container user and
// returns an ExecUser with uid, gid, sgids, and home.  And override can be provided for defaults.
func GetUserGroupInfo(containerMount, containerUser string, override *Overrides) (*user.ExecUser, error) {
	var (
		passwdDest, groupDest string
		defaultExecUser       *user.ExecUser
		err                   error
	)

	if override != nil {
		// Check for an override /etc/passwd path
		if override.ContainerEtcPasswdPath != "" {
			passwdDest = override.ContainerEtcPasswdPath
		}
		// Check for an override for /etc/group path
		if override.ContainerEtcGroupPath != "" {
			groupDest = override.ContainerEtcGroupPath
		}
	}

	if passwdDest == "" {
		// Make sure the /etc/passwd destination is not a symlink to something naughty
		if passwdDest, err = securejoin.SecureJoin(containerMount, etcpasswd); err != nil {
			logrus.Debug(err)
			return nil, err
		}
	}
	if groupDest == "" {
		// Make sure the /etc/group destination is not a symlink to something naughty
		if groupDest, err = securejoin.SecureJoin(containerMount, etcgroup); err != nil {
			logrus.Debug(err)
			return nil, err
		}
	}

	// Check for an override default user
	if override != nil && override.DefaultUser != nil {
		defaultExecUser = override.DefaultUser
	} else {
		// Define a default container user
		// defaultExecUser = &user.ExecUser{
		//	Uid:  0,
		//	Gid:  0,
		//	Home: "/",
		defaultExecUser = nil
	}

	return user.GetExecUserPath(containerUser, defaultExecUser, passwdDest, groupDest)
}

// GetContainerGroups uses securejoin to get a list of numerical groupids from a container. Per the runc
// function it calls: If a group name cannot be found, an error will be returned. If a group id cannot be found,
// or the given group data is nil, the id will be returned as-is  provided it is in the legal range.
func GetContainerGroups(groups []string, containerMount string, override *Overrides) ([]uint32, error) {
	var (
		groupDest string
		err       error
	)

	groupPath := etcgroup
	if override != nil && override.ContainerEtcGroupPath != "" {
		groupPath = override.ContainerEtcGroupPath
	}

	if groupDest, err = securejoin.SecureJoin(containerMount, groupPath); err != nil {
		logrus.Debug(err)
		return nil, err
	}

	gids, err := user.GetAdditionalGroupsPath(groups, groupDest)
	if err != nil {
		return nil, err
	}
	uintgids := make([]uint32, 0, len(gids))
	// For libpod, we want []uint32s
	for _, gid := range gids {
		uintgids = append(uintgids, uint32(gid))
	}
	return uintgids, nil
}

// GetUser takes a containermount path and user name or ID and returns
// a matching User structure from /etc/passwd.  If it cannot locate a user
// with the provided information, an ErrNoPasswdEntries is returned.
// When the provided user name was an ID, a User structure with Uid
// set is returned along with ErrNoPasswdEntries.
func GetUser(containerMount, userIDorName string) (*user.User, error) {
	var inputIsName bool
	uid, err := strconv.Atoi(userIDorName)
	if err != nil {
		inputIsName = true
	}
	passwdDest, err := securejoin.SecureJoin(containerMount, etcpasswd)
	if err != nil {
		return nil, err
	}
	users, err := user.ParsePasswdFileFilter(passwdDest, func(u user.User) bool {
		if inputIsName {
			return u.Name == userIDorName
		}
		return u.Uid == uid
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(users) > 0 {
		return &users[0], nil
	}
	if !inputIsName {
		return &user.User{Uid: uid}, user.ErrNoPasswdEntries
	}
	return nil, user.ErrNoPasswdEntries
}

// GetGroup takes a containermount path and a group name or ID and returns
// a match Group struct from /etc/group.  If it cannot locate a group,
// an ErrNoGroupEntries error is returned.  When the provided group name
// was an ID, a Group structure with Gid set is returned along with
// ErrNoGroupEntries.
func GetGroup(containerMount, groupIDorName string) (*user.Group, error) {
	var inputIsName bool
	gid, err := strconv.Atoi(groupIDorName)
	if err != nil {
		inputIsName = true
	}

	groupDest, err := securejoin.SecureJoin(containerMount, etcgroup)
	if err != nil {
		return nil, err
	}

	groups, err := user.ParseGroupFileFilter(groupDest, func(g user.Group) bool {
		if inputIsName {
			return g.Name == groupIDorName
		}
		return g.Gid == gid
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(groups) > 0 {
		return &groups[0], nil
	}
	if !inputIsName {
		return &user.Group{Gid: gid}, user.ErrNoGroupEntries
	}
	return nil, user.ErrNoGroupEntries
}
