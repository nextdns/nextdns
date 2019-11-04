// +build windows

package main

import "errors"

func activate(string) error {
	return errors.New("activate: not supported")
}

func deactivate(string) error {
	return errors.New("deactivate: not supported")
}
