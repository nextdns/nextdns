package ctl

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestClientSendContext_RoundTrip(t *testing.T) {
	cc, sc := net.Pipe()
	defer sc.Close()

	c := &Client{
		c:       cc,
		replies: make(chan Event, 16),
	}
	go c.readLoop()
	defer c.Close()

	// Server side: decode request and send reply.
	go func() {
		dec := json.NewDecoder(sc)
		var req Event
		_ = dec.Decode(&req)
		re := Event{Name: req.Name, Reply: true, Data: map[string]any{"ok": true}}
		_, _ = sc.Write(re.Bytes())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	v, err := c.SendContext(ctx, Event{Name: "ping"})
	if err != nil {
		t.Fatalf("SendContext error: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("unexpected reply type: %T", v)
	}
	if got, ok := m["ok"].(bool); !ok || !got {
		t.Fatalf("unexpected reply payload: %#v", v)
	}
}

func TestClientSendContext_TimesOut(t *testing.T) {
	cc, sc := net.Pipe()
	defer sc.Close()

	c := &Client{
		c:       cc,
		replies: make(chan Event, 16),
	}
	go c.readLoop()
	defer c.Close()

	// Server side: read request but never reply.
	go func() {
		dec := json.NewDecoder(sc)
		var req Event
		_ = dec.Decode(&req)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.SendContext(ctx, Event{Name: "no-reply"})
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

