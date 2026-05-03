package tcframe_test

import (
	"testing"

	"github.com/absmach/orbitron/pkg/ccsds/tcframe"
	"github.com/absmach/orbitron/pkg/errors"
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

			want: [6]byte{0x00, 0x5A, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "bypass flag set",
			hdr: tcframe.PrimaryHeader{
				BypassFlag: true,
				SCID:       0x001,
			},

			want: [6]byte{0x20, 0x01, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "control command flag set",
			hdr: tcframe.PrimaryHeader{
				ControlCommandFlag: true,
				SCID:               0x001,
			},

			want: [6]byte{0x10, 0x01, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "bypass and control command both set",
			hdr: tcframe.PrimaryHeader{
				BypassFlag:         true,
				ControlCommandFlag: true,
				SCID:               0x001,
			},

			want: [6]byte{0x30, 0x01, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "SCID uses high bits",
			hdr: tcframe.PrimaryHeader{
				SCID: 0x03FF,
			},

			want: [6]byte{0x03, 0xFF, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "VCID=63 maximum",
			hdr:  tcframe.PrimaryHeader{SCID: 0x001, VCID: 0x3F},

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
					BypassFlag:          true,
					SCID:                0x5A,
					VCID:                3,
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

				b[3] = 0xFF

				return b
			}(),
			wantErr: errors.ErrCRCMismatch,
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
