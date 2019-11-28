package netstatus

import (
	"bytes"
	"context"
	"net"
	"sync"
	"time"
)

type Change struct{}

var handlers struct {
	sync.Mutex
	c []chan<- Change
}

var cancel context.CancelFunc
var lastInterfacesSum []byte

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
	tick := time.NewTicker(10 * time.Second)
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

func getInterfacesSum() []byte {
	var sum []byte
	interfaces, err := net.Interfaces()
	if err != nil {
		return []byte(err.Error())
	}
	for _, in := range interfaces {
		sum = append(sum, byte(in.Flags))
		sum = append(sum, in.Name...)
		sum = append(sum, in.HardwareAddr...)
		addrs, err := in.Addrs()
		if err != nil {
			sum = append(sum, err.Error()...)
			continue
		}
		for _, addr := range addrs {
			sum = append(sum, addr.String()...)
		}
	}
	return sum
}

func changed() bool {
	interfacesSum := getInterfacesSum()
	changed := !bytes.Equal(lastInterfacesSum, interfacesSum)
	lastInterfacesSum = interfacesSum
	return changed
}
