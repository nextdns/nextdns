package arp

import (
	"bytes"
	"net"
)

type Table []Entry

type Entry struct {
	IP  net.IP
	MAC net.HardwareAddr
}

func (t Table) SearchMAC(ip net.IP) net.HardwareAddr {
	for i := range t {
		if t[i].IP.Equal(ip) {
			return t[i].MAC
		}
	}
	return nil
}

func (t Table) SearchIP(mac net.HardwareAddr) net.IP {
	for i := range t {
		if bytes.Equal(t[i].MAC, mac) {
			return t[i].IP
		}
	}
	return nil
}
