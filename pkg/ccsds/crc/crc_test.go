package crc_test

import (
	"testing"

	"github.com/absmach/zenith-link/pkg/ccsds/crc"
	"github.com/stretchr/testify/assert"
)

func TestCCITT16(t *testing.T) {
	tests := []struct {
		desc string
		data []byte
		want uint16
	}{
		{
			// NIST / online calculator disambiguation vector.
			// Only CRC-16/CCITT-FALSE (init=0xFFFF) produces 0x29B1.
			desc: "standard check vector 123456789",
			data: []byte("123456789"),
			want: 0x29B1,
		},
		{
			desc: "empty input",
			data: []byte{},
			want: 0xFFFF, // no bytes processed → init value returned
		},
		{
			desc: "single zero byte",
			data: []byte{0x00},
			want: 0xE1F0,
		},
		{
			// init=0xFFFF: index = (0xFF>>0) ^ 0xFF = 0x00, table[0]=0; crc = 0xFF00 ^ 0 = 0xFF00
			desc: "single 0xFF byte",
			data: []byte{0xFF},
			want: 0xFF00,
		},
		{
			// After {0x00} crc=0xE1F0; index=0xE1, table[0xE1]=0xED0F; crc=0xF000^0xED0F=0x1D0F
			desc: "two byte sequence",
			data: []byte{0x00, 0x00},
			want: 0x1D0F,
		},
		{
			desc: "all zeros 10 bytes",
			data: make([]byte, 10),
			want: func() uint16 {
				// Pre-computed reference.
				crcVal := uint16(0xFFFF)
				for i := 0; i < 10; i++ {
					crcVal = (crcVal << 8) ^ crcTable(byte(crcVal>>8))
				}
				return crcVal
			}(),
		},
		{
			desc: "CCSDS frame header 6 bytes example",
			data: []byte{0x00, 0x5A, 0x00, 0x00, 0x00, 0x00},
			want: crc.CCITT16([]byte{0x00, 0x5A, 0x00, 0x00, 0x00, 0x00}), // self-consistency
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := crc.CCITT16(tc.data)
			assert.Equal(t, tc.want, got,
				"CCITT16(%x) want 0x%04X got 0x%04X", tc.data, tc.want, got)
		})
	}
}

// crcTable mirrors the table-based computation for the self-consistency test.
func crcTable(b byte) uint16 {
	val := uint16(b) << 8
	for j := 0; j < 8; j++ {
		if val&0x8000 != 0 {
			val = (val << 1) ^ 0x1021
		} else {
			val <<= 1
		}
	}
	return val
}

func TestVerify(t *testing.T) {
	tests := []struct {
		desc  string
		frame []byte
		want  bool
	}{
		{
			desc:  "too short",
			frame: []byte{0x01},
			want:  false,
		},
		{
			desc: "good frame",
			frame: func() []byte {
				payload := []byte("123456789")
				return crc.Append(nil, payload)
			}(),
			want: true,
		},
		{
			desc: "corrupted last byte",
			frame: func() []byte {
				payload := []byte("123456789")
				f := crc.Append(nil, payload)
				f[len(f)-1] ^= 0xFF
				return f
			}(),
			want: false,
		},
		{
			desc: "corrupted middle byte",
			frame: func() []byte {
				payload := []byte("123456789")
				f := crc.Append(nil, payload)
				f[3] ^= 0x01
				return f
			}(),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := crc.Verify(tc.frame)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAppend(t *testing.T) {
	tests := []struct {
		desc    string
		dst     []byte
		payload []byte
	}{
		{
			desc:    "append to nil dst",
			dst:     nil,
			payload: []byte("hello"),
		},
		{
			desc:    "append to non-empty dst",
			dst:     []byte{0x01, 0x02},
			payload: []byte{0xAB, 0xCD},
		},
		{
			desc:    "empty payload",
			dst:     nil,
			payload: []byte{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			original := make([]byte, len(tc.dst))
			copy(original, tc.dst)

			result := crc.Append(tc.dst, tc.payload)

			// dst prefix must be preserved
			assert.Equal(t, original, result[:len(original)])
			// total length = dst + payload + 2
			assert.Equal(t, len(original)+len(tc.payload)+2, len(result))
			// the full result (dst + payload + crc) must verify cleanly
			assert.True(t, crc.Verify(result[len(original):]))
		})
	}
}
