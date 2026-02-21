package netstatus

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

type Change string

func (c Change) Changed() bool {
	return c != ""
}

func (c Change) String() string {
	return string(c)
}

var handlers struct {
	sync.Mutex
	c []chan<- Change
}

var cancel context.CancelFunc
var prevInterfaces []net.Interface

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
	newC := handlers.c[:0]
	for _, ch := range handlers.c {
		if ch != c {
			newC = append(newC, ch)
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
	chans := append([]chan<- Change(nil), handlers.c...)
	handlers.Unlock()
	// Best-effort delivery: a slow or stuck receiver should not block the
	// checker goroutine (or prevent Stop/Notify from making progress).
	for _, ch := range chans {
		select {
		case ch <- c:
		default:
		}
	}
}

func startChecker() {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_, _ = changed() // init
	for {
		select {
		case <-tick.C:
			if c, err := changed(); err == nil && c.Changed() {
				broadcast(c)
			}
		case <-ctx.Done():
			return
		}
	}
}

func changed() (Change, error) {
	newInterfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	c := Change(diff(prevInterfaces, newInterfaces))
	prevInterfaces = newInterfaces
	return c, nil
}

func diff(old, new []net.Interface) string {
	if old == nil || new == nil {
		return ""
	}
	sort.Slice(old, func(i, j int) bool {
		return old[i].Name < old[j].Name
	})
	sort.Slice(new, func(i, j int) bool {
		return new[i].Name < new[j].Name
	})
	l := len(old)
	if l2 := len(new); l2 > l {
		l = l2
	}
	for i := 0; i < l; i++ {
		if len(old) <= i {
			return fmt.Sprintf("%s added", new[i].Name)
		}
		if len(new) <= i {
			return fmt.Sprintf("%s removed", old[i].Name)
		}
		if old[i].Name != new[i].Name {
			if old[i].Name < new[i].Name {
				return fmt.Sprintf("%s removed", old[i].Name)
			}
			return fmt.Sprintf("%s added", new[i].Name)
		}
		if old[i].Flags != new[i].Flags {
			oldUp := old[i].Flags&net.FlagUp != 0
			newUp := new[i].Flags&net.FlagUp != 0
			if oldUp != newUp {
				if oldUp && !newUp {
					return fmt.Sprintf("%s down", new[i].Name)
				}
				return fmt.Sprintf("%s up", new[i].Name)
			}
			return fmt.Sprintf("%s flag %v -> %v", new[i].Name, old[i].Flags, new[i].Flags)
		}
		oldAddrs, _ := old[i].Addrs()
		newAddrs, _ := new[i].Addrs()
		if d := diffAddrs(oldAddrs, newAddrs); d != "" {
			return fmt.Sprintf("%s %s", new[i].Name, d)
		}
	}
	return ""
}

func diffAddrs(oldAddrs, newAddrs []net.Addr) string {
oldIP:
	for _, oip := range oldAddrs {
		for _, nip := range newAddrs {
			if oip.String() == nip.String() {
				continue oldIP
			}
		}
		return fmt.Sprintf("%s removed", oip)
	}
	if len(oldAddrs) != len(newAddrs) {
	newIP:
		for _, nip := range newAddrs {
			for _, oip := range oldAddrs {
				if oip.String() == nip.String() {
					continue newIP
				}
			}
			return fmt.Sprintf("%s added", nip)
		}
	}
	return ""
}
