//go:build !darwin && !windows && !linux
// +build !darwin,!windows,!linux

package host

// ActiveUser is only supported on macOS, Windows and Linux with systemd-logind.
func ActiveUser() string {
	return ""
}
