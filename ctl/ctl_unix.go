//+build !windows

package ctl

import "net"

func listen(addr string) (net.Listener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: addr, Net: "unix"})
}

func dial(addr string) (net.Conn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
}
