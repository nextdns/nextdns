//go:build darwin
// +build darwin

package host

import (
	"os"
	"os/user"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

const activeUserCacheTTL = 2 * time.Second

type activeUserCache struct {
	value     string
	refreshAt int64
}

var activeUserCached atomic.Pointer[activeUserCache]
var activeUserUpdating atomic.Bool

// ActiveUser returns the active console username on macOS.
func ActiveUser() string {
	now := time.Now().UnixNano()
	c := activeUserCached.Load()
	if c != nil && now < c.refreshAt {
		return c.value
	}

	// Ensure only one query refreshes the cache at a time. Others keep using
	// the previous cached value to avoid blocking.
	if activeUserUpdating.CompareAndSwap(false, true) {
		v := activeUserUncached()
		activeUserCached.Store(&activeUserCache{
			value:     v,
			refreshAt: time.Now().Add(activeUserCacheTTL).UnixNano(),
		})
		activeUserUpdating.Store(false)
		return v
	}

	if c != nil {
		return c.value
	}
	return ""
}

func activeUserUncached() string {
	fi, err := os.Stat("/dev/console")
	if err != nil {
		return ""
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
	if err != nil {
		return ""
	}
	if u.Username == "root" {
		return ""
	}
	return u.Username
}
