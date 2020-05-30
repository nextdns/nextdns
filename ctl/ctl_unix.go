//+build !windows

package ctl

import (
	"net"
	"os"
)

func listen(addr string) (net.Listener, error) {
	_ = os.Remove(addr)
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: addr, Net: "unix"})
	if l != nil {
		l.SetUnlinkOnClose(true)
	}
	return l, err
}

func dial(addr string) (net.Conn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
}
