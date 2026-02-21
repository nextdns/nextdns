package resolver

import (
	"reflect"
	"testing"
	"time"

	"github.com/nextdns/nextdns/resolver/query"
)

func Test_cacheValue_AdjustedResponse(t *testing.T) {
	type fields struct {
		time time.Time
		msg  []byte
	}
	now := time.Now()
	tests := []struct {
		name       string
		fields     fields
		id         uint16
		wantB      []byte
		wantMinTTL uint32
	}{
		{
			"Empty Record",
			fields{
				now.Add(-10 * time.Second),
				[]byte{},
			},
			123,
			[]byte{},
			0,
		},
		{
			"Happy Path",
			fields{
				now.Add(-10 * time.Second),
				[]byte{
					0xa6, 0xed, // ID
					0x81, 0x80, // Flags
					0x00, 0x01, // Questions
					0x00, 0x01, // Answers
					0x00, 0x00, // Authorities
					0x00, 0x01, // Additionals
					// Questions
					0x04, 0x74, 0x65, 0x73, 0x74, 0x03, 0x63, 0x6f, 0x6d, 0x00, // Label test.com.
					0x00, 0x01, // Type A
					0x00, 0x01, // Class IN
					// Answers
					0xc0, 0x0c, // Label pointer test.com.
					0x00, 0x01, // Type A
					0x00, 0x01, // Class IN
					0x00, 0x00, 0x0e, 0x10, // TTL 3600
					0x00, 0x04, // Data len 4
					0x45, 0xac, 0xc8, 0xeb, // 69.172.200.
					// Additionals
					0x00,       // Label <root>
					0x00, 0x29, // Type OPT
					0x05, 0xac, // UDP packet size
					0x00,       // Extended RCODE
					0x00,       // EDNS Version
					0x00, 0x00, // Flags
					0x00, 0x00, // DATA
				},
			},
			123,
			[]byte{
				0x00, 0x7b, // ID = 123
				0x81, 0x80, // Flags
				0x00, 0x01, // Questions
				0x00, 0x01, // Answers
				0x00, 0x00, // Authorities
				0x00, 0x01, // Additionals
				// Questions
				0x04, 0x74, 0x65, 0x73, 0x74, 0x03, 0x63, 0x6f, 0x6d, 0x00, // Label test.com.
				0x00, 0x01, // Type A
				0x00, 0x01, // Class IN
				// Answers
				0xc0, 0x0c, // Label pointer test.com.
				0x00, 0x01, // Type A
				0x00, 0x01, // Class IN
				0x00, 0x00, 0x0e, 0x06, // TTL 3600 - 10
				0x00, 0x04, // Data len 4
				0x45, 0xac, 0xc8, 0xeb, // 69.172.200.235
				// Additionals
				0x00,       // Label <root>
				0x00, 0x29, // Type OPT
				0x05, 0xac, // UDP packet size
				0x00,       // Extended RCODE
				0x00,       // EDNS Version
				0x00, 0x00, // Flags
				0x00, 0x00, // DATA
			},
			3600 - 10,
		},
		// TODO: fuzz
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := cacheValue{
				time: tt.fields.time,
				msg:  tt.fields.msg,
			}
			buf := make([]byte, 4096)
			n, gotMinTTL := v.AdjustedResponse(buf, tt.id, 0, 0, now)
			if gotB := buf[:n]; !reflect.DeepEqual(gotB, tt.wantB) {
				t.Errorf("cacheValue.AdjustedResponse()\ngotB:\n%#v\nwant:\n%#v", gotB, tt.wantB)
			}
			if gotMinTTL != tt.wantMinTTL {
				t.Errorf("cacheValue.AdjustedResponse() gotMinTTL = %v, want %v", gotMinTTL, tt.wantMinTTL)
			}
		})
	}
}

func Test_cacheKey_ValidateQuestion(t *testing.T) {
	msg := []byte{
		0x00, 0x01, // ID
		0x01, 0x00, // Flags (RD)
		0x00, 0x01, // Questions
		0x00, 0x00, // Answers
		0x00, 0x00, // Authorities
		0x00, 0x00, // Additionals
		// Question: test.com. A IN
		0x04, 0x74, 0x65, 0x73, 0x74, 0x03, 0x63, 0x6f, 0x6d, 0x00,
		0x00, 0x01, // Type A
		0x00, 0x01, // Class IN
	}
	k := cacheKey{"", query.ClassINET, query.TypeA, "test.com."}
	if !k.ValidateQuestion(msg) {
		t.Fatalf("expected ValidateQuestion to be true")
	}
	k2 := cacheKey{"", query.ClassINET, query.TypeAAAA, "test.com."}
	if k2.ValidateQuestion(msg) {
		t.Fatalf("expected ValidateQuestion to be false")
	}
}
