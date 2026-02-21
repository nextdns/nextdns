package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// serveDoH starts an HTTP/2 server on l that accepts DNS-over-HTTPS queries
// per RFC 8484. Both POST (binary body) and GET (?dns= base64url) are supported.
func (p Proxy) serveDoH(l net.Listener, inflightRequests chan struct{}) error {
	bpool := &sync.Pool{
		New: func() interface{} {
			return make([]byte, maxTCPSize)
		},
	}

	srv := &http.Server{
		Handler: p.dohHandler(inflightRequests, bpool),
	}
	return srv.Serve(l)
}

func (p Proxy) dohHandler(inflightRequests chan struct{}, bpool *sync.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dns-query" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		var payload []byte
		var err error

		switch r.Method {
		case http.MethodPost:
			if ct := r.Header.Get("Content-Type"); ct != "application/dns-message" {
				http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
				return
			}
			payload, err = io.ReadAll(io.LimitReader(r.Body, maxTCPSize))
			if err != nil {
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
		case http.MethodGet:
			dnsParam := r.URL.Query().Get("dns")
			if dnsParam == "" {
				http.Error(w, "Missing dns parameter", http.StatusBadRequest)
				return
			}
			payload, err = base64.RawURLEncoding.DecodeString(dnsParam)
			if err != nil {
				http.Error(w, "Invalid dns parameter", http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if len(payload) <= 14 {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Throttle inflight requests.
		select {
		case inflightRequests <- struct{}{}:
		default:
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		start := time.Now()
		buf := bpool.Get().([]byte)
		rbuf := bpool.Get().([]byte)

		var rsize int
		var ri resolver.ResolveInfo

		// Determine peer IP from the connection.
		peerIP := net.IP{}
		if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			_ = addr // local addr, not peer
		}
		if host, _, splitErr := net.SplitHostPort(r.RemoteAddr); splitErr == nil {
			peerIP = net.ParseIP(host)
		}
		localIP := net.IP{}
		if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			if host, _, splitErr := net.SplitHostPort(addr.String()); splitErr == nil {
				localIP = net.ParseIP(host)
			}
		}

		copy(buf[:len(payload)], payload)
		q, qerr := query.New(buf[:len(payload)], peerIP, localIP)

		defer func() {
			if rec := recover(); rec != nil {
				stackBuf := make([]byte, 64<<10)
				stackBuf = stackBuf[:runtime.Stack(stackBuf, false)]
				qerr = fmt.Errorf("panic: %v: %s", rec, string(stackBuf))
			}
			bpool.Put(buf)
			bpool.Put(rbuf)
			<-inflightRequests
			p.logQuery(QueryInfo{
				PeerIP:            q.PeerIP,
				Protocol:          "DoH",
				Type:              q.Type.String(),
				Name:              q.Name,
				QuerySize:         len(payload),
				ResponseSize:      rsize,
				Duration:          time.Since(start),
				Profile:           ri.Profile,
				FromCache:         ri.FromCache,
				UpstreamTransport: ri.Transport,
				Error:             qerr,
			})
		}()

		if qerr != nil {
			p.logErr(qerr)
			rsize = replyRCode(dnsmessage.RCodeFormatError, q, rbuf)
			w.Header().Set("Content-Type", "application/dns-message")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(rbuf[:rsize])
			return
		}

		ctx := r.Context()
		if p.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, p.Timeout)
			defer cancel()
		}

		if rsize, ri, qerr = p.Resolve(ctx, q, rbuf); qerr != nil || rsize <= 0 || rsize > maxTCPSize {
			rsize = replyRCode(dnsmessage.RCodeServerFailure, q, rbuf)
		}

		w.Header().Set("Content-Type", "application/dns-message")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rbuf[:rsize])
	})
}
