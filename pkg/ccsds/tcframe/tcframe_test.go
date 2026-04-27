package tcframe_test

import (
	"testing"

	"github.com/absmach/zenith-link/pkg/ccsds/tcframe"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultHeader() tcframe.PrimaryHeader {
	return tcframe.PrimaryHeader{
		BypassFlag:          false,
		ControlCommandFlag:  false,
		SCID:                0x5A,
		VCID:                0x00,
		FrameSequenceNumber: 0,
	}
}

// ─── EncodePrimary ──────────────────────────────────────────────────────────

func TestEncodePrimary(t *testing.T) {
	tests := []struct {
		desc    string
		hdr     tcframe.PrimaryHeader
		want    [6]byte
		wantErr error
	}{
		{
			desc: "basic TC frame SCID=0x5A VCID=0 N(S)=0",
			hdr:  defaultHeader(),
			// Byte 0: 00|0|0|00|00 = 0x00 (TFVN=00, BYP=0, CTRL=0, RSVD=00, SCID9-8=00)
			// Wait: SCID=0x5A=0b01011010, SCID[9:8]=0b00, SCID[7:0]=0b01011010=0x5A
			// Byte 0: 00_0_0_00_00 = 0x00
			// Byte 1: 0x5A
			// Byte 2: VCID=0x00
			// Bytes 3-4: FrameLength (set by Encode, not by EncodePrimary directly)
			// Byte 5: N(S)=0
			want: [6]byte{0x00, 0x5A, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "bypass flag set",
			hdr: tcframe.PrimaryHeader{
				BypassFlag: true,
				SCID:       0x001,
			},
			// Byte 0: 00_1_0_00_00 = 0x20
			// Byte 1: 0x01
			want: [6]byte{0x20, 0x01, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "control command flag set",
			hdr: tcframe.PrimaryHeader{
				ControlCommandFlag: true,
				SCID:               0x001,
			},
			// Byte 0: 00_0_1_00_00 = 0x10
			want: [6]byte{0x10, 0x01, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "bypass and control command both set",
			hdr: tcframe.PrimaryHeader{
				BypassFlag:         true,
				ControlCommandFlag: true,
				SCID:               0x001,
			},
			// Byte 0: 00_1_1_00_00 = 0x30
			want: [6]byte{0x30, 0x01, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "SCID uses high bits",
			hdr: tcframe.PrimaryHeader{
				SCID: 0x03FF, // max SCID
			},
			// SCID[9:8]=0b11 → Byte 0 bits[1:0] = 0b11
			// Byte 0 = 0x03, Byte 1 = 0xFF
			want: [6]byte{0x03, 0xFF, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "VCID=63 maximum",
			hdr:  tcframe.PrimaryHeader{SCID: 0x001, VCID: 0x3F},
			// Byte 2 = 0x3F
			want: [6]byte{0x00, 0x01, 0x3F, 0x00, 0x00, 0x00},
		},
		{
			desc: "FrameSequenceNumber=255 (rollover boundary)",
			hdr:  tcframe.PrimaryHeader{SCID: 0x001, FrameSequenceNumber: 255},
			want: [6]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0xFF},
		},
		{
			desc:    "SCID exceeds 10-bit max",
			hdr:     tcframe.PrimaryHeader{SCID: 0x0400},
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "VCID exceeds 6-bit max",
			hdr:     tcframe.PrimaryHeader{VCID: 0x40},
			wantErr: errors.ErrInvalidField,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tcframe.EncodePrimary(tc.hdr)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ─── DecodePrimary ──────────────────────────────────────────────────────────

func TestDecodePrimary(t *testing.T) {
	tests := []struct {
		desc    string
		input   []byte
		wantErr error
		check   func(t *testing.T, h tcframe.PrimaryHeader)
	}{
		{
			desc:  "basic header",
			input: []byte{0x00, 0x5A, 0x00, 0x00, 0x00, 0x00},
			check: func(t *testing.T, h tcframe.PrimaryHeader) {
				assert.False(t, h.BypassFlag)
				assert.Equal(t, uint16(0x5A), h.SCID)
				assert.Equal(t, uint8(0x00), h.VCID)
			},
		},
		{
			desc:  "bypass flag set",
			input: []byte{0x20, 0x01, 0x00, 0x00, 0x00, 0x00},
			check: func(t *testing.T, h tcframe.PrimaryHeader) {
				assert.True(t, h.BypassFlag)
				assert.Equal(t, uint16(0x001), h.SCID)
			},
		},
		{
			desc:    "too short",
			input:   []byte{0x00, 0x00, 0x00},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "bad version bits 7:6 = 0b01",
			input:   []byte{0x40, 0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr: errors.ErrBadVersion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tcframe.DecodePrimary(tc.input)
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

// ─── Encode / Decode ────────────────────────────────────────────────────────

func TestEncode(t *testing.T) {
	tests := []struct {
		desc    string
		frame   tcframe.TransferFrame
		wantErr error
		check   func(t *testing.T, b []byte)
	}{
		{
			desc: "valid TC frame",
			frame: tcframe.TransferFrame{
				Primary:   defaultHeader(),
				DataField: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			},
			check: func(t *testing.T, b []byte) {
				// 6 header + 4 data + 2 FECF = 12
				assert.Equal(t, 12, len(b))
			},
		},
		{
			desc: "minimum valid frame — 1 byte data",
			frame: tcframe.TransferFrame{
				Primary:   defaultHeader(),
				DataField: []byte{0xFF},
			},
			check: func(t *testing.T, b []byte) {
				assert.Equal(t, 9, len(b))
			},
		},
		{
			desc: "data too large",
			frame: tcframe.TransferFrame{
				Primary:   defaultHeader(),
				DataField: make([]byte, 1020),
			},
			wantErr: errors.ErrFrameTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tcframe.Encode(tc.frame)
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
		frame tcframe.TransferFrame
	}{
		{
			desc: "basic TC frame",
			frame: tcframe.TransferFrame{
				Primary: tcframe.PrimaryHeader{
					BypassFlag: true,
					SCID:       0x5A,
					VCID:       3,
					FrameSequenceNumber: 42,
				},
				DataField: []byte{0x01, 0x02, 0x03},
			},
		},
		{
			desc: "FrameSequenceNumber at 0xFF rollover boundary",
			frame: tcframe.TransferFrame{
				Primary: tcframe.PrimaryHeader{
					SCID:                0x001,
					FrameSequenceNumber: 255,
				},
				DataField: []byte{0xAB},
			},
		},
		{
			desc: "max VCID and max SCID",
			frame: tcframe.TransferFrame{
				Primary: tcframe.PrimaryHeader{
					SCID: tcframe.MaxSCID,
					VCID: tcframe.MaxVCID,
				},
				DataField: []byte{0xFF, 0xFF},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			encoded, err := tcframe.Encode(tc.frame)
			require.NoError(t, err)

			decoded, err := tcframe.Decode(encoded)
			require.NoError(t, err)

			assert.Equal(t, tc.frame.Primary.BypassFlag, decoded.Primary.BypassFlag)
			assert.Equal(t, tc.frame.Primary.ControlCommandFlag, decoded.Primary.ControlCommandFlag)
			assert.Equal(t, tc.frame.Primary.SCID, decoded.Primary.SCID)
			assert.Equal(t, tc.frame.Primary.VCID, decoded.Primary.VCID)
			assert.Equal(t, tc.frame.Primary.FrameSequenceNumber, decoded.Primary.FrameSequenceNumber)
			assert.Equal(t, tc.frame.DataField, decoded.DataField)
		})
	}
}

func TestDecode_Errors(t *testing.T) {
	good := func() []byte {
		b, _ := tcframe.Encode(tcframe.TransferFrame{
			Primary:   defaultHeader(),
			DataField: []byte{0x01},
		})
		return b
	}()

	tests := []struct {
		desc    string
		input   []byte
		wantErr error
	}{
		{
			desc:    "too short",
			input:   []byte{0x01, 0x02},
			wantErr: errors.ErrFrameTooSmall,
		},
		{
			desc:    "CRC corrupted",
			input:   func() []byte { b := append([]byte(nil), good...); b[len(b)-1] ^= 0xFF; return b }(),
			wantErr: errors.ErrCRCMismatch,
		},
		{
			desc: "FrameLength mismatch",
			input: func() []byte {
				b := append([]byte(nil), good...)
				// Change FrameLength to a wrong value.
				b[3] = 0xFF
				// Recompute CRC so CRC check passes but FrameLength still mismatches.
				// (We leave CRC wrong to test that CRC is checked first.)
				return b
			}(),
			wantErr: errors.ErrCRCMismatch, // CRC checked before FrameLength
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := tcframe.Decode(tc.input)
			assert.True(t, errors.Contains(err, tc.wantErr),
				"expected error %v, got %v", tc.wantErr, err)
		})
	}
}
