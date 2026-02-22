//go:build !darwin && !windows
// +build !darwin,!windows

package host

// ActiveUser is only supported on macOS and Windows.
func ActiveUser() string {
	return ""
}
