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
	return got[0].Family == want.Family &&
		bytes.Equal(got[0].Address, want.Address) &&
		dnsPortEqual(got[0].Port, want.Port) &&
		got[0].ServerName == want.ServerName
}

// dnsPortEqual treats an unspecified DNSEx port (0) as the default DNS port.
// systemd-resolved reports port 0 when SetLinkDNSEx was given 53; comparing
// against 53 then fails and EnsureDNS reactivates every check interval.
func dnsPortEqual(got, want uint16) bool {
	return normalizeDNSPort(got) == normalizeDNSPort(want)
}

func normalizeDNSPort(port uint16) uint16 {
	if port == 0 {
		return 53
	}
	return port
}
