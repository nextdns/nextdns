// +build windows

package main

import "errors"

func activate() error {
	return errors.New("activate: not supported")
}

func deactivate() error {
	return errors.New("activate: not supported")
}
