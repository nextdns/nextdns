package resolver

import (
	"time"

	"github.com/nextdns/nextdns/resolver/query"
)

type cacheKey struct {
	ctx    string
	qclass query.Class
	qtype  query.Type
	qname  string
}

type cacheValue struct {
	time  time.Time
	msg   []byte
	trans string
}

// AdjustedResponse returns the cached response the message id set to id and the
// TTLs adjusted to the age of the record in cache. The minimum resulting TTL is
// returned as minTTL. If the age of the record exceeded the minTTL, minTTL is set
// to 0.
// If the response is invalid, b is nil and minTTL is 0.
func (v cacheValue) AdjustedResponse(id uint16, now time.Time) (b []byte, minTTL uint32) {
	if len(v.msg) < 12 {
		return nil, 0
	}
	msg := v.msg
	b = make([]byte, len(msg))
	copy(b, msg)
	// Set the message id
	b[0] = byte(id >> 8)
	b[1] = byte(id)
	// Read message header
	questions := unpackUint16(msg[4:])
	answers := unpackUint16(msg[6:])
	authorities := unpackUint16(msg[8:])
	additionals := unpackUint16(msg[10:])
	// Skip message header
	off := 12
	// Skip questions
	for i := questions; i > 0; i-- {
		if off >= len(msg) {
			return nil, 0
		}
		l := skipName(msg[off:])
		if l == 0 {
			// Invalid label
			return nil, 0
		}
		off += l + 4 // qtype(uint16) + qclass(uint16)
		if off > len(msg) {
			return nil, 0
		}
	}
	// Update RRs
	minTTL = ^minTTL
	age := uint32(now.Sub(v.time) / time.Second)
	for i := answers + authorities + additionals; i > 0; i-- {
		if off >= len(msg) {
			break
		}

		// Skip label and fixed fields
		l := skipName(msg[off:])
		if l == 0 {
			// Invalid label
			return nil, 0
		}
		off += l + 10 // qtype(uint16) + qclass(uint16) + ttl(int32) + RDLENGTH(uint16)
		if off > len(msg) {
			// Invalid RR
			return nil, 0
		}

		// Update TTL
		ttl := unpackUint32(msg[off-6:])
		if age > ttl {
			ttl = 0
		} else {
			ttl -= age
		}
		if minTTL > ttl {
			minTTL = ttl
		}
		packUint32(b[off-6:], ttl)

		// Skip the data part of the record
		rdlen := unpackUint16(msg[off-2:])
		off += int(rdlen)
		if off > len(msg) {
			// Invalid RR
			return nil, 0
		}
	}
	if ^minTTL == 0 {
		minTTL = 0
	}
	return b, minTTL
}

func unpackUint16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

func unpackUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func packUint32(b []byte, n uint32) {
	b[0] = byte(n >> 24)
	b[1] = byte(n >> 16)
	b[2] = byte(n >> 8)
	b[3] = byte(n)
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
