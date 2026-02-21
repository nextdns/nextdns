package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"
)

type Client struct {
	c       net.Conn
	mu      sync.Mutex
	replies chan Event
}

var errClientClosed = errors.New("client closed")

func Dial(addr string) (*Client, error) {
	c, err := dial(addr)
	if err != nil {
		return nil, err
	}
	cl := &Client{
		c:       c,
		replies: make(chan Event, 16),
	}
	go cl.readLoop()
	return cl, nil
}

func (c *Client) readLoop() {
	dec := json.NewDecoder(c.c)
	defer func() {
		_ = c.c.Close()
		close(c.replies)
	}()
	for {
		var e Event
		err := dec.Decode(&e)
		if err != nil {
			break
		}
		if e.Reply {
			// Never drop replies. If caller is slow and channel is full, this will
			// block, providing backpressure instead of hanging Send forever.
			c.replies <- e
		}
	}
}

func (c *Client) Send(e Event) (any, error) {
	// Keep legacy signature but ensure it cannot hang forever.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.SendContext(ctx, e)
}

func (c *Client) SendContext(ctx context.Context, e Event) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.c.Write(e.Bytes())
	if err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case re, ok := <-c.replies:
			if !ok {
				return nil, errClientClosed
			}
			if re.Name == e.Name {
				return re.Data, nil
			}
		}
	}
}

func (c *Client) Close() error {
	return c.c.Close()
}
