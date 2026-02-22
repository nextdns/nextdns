//go:build darwin
// +build darwin

package host

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"
)

const activeUserCacheTTL = 2 * time.Second

var activeUserCache = newRefreshingStringCache(activeUserCacheTTL)

// ActiveUser returns the active console username on macOS.
func ActiveUser() string {
	return activeUserCache.Get(activeUserUncached)
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
