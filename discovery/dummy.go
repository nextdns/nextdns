package discovery

type Dummy struct{}

func (r Dummy) Name() string {
	return "dummy"
}

func (r Dummy) Visit(f func(name string, addrs []string)) {
}

func (r Dummy) LookupAddr(addr string) []string {
	return nil
}

func (r Dummy) LookupHost(name string) []string {
	return nil
}
