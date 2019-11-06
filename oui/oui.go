package oui

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"regexp"
)

type OUI map[uint32]string

func Load(ctx context.Context, url string) (OUI, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return Read(res.Body)
}

func Read(r io.Reader) (OUI, error) {
	s := bufio.NewScanner(r)
	re := regexp.MustCompile(`([0-9A-F]{2}-[0-9A-F]{2}-[0-9A-F]{2})\s+\(hex\)\s+(.*)`)
	oui := OUI{}
	for s.Scan() {
		if m := re.FindStringSubmatch(s.Text()); m != nil {
			mac, _ := net.ParseMAC(m[1] + "-00-00-00-00-00")
			key := genKey(mac)
			if key == 0 {
				continue
			}
			oui[key] = m[2]
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return oui, nil
}

func (o OUI) Lookup(mac net.HardwareAddr) string {
	return o[genKey(mac)]
}

func genKey(mac net.HardwareAddr) uint32 {
	if len(mac) < 3 {
		return 0
	}
	return uint32(mac[0])<<16 + uint32(mac[1])<<8 + uint32(mac[2])
}
