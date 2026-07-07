package endpoint

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// TestDOHEndpoint_ConcurrentRoundTripAndClose exercises the lazy transport init
// in RoundTrip racing closeTransport's read of the same field. Query reads of the
// active endpoint are lock-free, so an endpoint swap's closeTransport(prev) can
// run concurrently with a first-use RoundTrip(prev). Must be clean under -race.
func TestDOHEndpoint_ConcurrentRoundTripAndClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // fail requests immediately; we only care about the transport field access
	for i := 0; i < 200; i++ {
		e := &DOHEndpoint{Hostname: "example.invalid"}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, "POST", "https://example.invalid", http.NoBody)
			_, _ = e.RoundTrip(req)
		}()
		go func() {
			defer wg.Done()
			e.closeTransport()
		}()
		wg.Wait()
	}
}
