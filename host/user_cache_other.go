//go:build !darwin && !windows
// +build !darwin,!windows

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
