package endpoint

import (
	"context"
	"net"
)

type parallelDialer struct {
	net.Dialer
}

func (d *parallelDialer) DialParallel(ctx context.Context, network string, addrs []string) (net.Conn, error) {
	if len(addrs) == 1 {
		return d.DialContext(ctx, network, addrs[0])
	}
	returned := make(chan struct{})
	defer close(returned)

	type dialResult struct {
		net.Conn
		error
	}
	results := make(chan dialResult)

	racer := func(addr string) {
		c, err := d.DialContext(ctx, network, addr)
		select {
		case results <- dialResult{Conn: c, error: err}:
		case <-returned:
			if c != nil {
				c.Close()
			}
		}
	}

	for _, addr := range addrs {
		go racer(addr)
	}

	var err error
	for res := range results {
		if res.error == nil {
			return res.Conn, nil
		}
		err = res.error
	}
	return nil, err
}
