//go:build !darwin && !windows && !linux
// +build !darwin,!windows,!linux

package host

import "time"

type refreshingStringCache struct{}

func newRefreshingStringCache(time.Duration) refreshingStringCache {
	return refreshingStringCache{}
}

func (*refreshingStringCache) Get(load func() string) string {
	return load()
}

var _ = newRefreshingStringCache(0)
