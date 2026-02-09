package resolver

import (
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver/query"
)

type cacheKey struct {
	ctx    string
	qclass query.Class
	qtype  query.Type
	qname  string
}

func (k cacheKey) String() string {
	return fmt.Sprintf("%s %s %s %s", k.ctx, k.qclass, k.qtype, k.qname)
}

func (k cacheKey) Hash() uint64 {
	var d xxhash.Digest
	d.Reset()

	if k.ctx != "" {
		// Avoid allocation from []byte(string).
		_, _ = d.Write(unsafe.Slice(unsafe.StringData(k.ctx), len(k.ctx)))
	}
	var b [4]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(k.qclass))
	binary.BigEndian.PutUint16(b[2:4], uint16(k.qtype))
	_, _ = d.Write(b[:])
	if k.qname != "" {
		_, _ = d.Write(unsafe.Slice(unsafe.StringData(k.qname), len(k.qname)))
	}
	return d.Sum64()
}

func (k cacheKey) ValidateQuestion(msg []byte) bool {
	var p dnsmessage.Parser
	_, err := p.Start(msg)
	if err != nil {
		return false
	}
	q, err := p.Question()
	if err != nil {
		return false
	}
	if query.Class(q.Class) != k.qclass || query.Type(q.Type) != k.qtype {
		return false
	}
	return q.Name.String() == k.qname
}

type cacheValue struct {
	time  time.Time
	msg   []byte
	trans string
}

// AdjustedResponse returns the cached response the message id set to id and the
// TTLs adjusted to the age of the record in cache. The minimum resulting TTL is
// returned as minTTL. If the age of the record exceeded the minTTL or maxAge,
// minTTL is set to 0. If the response is invalid, b is nil and minTTL is 0. If
// maxTTL is greater than 0 and the age of a record exceeds it, the TTL is
// capped to this value, but won't affect returned minTTL.
func (v cacheValue) AdjustedResponse(buf []byte, id uint16, maxAge, maxTTL uint32, now time.Time) (n int, minTTL uint32) {
	n = len(v.msg)
	if n < 12 {
		return 0, 0
	}
	msg := v.msg
	if len(buf) < n {
		return 0, 0
	}
	copy(buf, msg)
	// Set the message id
	buf[0] = byte(id >> 8)
	buf[1] = byte(id)

	// Update TTLs and compute minTTL
	age := uint32(now.Sub(v.time) / time.Second)
	minTTL = updateTTL(buf[:n], age, maxAge, maxTTL)
	return n, minTTL
}

func updateTTL(msg []byte, age uint32, maxAge, maxTTL uint32) (minTTL uint32) {
	if len(msg) < 12 {
		return 0
	}
	// Read message header
	questions := binary.BigEndian.Uint16(msg[4:6])
	answers := binary.BigEndian.Uint16(msg[6:8])
	authorities := binary.BigEndian.Uint16(msg[8:10])
	additionals := binary.BigEndian.Uint16(msg[10:12])
	// Skip message header
	off := 12
	// Skip questions
	for i := questions; i > 0; i-- {
		if off >= len(msg) {
			return 0
		}
		l := skipName(msg[off:])
		if l == 0 {
			// Invalid label
			return 0
		}
		off += l + 4 // qtype(uint16) + qclass(uint16)
		if off > len(msg) {
			return 0
		}
	}
	// Update RRs
	minTTL = ^minTTL
	rrCount := answers + authorities + additionals
	additionalsIdx := answers + authorities
	for i := uint16(0); i < rrCount; i++ {
		if off >= len(msg) {
			break
		}

		// Skip label and fixed fields
		l := skipName(msg[off:])
		if l == 0 {
			// Invalid label
			return 0
		}
		off += l + 10 // qtype(uint16) + qclass(uint16) + ttl(int32) + RDLENGTH(uint16)
		if off > len(msg) {
			// Invalid RR
			return 0
		}

		// Update TTL (except if RR is OPT)
		qtype := binary.BigEndian.Uint16(msg[off-10 : off-8])
		if query.Type(qtype) != query.TypeOPT {
			ttl := binary.BigEndian.Uint32(msg[off-6 : off-2])
			if age > ttl {
				ttl = 0
			} else {
				ttl -= age
			}
			// Update minTTL for records in answer and authority sections
			if i < additionalsIdx {
				if maxAge > 0 && age > maxAge {
					minTTL = 0
				} else if minTTL > ttl {
					minTTL = ttl
				}
			}
			// Update the record
			if maxTTL > 0 && ttl > maxTTL {
				ttl = maxTTL
			}
			binary.BigEndian.PutUint32(msg[off-6:off-2], ttl)
		}

		// Skip the data part of the record
		rdlen := binary.BigEndian.Uint16(msg[off-2 : off])
		off += int(rdlen)
		if off > len(msg) {
			// Invalid RR
			return 0
		}
	}
	if ^minTTL == 0 {
		minTTL = 0
	}
	return minTTL
}

func skipName(msg []byte) (newOff int) {
Loop:
	for {
		if newOff >= len(msg) {
			return 0
		}
		c := int(msg[newOff])
		newOff++
		switch c & 0xC0 {
		case 0x00:
			if c == 0x00 {
				// A zero length signals the end of the name.
				break Loop
			}
			// literal string
			newOff += c
			if newOff > len(msg) {
				return 0
			}
		case 0xC0:
			// Pointer to somewhere else in msg.

			// Pointers are two bytes.
			newOff++

			// Don't follow the pointer as the data here has ended.
			break Loop
		default:
			// Prefixes 0x80 and 0x40 are reserved.
			return 0
		}
	}

	return newOff
}
