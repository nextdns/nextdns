// +build windows

package proxy

import (
	"net"
)

// raises not implemented errors from `golang.org/x/net/ipv4{4,6}` lib
// @TODO: check for implementation at a later time
func setUDPDstOptions(c *net.UDPConn) error { return nil }

func parseDstFromOOB([]byte) net.IP { return nil }
