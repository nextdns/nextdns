// +build !linux

package discovery

type Merlin struct {
}

func (r *Merlin) Name() string {
	return "merlin"
}

func (r *Merlin) Visit(f func(name string, addrs []string)) {
}

func (r *Merlin) LookupAddr(addr string) []string {
	return nil
}

func (r *Merlin) LookupHost(name string) []string {
	return nil
}
