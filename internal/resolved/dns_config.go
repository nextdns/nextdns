package resolved

import "bytes"

type dnsServer struct {
	Family     int32
	Address    []byte
	Port       uint16
	ServerName string
}

func dnsConfigMatches(got []dnsServer, want dnsServer) bool {
	if len(got) != 1 {
		return false
	}
	port := got[0].Port
	// systemd-resolved reports an unspecified port as 0 in DNSEx.
	if port == 0 {
		port = 53
	}
	return got[0].Family == want.Family &&
		bytes.Equal(got[0].Address, want.Address) &&
		port == want.Port &&
		got[0].ServerName == want.ServerName
}
