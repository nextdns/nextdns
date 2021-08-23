// +build linux

package ndp

import (
	"github.com/vishvananda/netlink"
)

func Get() (Table, error) {
	neights, err := netlink.NeighList(0, netlink.FAMILY_V6)
	if err != nil {
		return nil, err
	}

	var t Table
	for _, n := range neights {
		t = append(t, Entry{
			IP:  n.IP,
			MAC: n.HardwareAddr,
		})
	}

	return t, nil
}
