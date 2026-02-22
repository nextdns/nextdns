//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows
// +build !darwin,!linux,!freebsd,!openbsd,!netbsd,!dragonfly,!windows

package host

func Model() string {
	return ""
}
