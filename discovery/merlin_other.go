// +build !linux

package discovery

type Merlin struct {
}

func (r Merlin) Lookup(addr string) (string, bool) {
	return "", false
}
