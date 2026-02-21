package host

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func udpListener(t *testing.T) *net.UDPConn {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func udpRead(t *testing.T, conn *net.UDPConn) string {
	t.Helper()
	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("udp read: %v", err)
	}
	return string(buf[:n])
}

func tcpListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func tcpAcceptAndRead(t *testing.T, ln net.Listener) string {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("tcp accept: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("tcp read: %v", err)
	}
	return string(buf[:n])
}

func portFromAddr(addr net.Addr) uint {
	switch a := addr.(type) {
	case *net.UDPAddr:
		return uint(a.Port)
	case *net.TCPAddr:
		return uint(a.Port)
	}
	return 0
}

func TestRemoteSyslog_RFC3164(t *testing.T) {
	conn := udpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(conn.LocalAddr()),
		Transport: "udp",
		Level:     "info",
		Format:    "rfc3164",
		Tag:       "nextdns",
	})
	if err != nil {
		t.Fatal(err)
	}

	lg.Info("test message")

	msg := udpRead(t, conn)
	// RFC 3164: <PRI>TIMESTAMP HOSTNAME TAG: MESSAGE\n
	// Priority for daemon.notice = 3*8 + 5 = 29
	if !strings.HasPrefix(msg, "<29>") {
		t.Errorf("expected priority <29>, got prefix: %q", msg[:10])
	}
	if !strings.Contains(msg, "nextdns: test message") {
		t.Errorf("expected 'nextdns: test message' in %q", msg)
	}
	if !strings.HasSuffix(msg, "\n") {
		t.Errorf("expected trailing newline in %q", msg)
	}
}

func TestRemoteSyslog_RFC5424(t *testing.T) {
	conn := udpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(conn.LocalAddr()),
		Transport: "udp",
		Level:     "info",
		Format:    "rfc5424",
		Tag:       "nextdns",
	})
	if err != nil {
		t.Fatal(err)
	}

	lg.Info("structured test")

	msg := udpRead(t, conn)
	// RFC 5424: <PRI>1 TIMESTAMP HOSTNAME TAG - - - MESSAGE\n
	if !strings.HasPrefix(msg, "<29>1 ") {
		t.Errorf("expected '<29>1 ' prefix, got: %q", msg[:10])
	}
	if !strings.Contains(msg, " nextdns - - - structured test") {
		t.Errorf("expected structured data format in %q", msg)
	}
	// Timestamp should be ISO 8601 format with Z suffix
	parts := strings.SplitN(msg, " ", 4)
	if len(parts) >= 2 && !strings.HasSuffix(parts[1], "Z") {
		t.Errorf("expected UTC timestamp ending in Z, got: %q", parts[1])
	}
}

func TestRemoteSyslog_LevelFilter(t *testing.T) {
	conn := udpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(conn.LocalAddr()),
		Transport: "udp",
		Level:     "warning",
		Format:    "rfc3164",
		Tag:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Debug and Info should be filtered out.
	lg.Debug("should not arrive")
	lg.Info("should not arrive either")

	// Warning should arrive.
	lg.Warning("this should arrive")

	msg := udpRead(t, conn)
	if !strings.Contains(msg, "this should arrive") {
		t.Errorf("expected warning message, got: %q", msg)
	}

	// Error should also arrive.
	lg.Error("error too")
	msg = udpRead(t, conn)
	if !strings.Contains(msg, "error too") {
		t.Errorf("expected error message, got: %q", msg)
	}
}

func TestRemoteSyslog_TCP(t *testing.T) {
	ln := tcpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(ln.Addr()),
		Transport: "tcp",
		Level:     "info",
		Format:    "rfc3164",
		Tag:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan string, 1)
	go func() {
		done <- tcpAcceptAndRead(t, ln)
	}()

	lg.Info("tcp message")

	select {
	case msg := <-done:
		if !strings.Contains(msg, "tcp message") {
			t.Errorf("expected 'tcp message' in %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for TCP message")
	}
}

func TestRemoteSyslog_Reconnect(t *testing.T) {
	ln := tcpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(ln.Addr()),
		Transport: "tcp",
		Level:     "info",
		Format:    "rfc3164",
		Tag:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Accept first connection in a goroutine (logger connects lazily on
	// first send, so Accept must not block the main goroutine).
	firstMsg := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 2048)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		firstMsg <- string(buf[:n])
	}()

	lg.Info("first")

	select {
	case msg := <-firstMsg:
		if !strings.Contains(msg, "first") {
			t.Errorf("expected 'first' in %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first message")
	}

	// Force the logger to detect the broken connection by closing its conn
	// directly — on macOS, writes to a peer-closed TCP socket may be buffered
	// by the kernel and not fail immediately.
	rl := lg.(*remoteSyslogLogger)
	rl.mu.Lock()
	if rl.conn != nil {
		rl.conn.Close()
		rl.conn = nil
	}
	rl.mu.Unlock()

	// Accept the reconnection in a goroutine before writing.
	secondMsg := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 2048)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		secondMsg <- string(buf[:n])
	}()

	lg.Info("second")

	select {
	case msg := <-secondMsg:
		if !strings.Contains(msg, "second") {
			t.Errorf("expected 'second' in reconnected message, got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for reconnect message")
	}
}

func TestRemoteSyslog_InvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  RemoteSyslogConfig
	}{
		{"empty server", RemoteSyslogConfig{Server: "", Transport: "udp"}},
		{"invalid transport", RemoteSyslogConfig{Server: "localhost", Transport: "quic"}},
		{"invalid level", RemoteSyslogConfig{Server: "localhost", Level: "trace"}},
		{"invalid format", RemoteSyslogConfig{Server: "localhost", Format: "json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRemoteSyslogLogger(tt.cfg)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestRemoteSyslog_AutoPort(t *testing.T) {
	tests := []struct {
		transport string
		wantPort  string
	}{
		{"udp", ":514"},
		{"tcp", ":514"},
		{"tls", ":6514"},
	}
	for _, tt := range tests {
		t.Run(tt.transport, func(t *testing.T) {
			lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
				Server:    "192.0.2.1",
				Port:      0,
				Transport: tt.transport,
				Tag:       "test",
			})
			if err != nil {
				t.Fatal(err)
			}
			rl := lg.(*remoteSyslogLogger)
			if !strings.HasSuffix(rl.addr, tt.wantPort) {
				t.Errorf("addr = %q, want suffix %q", rl.addr, tt.wantPort)
			}
		})
	}
}

func TestRemoteSyslog_Priority(t *testing.T) {
	tests := []struct {
		severity int
		wantPri  int
	}{
		{severityDebug, 31},   // 3*8 + 7
		{severityNotice, 29},  // 3*8 + 5
		{severityWarning, 28}, // 3*8 + 4
		{severityError, 27},   // 3*8 + 3
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("severity_%d", tt.severity), func(t *testing.T) {
			got := priority(tt.severity)
			if got != tt.wantPri {
				t.Errorf("priority(%d) = %d, want %d", tt.severity, got, tt.wantPri)
			}
		})
	}
}

func TestRemoteSyslog_UDPTruncation(t *testing.T) {
	conn := udpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(conn.LocalAddr()),
		Transport: "udp",
		Level:     "debug",
		Format:    "rfc3164",
		Tag:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Send a message that with header will exceed 1024 bytes.
	longMsg := strings.Repeat("x", 1100)
	lg.Info(longMsg)

	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n > maxUDPLen {
		t.Errorf("UDP message length = %d, want <= %d", n, maxUDPLen)
	}
}

func TestRemoteSyslog_ConcurrentWrites(t *testing.T) {
	conn := udpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(conn.LocalAddr()),
		Transport: "udp",
		Level:     "debug",
		Format:    "rfc3164",
		Tag:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Drain messages in background.
	go func() {
		buf := make([]byte, 2048)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, err := conn.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				lg.Infof("goroutine %d msg %d", id, j)
			}
		}(i)
	}
	wg.Wait()
}

func TestRemoteSyslog_LogInjection(t *testing.T) {
	conn := udpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(conn.LocalAddr()),
		Transport: "udp",
		Level:     "info",
		Format:    "rfc3164",
		Tag:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Attempt log injection via newline in message.
	lg.Info("legit\n<29>fake injected message")

	msg := udpRead(t, conn)
	// The newline should be replaced, so the message should be on one line.
	lines := strings.Split(strings.TrimSuffix(msg, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("log injection: message split into %d lines: %q", len(lines), msg)
	}
}

func TestRemoteSyslog_TCPNoTruncation(t *testing.T) {
	ln := tcpListener(t)
	lg, err := NewRemoteSyslogLogger(RemoteSyslogConfig{
		Server:    "127.0.0.1",
		Port:      portFromAddr(ln.Addr()),
		Transport: "tcp",
		Level:     "debug",
		Format:    "rfc3164",
		Tag:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		b, _ := io.ReadAll(conn)
		done <- string(b)
	}()

	// Send a long message — TCP should not truncate.
	longMsg := strings.Repeat("y", 1500)
	lg.Info(longMsg)

	// Close the logger's connection to trigger EOF on reader side.
	rl := lg.(*remoteSyslogLogger)
	rl.mu.Lock()
	if rl.conn != nil {
		rl.conn.Close()
		rl.conn = nil
	}
	rl.mu.Unlock()

	select {
	case msg := <-done:
		if len(msg) <= maxUDPLen {
			t.Errorf("TCP message should not be truncated, got length %d", len(msg))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}
