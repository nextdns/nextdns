// +build !windows

package service

func runService(r Runner) error {
	return runForeground(r)
}
