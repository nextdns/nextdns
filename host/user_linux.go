//go:build linux
// +build linux

package host

import (
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	logindDBusName      = "org.freedesktop.login1"
	logindDBusPath      = "/org/freedesktop/login1"
	logindDBusInterface = "org.freedesktop.login1.Manager"
	logindSessionIface  = "org.freedesktop.login1.Session"
)

const activeUserCacheTTL = 2 * time.Second

var activeUserCache = newRefreshingStringCache(activeUserCacheTTL)

type logindSession struct {
	ID   string
	UID  uint32
	User string
	Seat string
	Path dbus.ObjectPath
}

// ActiveUser returns the active local interactive username on Linux.
// If systemd-logind is unavailable, an empty string is returned.
func ActiveUser() string {
	return activeUserCache.Get(activeUserUncached)
}

func activeUserUncached() string {
	conn, err := dbus.SystemBus()
	if err != nil {
		return ""
	}
	if !logindHasOwner(conn) {
		return ""
	}
	obj := conn.Object(logindDBusName, dbus.ObjectPath(logindDBusPath))
	call := obj.Call(logindDBusInterface+".ListSessions", 0)
	if call.Err != nil {
		return ""
	}
	var sessions []logindSession
	if err := call.Store(&sessions); err != nil {
		return ""
	}
	for _, s := range sessions {
		if s.User == "" || s.Path == "" {
			continue
		}
		sessionObj := conn.Object(logindDBusName, s.Path)
		activeVar, err := sessionObj.GetProperty(logindSessionIface + ".Active")
		if err != nil {
			continue
		}
		active, ok := activeVar.Value().(bool)
		if !ok || !active {
			continue
		}
		remoteVar, err := sessionObj.GetProperty(logindSessionIface + ".Remote")
		if err == nil {
			if remote, ok := remoteVar.Value().(bool); ok && remote {
				continue
			}
		}
		return s.User
	}
	return ""
}

func logindHasOwner(conn *dbus.Conn) bool {
	var hasOwner bool
	call := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, logindDBusName)
	return call.Err == nil && call.Store(&hasOwner) == nil && hasOwner
}
