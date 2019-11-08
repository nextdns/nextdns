package arp

import (
	"net"
	"sync/atomic"
	"time"
)

type cache struct {
	lastUpdate int64
	table      atomic.Value
}

func (c *cache) get() Table {
	now := time.Now().UTC().Unix()
	last := atomic.LoadInt64(&c.lastUpdate)
	if now-last > 30 && atomic.SwapInt64(&c.lastUpdate, now) == last {
		go func() {
			t, _ := Get()
			c.table.Store(t)
		}()
	}
	t, _ := c.table.Load().(Table)
	return t
}

var global = &cache{}

func SearchMAC(ip net.IP) net.HardwareAddr {
	return global.get().SearchMAC(ip)
}

func SearchIP(mac net.HardwareAddr) net.IP {
	return global.get().SearchIP(mac)
}
