//go:build linux
// +build linux

package resolved

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
)

const (
	dbusName      = "org.freedesktop.resolve1"
	dbusPath      = "/org/freedesktop/resolve1"
	dbusInterface = "org.freedesktop.resolve1.Manager"
	stateFile     = "/var/run/nextdns-resolved-state.json"
)

var errUnavailable = errors.New("systemd-resolved D-Bus API unavailable")

type state struct {
	Mode  string `json:"mode"`
	Links []int  `json:"links"`
}

type dns struct {
	Family  int32
	Address []byte
}

type dnsEx struct {
	Family     int32
	Address    []byte
	Port       uint16
	ServerName string
}

// StubConfig describes the active systemd-resolved DNS stub listener.
type StubConfig struct {
	Enabled bool
	Addrs   []net.IP
}

func Available() bool {
	conn, err := dbus.SystemBus()
	if err != nil {
		return false
	}
	return hasOwner(conn)
}

func hasOwner(conn *dbus.Conn) bool {
	var hasOwner bool
	call := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, dbusName)
	return call.Err == nil && call.Store(&hasOwner) == nil && hasOwner
}

func SetDNS(ip string, port uint16) error {
	if port == 0 {
		port = 53
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	if !hasOwner(conn) {
		return errUnavailable
	}
	links, err := activeLinks()
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return errors.New("systemd-resolved: no active non-loopback network link found")
	}
	var applied []int
	for _, link := range links {
		if err := setLinkDNS(conn, link, ip, port); err != nil {
			_ = revertLinks(conn, applied)
			return err
		}
		applied = append(applied, link)
	}
	s := state{
		Mode:  "resolved-dbus",
		Links: applied,
	}
	if err := writeState(s); err != nil {
		_ = revertLinks(conn, applied)
		return err
	}
	_ = flushCaches(conn)
	return nil
}

func ResetDNS() error {
	s, err := readState()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	var firstErr error
	for _, link := range s.Links {
		call := conn.Object(dbusName, dbus.ObjectPath(dbusPath)).
			Call(dbusInterface+".RevertLink", 0, int32(link))
		if call.Err != nil && firstErr == nil {
			firstErr = fmt.Errorf("systemd-resolved: revert link %d: %w", link, call.Err)
		}
	}
	_ = flushCaches(conn)
	if firstErr != nil {
		return firstErr
	}
	return os.Remove(stateFile)
}

func StateExists() bool {
	_, err := os.Stat(stateFile)
	return err == nil
}

func Stub() (StubConfig, error) {
	var cfg StubConfig
	conn, err := dbus.SystemBus()
	if err != nil {
		return cfg, err
	}
	if !hasOwner(conn) {
		return cfg, errUnavailable
	}
	obj := conn.Object(dbusName, dbus.ObjectPath(dbusPath))
	modeVar, err := obj.GetProperty(dbusInterface + ".DNSStubListener")
	if err != nil {
		return cfg, err
	}
	mode, ok := modeVar.Value().(string)
	if !ok {
		return cfg, errors.New("systemd-resolved: unexpected DNSStubListener type")
	}
	cfg.Enabled = mode != "no"
	if !cfg.Enabled {
		return cfg, nil
	}
	// Default stub address when enabled.
	cfg.Addrs = append(cfg.Addrs, net.ParseIP("127.0.0.53"))
	if extraVar, err := obj.GetProperty(dbusInterface + ".DNSStubListenerExtra"); err == nil {
		cfg.Addrs = append(cfg.Addrs, parseStubListenerExtra(extraVar.Value())...)
	}
	return cfg, nil
}

func parseStubListenerExtra(v any) (ips []net.IP) {
	list, ok := v.([]string)
	if !ok {
		return nil
	}
	for _, addr := range list {
		ip := parseStubAddr(addr)
		if ip == nil {
			continue
		}
		ips = append(ips, ip)
	}
	return ips
}

func parseStubAddr(addr string) net.IP {
	// Handles "1.2.3.4", "1.2.3.4:53", "[::1]:53", "udp:1.2.3.4:53", etc.
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "udp:")
	addr = strings.TrimPrefix(addr, "tcp:")
	addr = strings.TrimPrefix(addr, "tls:")
	addr = strings.TrimPrefix(addr, "https:")
	if h, _, err := net.SplitHostPort(addr); err == nil {
		addr = h
	}
	addr = strings.TrimPrefix(addr, "[")
	addr = strings.TrimSuffix(addr, "]")
	return net.ParseIP(addr)
}

func setLinkDNS(conn *dbus.Conn, link int, ip string, port uint16) error {
	family, addr, err := parseIP(ip)
	if err != nil {
		return err
	}
	obj := conn.Object(dbusName, dbus.ObjectPath(dbusPath))
	exCall := obj.Call(
		dbusInterface+".SetLinkDNSEx",
		0,
		int32(link),
		[]dnsEx{{
			Family:     family,
			Address:    addr,
			Port:       port,
			ServerName: "",
		}},
	)
	if exCall.Err == nil {
		return nil
	}
	if port != 53 {
		return fmt.Errorf("systemd-resolved: non 53 port requires SetLinkDNSEx support: %w", exCall.Err)
	}
	call := obj.Call(
		dbusInterface+".SetLinkDNS",
		0,
		int32(link),
		[]dns{{
			Family:  family,
			Address: addr,
		}},
	)
	if call.Err != nil {
		return fmt.Errorf("systemd-resolved: set link dns %d: %w", link, call.Err)
	}
	return nil
}

func revertLinks(conn *dbus.Conn, links []int) error {
	var firstErr error
	obj := conn.Object(dbusName, dbus.ObjectPath(dbusPath))
	for _, link := range links {
		call := obj.Call(dbusInterface+".RevertLink", 0, int32(link))
		if call.Err != nil && firstErr == nil {
			firstErr = fmt.Errorf("systemd-resolved: revert link %d: %w", link, call.Err)
		}
	}
	return firstErr
}

func flushCaches(conn *dbus.Conn) error {
	call := conn.Object(dbusName, dbus.ObjectPath(dbusPath)).
		Call(dbusInterface+".FlushCaches", 0)
	return call.Err
}

func parseIP(ip string) (family int32, addr []byte, err error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return 0, nil, fmt.Errorf("systemd-resolved: invalid ip %q", ip)
	}
	if v4 := parsed.To4(); v4 != nil {
		return syscall.AF_INET, []byte(v4), nil
	}
	v6 := parsed.To16()
	if v6 == nil {
		return 0, nil, fmt.Errorf("systemd-resolved: invalid ip %q", ip)
	}
	return syscall.AF_INET6, []byte(v6), nil
}

func activeLinks() ([]int, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	links := make([]int, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Index <= 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		links = append(links, iface.Index)
	}
	slices.Sort(links)
	return links, nil
}

func writeState(s state) error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := stateFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, stateFile)
}

func readState() (state, error) {
	var s state
	b, err := os.ReadFile(stateFile)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, nil
}
