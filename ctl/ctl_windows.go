package ctl

import (
	"net"

	"github.com/Microsoft/go-winio"
)

func listen(addr string) (net.Listener, error) {
	return winio.ListenPipe(`\\.\pipe\`+addr, &winio.PipeConfig{
		SecurityDescriptor: "O:SYD:P(A;;GA;;;WD)",
	})
}

func dial(addr string) (net.Conn, error) {
	return winio.DialPipe(`\\.\pipe\`+addr, nil)
}
