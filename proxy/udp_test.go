package proxy

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

func TestServeUDP_FullLimitReturnsServerFailure(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	t.Cleanup(func() { server.Close() })
	client, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		server.Close()
		t.Fatalf("DialUDP() error = %v", err)
	}
	defer client.Close()

	inflightRequests := make(chan struct{}, 1)
	inflightRequests <- struct{}{}
	errC := make(chan error, 1)
	go func() {
		errC <- (Proxy{}).serveUDP(server, inflightRequests)
	}()

	query := []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x04, 't', 'e', 's', 't', 0x00, 0x00, 0x01, 0x00, 0x01,
	}
	if _, err := client.Write(query); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 512)
	n, err := client.Read(response)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n < 4 || response[3]&0x0f != 2 {
		t.Fatalf("response = %v, want SERVFAIL", response[:n])
	}

	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-errC:
	case <-time.After(time.Second):
		t.Fatal("serveUDP() did not stop while the inflight limit was full")
	}
}

type largeAnswerResolver struct{}

func (largeAnswerResolver) Resolve(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
	name := dnsmessage.MustNewName(q.Name)
	b := dnsmessage.NewBuilder(buf[:0], dnsmessage.Header{ID: q.ID, Response: true, RecursionDesired: true, RecursionAvailable: true})
	_ = b.StartQuestions()
	_ = b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET})
	_ = b.StartAnswers()
	txt := make([]byte, 200)
	for i := range txt {
		txt[i] = 'a'
	}
	for i := 0; i < 10; i++ {
		_ = b.TXTResource(dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.TXTResource{TXT: []string{string(txt)}})
	}
	_ = b.StartAdditionals()
	var opt dnsmessage.ResourceHeader
	_ = opt.SetEDNS0(1232, dnsmessage.RCodeSuccess, false)
	_ = b.OPTResource(opt, dnsmessage.OPTResource{})
	msg, err := b.Finish()
	return len(msg), resolver.ResolveInfo{}, err
}

func TestServeUDP_TruncatedResponseIsWellFormed(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	t.Cleanup(func() { server.Close() })
	client, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("DialUDP() error = %v", err)
	}
	defer client.Close()

	go func() {
		_ = (Proxy{Upstream: largeAnswerResolver{}}).serveUDP(server, make(chan struct{}, 10))
	}()

	for _, bufSize := range []uint16{0, 1, 512, 1232} {
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 42, RecursionDesired: true})
		_ = b.StartQuestions()
		_ = b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName("example.com."), Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET})
		if bufSize > 0 {
			_ = b.StartAdditionals()
			var opt dnsmessage.ResourceHeader
			_ = opt.SetEDNS0(int(bufSize), dnsmessage.RCodeSuccess, true)
			_ = b.OPTResource(opt, dnsmessage.OPTResource{})
		}
		q, err := b.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Write(q); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		resp := make([]byte, 4096)
		n, err := client.Read(resp)
		if err != nil {
			t.Fatalf("bufsize %d: Read() error = %v", bufSize, err)
		}
		limit := 512
		if bufSize > 512 {
			limit = int(bufSize)
		}
		if n > limit {
			t.Fatalf("bufsize %d: response is %d bytes, want <= %d", bufSize, n, limit)
		}
		var msg dnsmessage.Message
		if err := msg.Unpack(resp[:n]); err != nil {
			t.Fatalf("bufsize %d: truncated response is malformed: %v", bufSize, err)
		}
		if !msg.Header.Truncated {
			t.Fatalf("bufsize %d: TC bit not set", bufSize)
		}
		if msg.Header.ID != 42 || len(msg.Questions) != 1 || msg.Questions[0].Name.String() != "example.com." {
			t.Fatalf("bufsize %d: unexpected header/question: %+v", bufSize, msg)
		}
		if len(msg.Answers) != 0 {
			t.Fatalf("bufsize %d: got %d answers in truncated response, want 0", bufSize, len(msg.Answers))
		}
		wantOPT := 0
		if bufSize > 0 {
			wantOPT = 1
		}
		if len(msg.Additionals) != wantOPT || (wantOPT == 1 && msg.Additionals[0].Header.Type != dnsmessage.TypeOPT) {
			t.Fatalf("bufsize %d: additionals = %+v, want %d OPT", bufSize, msg.Additionals, wantOPT)
		}
	}
}
