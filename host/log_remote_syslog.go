package host

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Syslog severity levels (RFC 5424 Section 6.2.1).
const (
	severityEmergency = iota
	severityAlert
	severityCritical
	severityError
	severityWarning
	severityNotice
	severityInfo
	severityDebug
)

// Syslog facility for daemon (RFC 5424 Section 6.2.1).
const facilityDaemon = 3

// maxUDPLen is the maximum safe UDP syslog message size per RFC 3164.
const maxUDPLen = 1024

// RemoteSyslogConfig holds configuration for a remote syslog logger.
type RemoteSyslogConfig struct {
	Server    string // host or IP (required)
	Port      uint   // 0 = auto (514 for udp/tcp, 6514 for tls)
	Transport string // "udp", "tcp", "tls"
	Level     string // "debug", "info", "warning", "error"
	Format    string // "rfc3164", "rfc5424"
	Tag       string // syslog tag (e.g. "nextdns")
}

type remoteSyslogLogger struct {
	mu        sync.Mutex
	conn      net.Conn
	addr      string
	network   string        // "udp" or "tcp"
	tlsConfig *tls.Config   // non-nil for TLS transport
	level     int           // minimum severity to send (lower = more severe)
	formatter func(severity int, tag, hostname, msg string) string
	tag       string
	hostname  string
}

// NewRemoteSyslogLogger creates a Logger that sends messages to a remote syslog
// server over UDP, TCP, or TLS. The connection is established lazily on first
// write and re-established transparently on failure.
func NewRemoteSyslogLogger(cfg RemoteSyslogConfig) (Logger, error) {
	if cfg.Server == "" {
		return nil, errors.New("syslog-server is required")
	}

	var network string
	var tlsConfig *tls.Config
	switch strings.ToLower(cfg.Transport) {
	case "udp", "":
		network = "udp"
	case "tcp":
		network = "tcp"
	case "tls":
		network = "tcp"
		tlsConfig = &tls.Config{
			ServerName: cfg.Server,
		}
	default:
		return nil, fmt.Errorf("invalid syslog-transport: %q (must be udp, tcp, or tls)", cfg.Transport)
	}

	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	var formatter func(severity int, tag, hostname, msg string) string
	switch strings.ToLower(cfg.Format) {
	case "rfc3164", "":
		formatter = formatRFC3164
	case "rfc5424":
		formatter = formatRFC5424
	default:
		return nil, fmt.Errorf("invalid syslog-format: %q (must be rfc3164 or rfc5424)", cfg.Format)
	}

	port := cfg.Port
	if port == 0 {
		if tlsConfig != nil {
			port = 6514
		} else {
			port = 514
		}
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	tag := cfg.Tag
	if tag == "" {
		tag = "nextdns"
	}

	return &remoteSyslogLogger{
		addr:      net.JoinHostPort(cfg.Server, fmt.Sprint(port)),
		network:   network,
		tlsConfig: tlsConfig,
		level:     level,
		formatter: formatter,
		tag:       tag,
		hostname:  hostname,
	}, nil
}

func parseLevel(s string) (int, error) {
	switch strings.ToLower(s) {
	case "debug":
		return severityDebug, nil
	case "info", "":
		return severityNotice, nil
	case "warning":
		return severityWarning, nil
	case "error":
		return severityError, nil
	default:
		return 0, fmt.Errorf("invalid syslog-level: %q (must be debug, info, warning, or error)", s)
	}
}

func (l *remoteSyslogLogger) Debug(v ...interface{}) {
	l.send(severityDebug, fmt.Sprint(v...))
}

func (l *remoteSyslogLogger) Debugf(format string, a ...interface{}) {
	l.send(severityDebug, fmt.Sprintf(format, a...))
}

func (l *remoteSyslogLogger) Info(v ...interface{}) {
	// Use notice instead of info as many systems filter < notice level.
	l.send(severityNotice, fmt.Sprint(v...))
}

func (l *remoteSyslogLogger) Infof(format string, a ...interface{}) {
	l.send(severityNotice, fmt.Sprintf(format, a...))
}

func (l *remoteSyslogLogger) Warning(v ...interface{}) {
	l.send(severityWarning, fmt.Sprint(v...))
}

func (l *remoteSyslogLogger) Warningf(format string, a ...interface{}) {
	l.send(severityWarning, fmt.Sprintf(format, a...))
}

func (l *remoteSyslogLogger) Error(v ...interface{}) {
	l.send(severityError, fmt.Sprint(v...))
}

func (l *remoteSyslogLogger) Errorf(format string, a ...interface{}) {
	l.send(severityError, fmt.Sprintf(format, a...))
}

func (l *remoteSyslogLogger) send(severity int, msg string) {
	// Filter by configured level. Lower severity number = more critical.
	if severity > l.level {
		return
	}

	// Sanitize: replace newlines to prevent log injection.
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")

	line := l.formatter(severity, l.tag, l.hostname, msg)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn == nil {
		if err := l.dial(); err != nil {
			return
		}
	}

	data := []byte(line)
	// Truncate UDP messages to max safe size.
	if l.network == "udp" && len(data) > maxUDPLen {
		data = data[:maxUDPLen]
	}

	_, err := l.conn.Write(data)
	if err != nil {
		// Close and nil the connection so next call retries.
		l.conn.Close()
		l.conn = nil
	}
}

func (l *remoteSyslogLogger) dial() error {
	var conn net.Conn
	var err error

	dialer := net.Dialer{Timeout: 5 * time.Second}
	if l.tlsConfig != nil {
		conn, err = tls.DialWithDialer(&dialer, l.network, l.addr, l.tlsConfig)
	} else {
		conn, err = dialer.Dial(l.network, l.addr)
	}
	if err != nil {
		return err
	}
	l.conn = conn
	return nil
}

// priority computes the syslog PRI value: facility * 8 + severity.
func priority(severity int) int {
	return facilityDaemon*8 + severity
}

// formatRFC3164 produces a BSD syslog message (RFC 3164).
// Format: <PRI>Mon DD HH:MM:SS hostname tag: message\n
func formatRFC3164(severity int, tag, hostname, msg string) string {
	ts := time.Now().Format(time.Stamp) // "Jan _2 15:04:05"
	return fmt.Sprintf("<%d>%s %s %s: %s\n", priority(severity), ts, hostname, tag, msg)
}

// formatRFC5424 produces a structured syslog message (RFC 5424).
// Format: <PRI>1 TIMESTAMP HOSTNAME TAG - - - MESSAGE\n
func formatRFC5424(severity int, tag, hostname, msg string) string {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return fmt.Sprintf("<%d>1 %s %s %s - - - %s\n", priority(severity), ts, hostname, tag, msg)
}
