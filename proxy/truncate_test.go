package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
	"golang.org/x/net/dns/dnsmessage"
)

func truncationMessage(answers int) dnsmessage.Message {
	name := dnsmessage.MustNewName("example.com.")
	m := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 42, Response: true, RecursionDesired: true, RecursionAvailable: true, AuthenticData: true, CheckingDisabled: true},
		Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET}},
	}
	for i := 0; i < answers; i++ {
		m.Answers = append(m.Answers, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET, TTL: 60},
			Body:   &dnsmessage.TXTResource{TXT: []string{strings.Repeat("x", 200)}},
		})
	}
	return m
}

func truncationOPT(t testing.TB, options ...dnsmessage.Option) dnsmessage.Resource {
	t.Helper()
	var h dnsmessage.ResourceHeader
	if err := h.SetEDNS0(4094, dnsmessage.RCodeSuccess, true); err != nil {
		t.Fatal(err)
	}
	return dnsmessage.Resource{Header: h, Body: &dnsmessage.OPTResource{Options: options}}
}

func truncationQuery(t testing.TB, size int) query.Query {
	t.Helper()
	m := truncationMessage(0)
	m.Header = dnsmessage.Header{ID: 42, RecursionDesired: true, CheckingDisabled: true}
	if size >= 0 {
		r := truncationOPT(t)
		r.Header.Class = dnsmessage.Class(size)
		m.Additionals = []dnsmessage.Resource{r}
	}
	q, err := query.New(packTruncationMessage(t, m), net.IPv4(127, 0, 0, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func packTruncationMessage(t testing.TB, m dnsmessage.Message) []byte {
	t.Helper()
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func checkTruncation(t testing.TB, original []byte, size int) (dnsmessage.Message, []byte) {
	t.Helper()
	q := truncationQuery(t, size)
	limit := min(max(512, int(q.MsgSize)), maxDNS0Size)
	buf := make([]byte, maxTCPSize)
	copy(buf, original)
	n := truncateResponse(buf, len(original), limit, q)
	if n < 12 || n > limit {
		t.Fatalf("response length = %d, want 12..%d", n, limit)
	}
	var m dnsmessage.Message
	if err := m.Unpack(buf[:n]); err != nil {
		t.Fatalf("malformed response: %v", err)
	}
	if m.ID != q.ID || !m.Response || len(m.Questions) != 1 || m.Questions[0].Name.String() != q.Name {
		t.Fatalf("unexpected response header/question: %+v", m)
	}
	optCount := 0
	for _, rr := range m.Additionals {
		if rr.Header.Type == dnsmessage.TypeOPT {
			optCount++
		}
	}
	if (size >= 0 && optCount != 1) || (size < 0 && optCount != 0) {
		t.Fatalf("client EDNS size %d: got %d OPT records", size, optCount)
	}
	return m, buf[:n]
}

func TestTruncateResponseRequiredAnswer(t *testing.T) {
	for _, size := range []int{-1, 0, 1, 512, 1232, 4096, 8192} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			m := truncationMessage(30)
			m.Additionals = []dnsmessage.Resource{truncationOPT(t)}
			got, _ := checkTruncation(t, packTruncationMessage(t, m), size)
			if !got.Truncated || len(got.Answers) != 0 || len(got.Authorities) != 0 {
				t.Fatalf("expected minimal truncated response: %+v", got)
			}
			if !got.RecursionDesired || !got.RecursionAvailable || !got.AuthenticData || !got.CheckingDisabled {
				t.Fatalf("lost header flags: %+v", got.Header)
			}
		})
	}
}

func TestTruncateResponseEDNSOptions(t *testing.T) {
	cookie := dnsmessage.Option{Code: 10, Data: bytes.Repeat([]byte{1}, 24)}
	ecs := dnsmessage.Option{Code: 8, Data: []byte{0, 1, 24, 24, 192, 0, 2}}
	for _, code := range []uint16{12, 15, 65001} {
		m := truncationMessage(10)
		data := bytes.Repeat([]byte{'x'}, 1400)
		data[0], data[1] = 0, 0
		r := truncationOPT(t, dnsmessage.Option{Code: code, Data: data}, cookie, ecs)
		// Extended RCODEs live in OPT, not just the ordinary DNS header.
		if err := r.Header.SetEDNS0(4094, dnsmessage.RCode(17), true); err != nil {
			t.Fatal(err)
		}
		m.RCode = 1
		m.Additionals = []dnsmessage.Resource{r}
		got, _ := checkTruncation(t, packTruncationMessage(t, m), 512)
		if !got.Truncated || got.Additionals[0].Header.ExtendedRCode(got.RCode) != 17 || !got.Additionals[0].Header.DNSSECAllowed() {
			t.Fatalf("option %d: lost truncation/EDNS metadata: %+v", code, got)
		}
		opts := got.Additionals[0].Body.(*dnsmessage.OPTResource).Options
		for _, wanted := range []dnsmessage.Option{cookie, ecs} {
			found := false
			for _, o := range opts {
				found = found || (o.Code == wanted.Code && bytes.Equal(o.Data, wanted.Data))
			}
			if !found {
				t.Fatalf("option %d: lost required option %d: %+v", code, wanted.Code, opts)
			}
		}
	}
}

func TestTruncateResponseOptionalAdditionals(t *testing.T) {
	for _, upstreamEDNS := range []bool{false, true} {
		m := truncationMessage(1)
		for i := 0; i < 40; i++ {
			m.Additionals = append(m.Additionals, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Class: dnsmessage.ClassINET},
				Body:   &dnsmessage.AResource{A: [4]byte{192, 0, 2, byte(i)}},
			})
		}
		if upstreamEDNS {
			// OPT may appear before other additional records.
			m.Additionals = append([]dnsmessage.Resource{truncationOPT(t)}, m.Additionals...)
		}
		for _, clientSize := range []int{-1, 512} {
			got, _ := checkTruncation(t, packTruncationMessage(t, m), clientSize)
			if got.Truncated || len(got.Answers) != 1 || got.Answers[0].Body.(*dnsmessage.TXTResource).TXT[0] != strings.Repeat("x", 200) {
				t.Fatalf("discarded fitting answer/forced TCP: %+v", got)
			}
		}
	}
}

func TestTruncateResponseKeepsOriginalTC(t *testing.T) {
	m := truncationMessage(1)
	m.Truncated = true
	m.Additionals = []dnsmessage.Resource{truncationOPT(t, dnsmessage.Option{Code: 12, Data: make([]byte, 700)})}
	got, _ := checkTruncation(t, packTruncationMessage(t, m), 512)
	if !got.Truncated {
		t.Fatal("cleared upstream TC when removing padding")
	}
}

func TestTruncateResponseCookieTakesPriority(t *testing.T) {
	m := truncationMessage(2)
	for _, rr := range m.Answers {
		rr.Body.(*dnsmessage.TXTResource).TXT[0] = strings.Repeat("x", 220)
	}
	cookie := dnsmessage.Option{Code: 10, Data: bytes.Repeat([]byte{1}, 40)}
	m.Additionals = []dnsmessage.Resource{truncationOPT(t, cookie)}
	got, _ := checkTruncation(t, packTruncationMessage(t, m), 512)
	if !got.Truncated {
		t.Fatal("lost data without TC")
	}
	opts := got.Additionals[0].Body.(*dnsmessage.OPTResource).Options
	if len(opts) != 1 || opts[0].Code != 10 || !bytes.Equal(opts[0].Data, cookie.Data) {
		t.Fatal("discarded COOKIE to retain answer records")
	}
}

func TestTruncateResponseKeepsNegativeAuthority(t *testing.T) {
	m := truncationMessage(1)
	m.RCode = dnsmessage.RCodeNameError
	m.Authorities = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.SOAResource{NS: dnsmessage.MustNewName("ns.example.com."), MBox: dnsmessage.MustNewName("hostmaster.example.com."), Serial: 1, Refresh: 60, Retry: 60, Expire: 3600, MinTTL: 60}}}
	m.Additionals = []dnsmessage.Resource{truncationOPT(t, dnsmessage.Option{Code: 12, Data: make([]byte, 700)})}
	got, _ := checkTruncation(t, packTruncationMessage(t, m), 512)
	if got.Truncated || len(got.Answers) != 1 || len(got.Authorities) != 1 || got.Authorities[0].Header.Type != dnsmessage.TypeSOA || got.RCode != dnsmessage.RCodeNameError {
		t.Fatalf("lost negative-answer authority: %+v", got)
	}
}

func TestTruncateResponsePreservesWireAnswers(t *testing.T) {
	m := truncationMessage(0)
	// MD is a legacy type whose RDATA may use compression, but dnsmessage
	// treats it as unknown. Repacking the complete message would move it.
	m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Class: dnsmessage.ClassINET}, Body: &dnsmessage.UnknownResource{Type: 3, Data: []byte{0xc0, 0x0c}}}}
	m.Additionals = []dnsmessage.Resource{truncationOPT(t, dnsmessage.Option{Code: 12, Data: make([]byte, 700)})}
	wire := packTruncationMessage(t, m)
	got, truncated := checkTruncation(t, wire, 512)
	if got.Truncated || len(got.Answers) != 1 || !bytes.Equal(got.Answers[0].Body.(*dnsmessage.UnknownResource).Data, []byte{0xc0, 0x0c}) {
		t.Fatalf("lost valid opaque answer: %+v", got)
	}
	// The original question/answer wire prefix must not move.
	m.Additionals = nil
	prefix := packTruncationMessage(t, m)
	if !bytes.Equal(prefix[12:], truncated[12:len(prefix)]) {
		t.Fatal("question/answer wire encoding changed")
	}
}

func TestTruncateResponseMalformedUpstream(t *testing.T) {
	m := truncationMessage(10)
	m.Additionals = []dnsmessage.Resource{truncationOPT(t)}
	wire := packTruncationMessage(t, m)
	for _, bad := range [][]byte{wire[:len(wire)-1], append(append([]byte(nil), wire...), 1)} {
		got, _ := checkTruncation(t, bad, 512)
		if got.RCode != dnsmessage.RCodeServerFailure || got.Truncated {
			t.Fatalf("malformed upstream did not produce SERVFAIL: %+v", got)
		}
	}
}

func TestTruncateResponseReferralGlue(t *testing.T) {
	for _, addresses := range []int{1, 40} {
		m := truncationMessage(0)
		m.RecursionAvailable = false
		zone := dnsmessage.MustNewName("example.com.")
		ns := dnsmessage.MustNewName("ns.example.com.")
		m.Authorities = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: zone, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.NSResource{NS: ns}}}
		for i := 0; i < addresses; i++ {
			m.Additionals = append(m.Additionals, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: ns, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, byte(i)}}})
		}
		m.Additionals = append(m.Additionals, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("optional.example.net."), Class: dnsmessage.ClassINET}, Body: &dnsmessage.TXTResource{TXT: []string{strings.Repeat("z", 200), strings.Repeat("z", 200), strings.Repeat("z", 200)}}}, truncationOPT(t))
		got, _ := checkTruncation(t, packTruncationMessage(t, m), 512)
		if addresses == 40 {
			if !got.Truncated {
				t.Fatal("missing in-domain glue did not set TC")
			}
		} else {
			if got.Truncated || len(got.Authorities) != 1 {
				t.Fatalf("fitting referral was discarded: %+v", got)
			}
			found := false
			for _, rr := range got.Additionals {
				found = found || (rr.Header.Type == dnsmessage.TypeA && rr.Header.Name.String() == ns.String())
			}
			if !found {
				t.Fatal("lost fitting in-domain glue")
			}
		}
	}
}

func TestTruncateResponseAliasReferral(t *testing.T) {
	for _, tc := range []struct {
		name     string
		qtype    dnsmessage.Type
		alias    bool
		terminal bool
		dname    bool
		referral bool
	}{
		{name: "cname referral", qtype: dnsmessage.TypeA, alias: true, referral: true},
		{name: "complete A", qtype: dnsmessage.TypeA, terminal: true},
		{name: "CNAME query", qtype: dnsmessage.TypeCNAME, alias: true},
		{name: "ANY query", qtype: dnsmessage.TypeALL, alias: true},
		{name: "complete alias chain", qtype: dnsmessage.TypeA, alias: true, terminal: true},
		{name: "DNAME referral", qtype: dnsmessage.TypeA, alias: true, dname: true, referral: true},
		{name: "DNAME query referral", qtype: 39, alias: true, dname: true, referral: true},
		{name: "complete DNAME chain", qtype: 39, alias: true, dname: true, terminal: true},
	} {
		for _, addresses := range []int{1, 40} {
			t.Run(tc.name+"/"+strconv.Itoa(addresses), func(t *testing.T) {
				m := truncationMessage(0)
				m.Questions[0].Type = tc.qtype
				name := m.Questions[0].Name
				if tc.alias {
					name = dnsmessage.MustNewName("example.test.")
					m.Answers = append(m.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Class: dnsmessage.ClassINET}, Body: &dnsmessage.CNAMEResource{CNAME: name}})
				}
				if tc.dname {
					m.Answers = append(m.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("com."), Class: dnsmessage.ClassINET}, Body: &dnsmessage.UnknownResource{Type: 39, Data: []byte{4, 't', 'e', 's', 't', 0}}})
				}
				if tc.terminal {
					var body dnsmessage.ResourceBody = &dnsmessage.AResource{A: [4]byte{192, 0, 2, 1}}
					if tc.qtype == 39 {
						body = &dnsmessage.UnknownResource{Type: 39, Data: []byte{3, 'n', 'e', 't', 0}}
					}
					m.Answers = append(m.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET}, Body: body})
				}
				zone := dnsmessage.MustNewName("test.")
				if !tc.alias {
					zone = dnsmessage.MustNewName("com.")
				}
				ns := dnsmessage.MustNewName("ns." + zone.String())
				m.Authorities = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: zone, Class: dnsmessage.ClassINET}, Body: &dnsmessage.NSResource{NS: ns}}}
				for i := 0; i < addresses; i++ {
					m.Additionals = append(m.Additionals, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: ns, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, byte(i)}}})
				}
				m.Additionals = append(m.Additionals, truncationOPT(t, dnsmessage.Option{Code: 12, Data: make([]byte, 700)}))
				got, _ := checkTruncation(t, packTruncationMessage(t, m), 512)
				if wantTC := tc.referral && addresses == 40; got.Truncated != wantTC {
					t.Fatalf("TC=%v, want %v", got.Truncated, wantTC)
				}
				if tc.referral && addresses == 1 && (len(got.Additionals) != 2 || got.Additionals[0].Header.Type != dnsmessage.TypeA) {
					t.Fatal("lost fitting glue after aliases")
				}
				if !got.Truncated && len(got.Answers) != len(m.Answers) {
					t.Fatal("lost complete answer")
				}
			})
		}
	}
}

func TestTruncateResponseUnchangedAtLimit(t *testing.T) {
	m := truncationMessage(3)
	m.Additionals = []dnsmessage.Resource{truncationOPT(t)}
	wire := packTruncationMessage(t, m)
	for _, limit := range []int{len(wire), len(wire) + 1} {
		buf := append([]byte(nil), wire...)
		q := truncationQuery(t, limit)
		n := truncateResponse(buf, len(buf), limit, q)
		if n != len(wire) || !bytes.Equal(buf[:n], wire) {
			t.Fatal("changed a reply within the UDP limit")
		}
	}
}

func TestTruncateResponseRelocatesGlue(t *testing.T) {
	m := truncationMessage(0)
	m.Authorities = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Class: dnsmessage.ClassINET}, Body: &dnsmessage.NSResource{NS: dnsmessage.MustNewName("NS.example.com.")}}}
	name := dnsmessage.MustNewName("ns.example.com.")
	// The A owner's compression pointer refers to this optional TXT owner,
	// rather than the differently cased NS name in the authority section.
	m.Additionals = []dnsmessage.Resource{
		{Header: dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET}, Body: &dnsmessage.TXTResource{TXT: []string{strings.Repeat("z", 200), strings.Repeat("z", 200), strings.Repeat("z", 200)}}},
		{Header: dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 1}}},
		truncationOPT(t),
	}
	got, _ := checkTruncation(t, packTruncationMessage(t, m), 512)
	if got.Truncated || len(got.Additionals) != 2 || got.Additionals[0].Header.Name.String() != "ns.example.com." || got.Additionals[0].Body.(*dnsmessage.AResource).A != [4]byte{192, 0, 2, 1} {
		t.Fatalf("lost or corrupted relocated glue: %+v", got)
	}
}

func TestTruncateResponseSignedMessages(t *testing.T) {
	for _, typ := range []dnsmessage.Type{250, 24} {
		m := truncationMessage(10)
		m.Additionals = []dnsmessage.Resource{truncationOPT(t), {Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("key.example.com."), Class: dnsmessage.ClassANY}, Body: &dnsmessage.UnknownResource{Type: typ, Data: make([]byte, 40)}}}
		wire := packTruncationMessage(t, m)
		q := truncationQuery(t, 4096)
		buf := append([]byte(nil), wire...)
		if n := truncateResponse(buf, len(buf), 4096, q); n != len(wire) || !bytes.Equal(buf, wire) {
			t.Fatal("changed signed message that fits")
		}
		got, _ := checkTruncation(t, wire, 512)
		if got.RCode != dnsmessage.RCodeServerFailure || len(got.Answers) != 0 {
			t.Fatal("silently removed transaction signature from answer")
		}
	}
}

func FuzzTruncateResponse(f *testing.F) {
	m := truncationMessage(10)
	m.Additionals = []dnsmessage.Resource{truncationOPT(f)}
	f.Add(packTruncationMessage(f, m), uint16(512))
	f.Add(packTruncationMessage(f, m), uint16(1232))
	m = truncationMessage(0)
	m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Class: dnsmessage.ClassINET}, Body: &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("target.example.net.")}}}
	ns := dnsmessage.MustNewName("ns.example.net.")
	m.Authorities = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("example.net."), Class: dnsmessage.ClassINET}, Body: &dnsmessage.NSResource{NS: ns}}}
	m.Additionals = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: ns, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, 1}}}, truncationOPT(f, dnsmessage.Option{Code: 12, Data: make([]byte, 700)})}
	f.Add(packTruncationMessage(f, m), uint16(512))
	q := truncationQuery(f, 512)
	f.Fuzz(func(t *testing.T, data []byte, size uint16) {
		limit := min(max(512, int(size)), maxDNS0Size)
		if len(data) <= limit || len(data) > maxTCPSize {
			t.Skip()
		}
		buf := make([]byte, maxTCPSize)
		copy(buf, data)
		n := truncateResponse(buf, len(data), limit, q)
		if n < 12 || n > limit {
			t.Fatalf("bad response size: %d", n)
		}
		var got dnsmessage.Message
		if err := got.Unpack(buf[:n]); err != nil {
			t.Fatalf("malformed response: %v", err)
		}
	})
}

type truncationResolver struct{ response []byte }

func (r truncationResolver) Resolve(_ context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
	n := copy(buf, r.response)
	binary.BigEndian.PutUint16(buf[:2], q.ID)
	return n, resolver.ResolveInfo{}, nil
}

func TestServeUDP_TruncationTCPRetry(t *testing.T) {
	m := truncationMessage(30)
	m.Additionals = []dnsmessage.Resource{truncationOPT(t)}
	full := packTruncationMessage(t, m)
	p := Proxy{Upstream: truncationResolver{full}}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	tcp, err := net.Listen("tcp", udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() { _ = p.serveUDP(udp, make(chan struct{}, 10)) }()
	go func() { _ = p.serveTCP(tcp, make(chan struct{}, 10)) }()
	for _, size := range []int{512, 1232, 8192} {
		q := truncationQuery(t, size)
		c, err := net.Dial("udp", udp.LocalAddr().String())
		if err != nil {
			t.Fatal(err)
		}
		if err := c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Write(q.Payload); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, maxTCPSize)
		n, err := c.Read(buf)
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		var truncated dnsmessage.Message
		if err := truncated.Unpack(buf[:n]); err != nil {
			t.Fatal(err)
		}
		if !truncated.Truncated || n > min(size, maxDNS0Size) {
			t.Fatalf("size %d: UDP reply length=%d TC=%v", size, n, truncated.Truncated)
		}
		c, err = net.Dial("tcp", tcp.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if err := c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		request := make([]byte, 2, 2+len(q.Payload))
		binary.BigEndian.PutUint16(request, uint16(len(q.Payload)))
		request = append(request, q.Payload...)
		if _, err := c.Write(request); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(c, buf[:2]); err != nil {
			t.Fatal(err)
		}
		n = int(binary.BigEndian.Uint16(buf[:2]))
		if _, err := io.ReadFull(c, buf[:n]); err != nil {
			t.Fatal(err)
		}
		c.Close()
		if !bytes.Equal(buf[:n], full) {
			t.Fatal("TCP retry did not receive unmodified full answer")
		}
	}
}
