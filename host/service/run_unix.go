// +build !windows

package service

func runService(name string, r Runner) error {
	return runForeground(r)
}
