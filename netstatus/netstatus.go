package netstatus

import (
	"context"
	"net"
	"reflect"
	"sync"
	"time"
)

type Change struct{}

var handlers struct {
	sync.Mutex
	c []chan<- Change
}

var cancel context.CancelFunc
var lastInterfaces []net.Interface

// Notify sends a Change to c any time the network interfaces status change.
func Notify(c chan<- Change) {
	handlers.Lock()
	defer handlers.Unlock()
	if handlers.c == nil {
		go startChecker()
	}
	handlers.c = append(handlers.c, c)
}

// Stop unsubscribes from network interfaces change notification.
func Stop(c chan<- Change) {
	handlers.Lock()
	defer handlers.Unlock()
	var newC = make([]chan<- Change, 0, len(handlers.c)-1)
	for _, ch := range handlers.c {
		if ch != c {
			newC = append(newC, c)
		}
	}
	handlers.c = newC
	if len(handlers.c) == 0 && cancel != nil {
		cancel()
		cancel = nil
	}
}

func broadcast(c Change) {
	handlers.Lock()
	defer handlers.Unlock()
	for _, ch := range handlers.c {
		ch <- c
	}
}

func startChecker() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	changed() // init
	for {
		select {
		case <-tick.C:
			if changed() {
				broadcast(Change{})
			}
		case <-ctx.Done():
			break
		}
	}
}

func changed() bool {
	interfaces, _ := net.Interfaces()
	changed := !reflect.DeepEqual(lastInterfaces, interfaces)
	lastInterfaces = interfaces
	return changed
}
