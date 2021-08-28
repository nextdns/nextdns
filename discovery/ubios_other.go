// +build !linux

package discovery

type Ubios struct {
}

func (r *Ubios) Name() string {
	return "ubios"
}

func (r *Ubios) Visit(f func(name string, addrs []string)) {
}

func (r *Ubios) LookupAddr(addr string) []string {
	return nil
}

func (r *Ubios) LookupHost(name string) []string {
	return nil
}
