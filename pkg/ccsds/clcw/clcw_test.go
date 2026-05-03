package clcw_test

import (
	"testing"

	"github.com/absmach/orbitron/pkg/ccsds/clcw"
	"github.com/absmach/orbitron/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		desc    string
		input   clcw.CLCW
		want    [4]byte
		wantErr error
	}{
		{
			desc:  "all-zero CLCW",
			input: clcw.CLCW{},

			want: [4]byte{0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "NoRFAvail set",
			input: clcw.CLCW{
				NoRFAvail: true,
			},

			want: [4]byte{0x00, 0x00, 0x80, 0x00},
		},
		{
			desc: "NoBitLock set",
			input: clcw.CLCW{
				NoBitLock: true,
			},

			want: [4]byte{0x00, 0x00, 0x40, 0x00},
		},
		{
			desc: "Lockout set",
			input: clcw.CLCW{
				Lockout: true,
			},

			want: [4]byte{0x00, 0x00, 0x20, 0x00},
		},
		{
			desc: "Wait set",
			input: clcw.CLCW{
				Wait: true,
			},

			want: [4]byte{0x00, 0x00, 0x10, 0x00},
		},
		{
			desc: "Retransmit set",
			input: clcw.CLCW{
				Retransmit: true,
			},

			want: [4]byte{0x00, 0x00, 0x08, 0x00},
		},
		{
			desc: "FARMBCounter=3 ReportType=1",
			input: clcw.CLCW{
				FARMBCounter: 0x03,
				ReportType:   0x01,
			},

			want: [4]byte{0x00, 0x00, 0x07, 0x00},
		},
		{
			desc: "ReportValue N(R)=42",
			input: clcw.CLCW{
				ReportValue: 42,
			},
			want: [4]byte{0x00, 0x00, 0x00, 0x2A},
		},
		{
			desc: "VCID=63 (max)",
			input: clcw.CLCW{
				VCIDField: 0x3F,
			},

			want: [4]byte{0x00, 0xFC, 0x00, 0x00},
		},
		{
			desc: "StatusField=7 COPInEffect=3",
			input: clcw.CLCW{
				StatusField: 0x07,
				COPInEffect: 0x03,
			},

			want: [4]byte{0x1F, 0x00, 0x00, 0x00},
		},
		{
			desc: "realistic COP-1 normal operation VCID=1",
			input: clcw.CLCW{
				COPInEffect: 0,
				VCIDField:   1,
				ReportValue: 7,
			},

			want: [4]byte{0x00, 0x04, 0x00, 0x07},
		},
		{
			desc: "all flags set max counters",
			input: clcw.CLCW{
				StatusField:  0x07,
				COPInEffect:  0x03,
				VCIDField:    0x3F,
				NoRFAvail:    true,
				NoBitLock:    true,
				Lockout:      true,
				Wait:         true,
				Retransmit:   true,
				FARMBCounter: 0x03,
				ReportType:   0x01,
				ReportValue:  0xFF,
			},

			want: [4]byte{0x1F, 0xFC, 0xFF, 0xFF},
		},
		{
			desc:    "StatusField exceeds 3-bit max",
			input:   clcw.CLCW{StatusField: 0x08},
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "COPInEffect exceeds 2-bit max",
			input:   clcw.CLCW{COPInEffect: 0x04},
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "VCIDField exceeds 6-bit max",
			input:   clcw.CLCW{VCIDField: 0x40},
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "FARMBCounter exceeds 2-bit max",
			input:   clcw.CLCW{FARMBCounter: 0x04},
			wantErr: errors.ErrInvalidField,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := clcw.Encode(tc.input)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		desc    string
		input   []byte
		wantErr error
		check   func(t *testing.T, c clcw.CLCW)
	}{
		{
			desc:  "all-zero CLCW",
			input: []byte{0x00, 0x00, 0x00, 0x00},
			check: func(t *testing.T, c clcw.CLCW) {
				assert.Equal(t, uint8(0), c.StatusField)
				assert.Equal(t, uint8(0), c.VCIDField)
				assert.False(t, c.NoRFAvail)
				assert.False(t, c.Lockout)
				assert.Equal(t, uint8(0), c.ReportValue)
			},
		},
		{
			desc:  "NoRFAvail and NoBitLock",
			input: []byte{0x00, 0x00, 0xC0, 0x00},
			check: func(t *testing.T, c clcw.CLCW) {
				assert.True(t, c.NoRFAvail)
				assert.True(t, c.NoBitLock)
				assert.False(t, c.Lockout)
			},
		},
		{
			desc:  "all flags + max fields",
			input: []byte{0x1F, 0xFC, 0xFF, 0xFF},
			check: func(t *testing.T, c clcw.CLCW) {
				assert.Equal(t, uint8(0x07), c.StatusField)
				assert.Equal(t, uint8(0x03), c.COPInEffect)
				assert.Equal(t, uint8(0x3F), c.VCIDField)
				assert.True(t, c.NoRFAvail)
				assert.True(t, c.NoBitLock)
				assert.True(t, c.Lockout)
				assert.True(t, c.Wait)
				assert.True(t, c.Retransmit)
				assert.Equal(t, uint8(0x03), c.FARMBCounter)
				assert.Equal(t, uint8(0x01), c.ReportType)
				assert.Equal(t, uint8(0xFF), c.ReportValue)
			},
		},
		{
			desc:  "VCID=1 ReportValue=7",
			input: []byte{0x00, 0x04, 0x00, 0x07},
			check: func(t *testing.T, c clcw.CLCW) {
				assert.Equal(t, uint8(1), c.VCIDField)
				assert.Equal(t, uint8(7), c.ReportValue)
			},
		},
		{
			desc:    "too short — 3 bytes",
			input:   []byte{0x00, 0x00, 0x00},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "empty slice",
			input:   []byte{},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "control word type bit set (bit 7 of byte 0 = 1)",
			input:   []byte{0x80, 0x00, 0x00, 0x00},
			wantErr: errors.ErrBadVersion,
		},
		{
			desc:    "CLCW version mismatch (bits 6:5 of byte 0 = 0b01)",
			input:   []byte{0x20, 0x00, 0x00, 0x00},
			wantErr: errors.ErrBadVersion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := clcw.Decode(tc.input)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		desc  string
		input clcw.CLCW
	}{
		{
			desc:  "zero value",
			input: clcw.CLCW{},
		},
		{
			desc: "COP-1 normal lockout",
			input: clcw.CLCW{
				COPInEffect: 0,
				VCIDField:   3,
				Lockout:     true,
				ReportValue: 100,
			},
		},
		{
			desc: "all flags max counters",
			input: clcw.CLCW{
				StatusField:  0x07,
				COPInEffect:  0x03,
				VCIDField:    0x3F,
				NoRFAvail:    true,
				NoBitLock:    true,
				Lockout:      true,
				Wait:         true,
				Retransmit:   true,
				FARMBCounter: 0x03,
				ReportType:   0x01,
				ReportValue:  0xFF,
			},
		},
		{
			desc: "retransmit with N(R)=200",
			input: clcw.CLCW{
				VCIDField:   2,
				Retransmit:  true,
				ReportValue: 200,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			encoded, err := clcw.Encode(tc.input)
			require.NoError(t, err)

			decoded, err := clcw.Decode(encoded[:])
			require.NoError(t, err)

			assert.Equal(t, tc.input.StatusField, decoded.StatusField)
			assert.Equal(t, tc.input.COPInEffect, decoded.COPInEffect)
			assert.Equal(t, tc.input.VCIDField, decoded.VCIDField)
			assert.Equal(t, tc.input.NoRFAvail, decoded.NoRFAvail)
			assert.Equal(t, tc.input.NoBitLock, decoded.NoBitLock)
			assert.Equal(t, tc.input.Lockout, decoded.Lockout)
			assert.Equal(t, tc.input.Wait, decoded.Wait)
			assert.Equal(t, tc.input.Retransmit, decoded.Retransmit)
			assert.Equal(t, tc.input.FARMBCounter, decoded.FARMBCounter)
			assert.Equal(t, tc.input.ReportType, decoded.ReportType)
			assert.Equal(t, tc.input.ReportValue, decoded.ReportValue)
		})
	}
}
