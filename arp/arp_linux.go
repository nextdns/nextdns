// +build linux

package arp

import (
	"bufio"
	"net"
	"os"
	"strings"
)

const (
	fieldIPAddr int = iota
	fieldHWType
	fieldFlags
	fieldHWAddr
	fieldMask
	fieldDevice
)

func Get() (Table, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	s.Scan() // skip the field descriptions

	var t Table

	for s.Scan() {
		fields := strings.Fields(s.Text())
		t = append(t, Entry{
			IP:  net.ParseIP(fields[fieldIPAddr]),
			MAC: parseMAC(fields[fieldHWAddr]),
		})
	}

	return t, nil
}
