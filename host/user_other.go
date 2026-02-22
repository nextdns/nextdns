//go:build !darwin
// +build !darwin

package host

// ActiveUser is only supported on macOS.
func ActiveUser() string {
	return ""
}
