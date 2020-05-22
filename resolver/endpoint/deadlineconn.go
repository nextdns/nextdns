package endpoint

import (
	"net"
	"time"
)

type deadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c deadlineConn) Write(p []byte) (n int, err error) {
	_ = c.SetWriteDeadline(time.Now().Add(c.timeout))
	n, err = c.Conn.Write(p)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			// The write end of the connection is no longer in a known
			// consistent state, unlock the read loop by closing the connection
			// and force a cleanup.
			c.Close()
			return n, err
		}
	}
	_ = c.SetWriteDeadline(time.Time{})
	return n, err
}
