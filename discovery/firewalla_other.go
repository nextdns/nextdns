//go:build !linux
// +build !linux

package discovery

type Firewalla struct {
}

func (r *Firewalla) Name() string {
	return "firewalla"
}

func (r *Firewalla) Visit(f func(name string, addrs []string)) {
}

func (r *Firewalla) LookupAddr(addr string) []string {
	return nil
}

func (r *Firewalla) LookupHost(name string) []string {
	return nil
}
