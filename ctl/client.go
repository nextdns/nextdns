package ctl

import (
	"encoding/json"
	"net"
	"sync"
)

type Client struct {
	c       net.Conn
	mu      sync.Mutex
	replies chan Event
}

func Dial(addr string) (*Client, error) {
	c, err := dial(addr)
	if err != nil {
		return nil, err
	}
	cl := &Client{
		c:       c,
		replies: make(chan Event),
	}
	go cl.readLoop()
	return cl, nil
}

func (c *Client) readLoop() {
	dec := json.NewDecoder(c.c)
	defer c.c.Close()
	for {
		var e Event
		err := dec.Decode(&e)
		if err != nil {
			break
		}
		if e.Reply {
			select {
			case c.replies <- e:
			default:
			}
		}
	}
}

func (c *Client) Send(e Event) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.c.Write(e.Bytes())
	if err != nil {
		return nil, err
	}
	for {
		re := <-c.replies
		if re.Name == e.Name {
			return re.Data, nil
		}
	}
}

func (c *Client) Close() error {
	return c.c.Close()
}
