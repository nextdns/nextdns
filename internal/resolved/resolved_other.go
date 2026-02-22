//go:build !linux
// +build !linux

package resolved

import "net"

type StubConfig struct {
	Enabled bool
	Addrs   []net.IP
}

func Available() bool {
	return false
}

func SetDNS(ip string, port uint16) error {
	_ = ip
	_ = port
	return nil
}

func ResetDNS() error {
	return nil
}

func StateExists() bool {
	return false
}

func Stub() (StubConfig, error) {
	return StubConfig{}, nil
}
