package systemd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/coreos/go-systemd/v22/dbus"
	godbus "github.com/godbus/dbus/v5"
	"github.com/sirupsen/logrus"
)

// IsSystemdSessionValid checks if sessions is valid for provided rootless uid.
func IsSystemdSessionValid(uid int) bool {
	var conn *godbus.Conn
	var err error
	var object godbus.BusObject
	var seat0Path godbus.ObjectPath
	dbusDest := "org.freedesktop.login1"
	dbusInterface := "org.freedesktop.login1.Manager"
	dbusPath := "/org/freedesktop/login1"

	if rootless.IsRootless() {
		conn, err = GetLogindConnection(rootless.GetRootlessUID())
		if err != nil {
			// unable to fetch systemd object for logind
			logrus.Debugf("systemd-logind: %s", err)
			return false
		}
		object = conn.Object(dbusDest, godbus.ObjectPath(dbusPath))
		if err := object.Call(dbusInterface+".GetSeat", 0, "seat0").Store(&seat0Path); err != nil {
			// unable to get seat0 path.
			logrus.Debugf("systemd-logind: %s", err)
			return false
		}
		seat0Obj := conn.Object(dbusDest, seat0Path)
		activeSession, err := seat0Obj.GetProperty(dbusDest + ".Seat.ActiveSession")
		if err != nil {
			// unable to get active sessions.
			logrus.Debugf("systemd-logind: %s", err)
			return false
		}
		activeSessionMap, ok := activeSession.Value().([]any)
		if !ok || len(activeSessionMap) < 2 {
			// unable to get active session map.
			logrus.Debugf("systemd-logind: %s", err)
			return false
		}
		activeSessionPath, ok := activeSessionMap[1].(godbus.ObjectPath)
		if !ok {
			// unable to fetch active session path.
			logrus.Debugf("systemd-logind: %s", err)
			return false
		}
		activeSessionObj := conn.Object(dbusDest, activeSessionPath)
		sessionUser, err := activeSessionObj.GetProperty(dbusDest + ".Session.User")
		if err != nil {
			// unable to fetch session user from activeSession path.
			logrus.Debugf("systemd-logind: %s", err)
			return false
		}
		dbusUser, ok := sessionUser.Value().([]any)
		if !ok {
			// not a valid user.
			return false
		}
		if len(dbusUser) < 2 {
			// not a valid session user.
			return false
		}
		activeUID, ok := dbusUser[0].(uint32)
		if !ok {
			return false
		}
		// active session found which belongs to following rootless user
		if activeUID == uint32(uid) {
			return true
		}
		return false
	}
	return true
}

// GetDbusConnection returns a user connection to D-BUS
func GetLogindConnection(uid int) (*godbus.Conn, error) {
	return dbusAuthConnectionLogind(uid)
}

func dbusAuthConnectionLogind(uid int) (*godbus.Conn, error) {
	var conn *godbus.Conn
	var err error
	conn, err = godbus.SystemBusPrivate()
	if err != nil {
		return nil, err
	}
	methods := []godbus.Auth{godbus.AuthExternal(strconv.Itoa(uid))}
	if err = conn.Auth(methods); err != nil {
		conn.Close()
		return nil, err
	}
	err = conn.Hello()
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func dbusAuthRootlessConnection(createBus func(opts ...godbus.ConnOption) (*godbus.Conn, error)) (*godbus.Conn, error) {
	conn, err := createBus()
	if err != nil {
		return nil, err
	}

	methods := []godbus.Auth{godbus.AuthExternal(strconv.Itoa(rootless.GetRootlessUID()))}

	err = conn.Auth(methods)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func newRootlessConnection() (*dbus.Conn, error) {
	return dbus.NewConnection(func() (*godbus.Conn, error) {
		return dbusAuthRootlessConnection(func(_ ...godbus.ConnOption) (*godbus.Conn, error) {
			path := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "systemd", "private")
			path, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, err
			}
			return godbus.Dial(fmt.Sprintf("unix:path=%s", path))
		})
	})
}

// ConnectToDBUS returns a DBUS connection.  It works both as root and non-root
// users.
func ConnectToDBUS() (*dbus.Conn, error) {
	if rootless.IsRootless() {
		return newRootlessConnection()
	}
	return dbus.NewSystemdConnectionContext(context.Background())
}
