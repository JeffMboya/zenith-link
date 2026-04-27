package tmframe_test

import (
	"testing"

	"github.com/absmach/zenith-link/pkg/ccsds/crc"
	"github.com/absmach/zenith-link/pkg/ccsds/spacepacket"
	"github.com/absmach/zenith-link/pkg/ccsds/tmframe"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultHeader() tmframe.PrimaryHeader {
	return tmframe.PrimaryHeader{
		SCID:                    0x5A,
		VCID:                    0,
		OCFFlag:                 false,
		MasterChannelFrameCount: 0,
		VirtualChannelFrameCount: 0,
		SyncFlag:                false,
		SegmentLengthID:         0b11,
		FirstHeaderPointer:      0,
	}
}

// ─── EncodePrimary ──────────────────────────────────────────────────────────

func TestEncodePrimary(t *testing.T) {
	tests := []struct {
		desc    string
		hdr     tmframe.PrimaryHeader
		want    [6]byte
		wantErr error
	}{
		{
			desc: "SCID=0x5A VCID=0 no OCF FHP=0",
			hdr:  defaultHeader(),
			// Byte 0-1: 00|01011010|000|0 = 0x0590?
			// TFVN(2)=00, SCID(10)=0x5A=0b01011010, VCID(3)=000, OCF=0
			// w0 = 00 | 0001011010 | 000 | 0
			//    = 0b00_0001011010_000_0 = 0x0590? Let me recalculate:
			// bits: 00 0001011010 000 0
			// = 0000 0101 1010 0000 = 0x05A0
			// Byte 2: MCFC=0, Byte 3: VCFC=0
			// Byte 4-5: 0|0|0|11|000_0000_0000 = 0b0_0_0_11_000_0000_0000 = 0x1800
			want: [6]byte{0x05, 0xA0, 0x00, 0x00, 0x18, 0x00},
		},
		{
			desc: "with OCF flag set",
			hdr: tmframe.PrimaryHeader{
				SCID:               0x5A,
				VCID:               0,
				OCFFlag:            true,
				SegmentLengthID:    0b11,
				FirstHeaderPointer: 0,
			},
			// w0 bit[0] = 1
			want: [6]byte{0x05, 0xA1, 0x00, 0x00, 0x18, 0x00},
		},
		{
			desc: "FHP = FHPNoPacketStart",
			hdr: tmframe.PrimaryHeader{
				SCID:               0x5A,
				SegmentLengthID:    0b11,
				FirstHeaderPointer: tmframe.FHPNoPacketStart,
			},
			// Byte 4-5 lower 11 bits = 0x07FE
			// With SegLenID=0b11: 0b0_0_0_11_111_1111_1110 = 0x1FFE
			want: [6]byte{0x05, 0xA0, 0x00, 0x00, 0x1F, 0xFE},
		},
		{
			desc: "MCFC and VCFC incremented",
			hdr: tmframe.PrimaryHeader{
				SCID:                    0x001,
				MasterChannelFrameCount: 100,
				VirtualChannelFrameCount: 200,
				SegmentLengthID:         0b11,
			},
			want: [6]byte{0x00, 0x10, 0x64, 0xC8, 0x18, 0x00},
		},
		{
			desc:    "SCID exceeds 10-bit maximum",
			hdr:     tmframe.PrimaryHeader{SCID: 0x0400},
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "VCID exceeds 3-bit maximum",
			hdr:     tmframe.PrimaryHeader{VCID: 8},
			wantErr: errors.ErrInvalidField,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tmframe.EncodePrimary(tc.hdr)
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
		check   func(t *testing.T, h tmframe.PrimaryHeader)
	}{
		{
			desc:  "valid minimal header",
			input: []byte{0x05, 0xA0, 0x00, 0x00, 0x18, 0x00},
			check: func(t *testing.T, h tmframe.PrimaryHeader) {
				assert.Equal(t, uint16(0x5A), h.SCID)
				assert.Equal(t, uint8(0), h.VCID)
				assert.False(t, h.OCFFlag)
				assert.Equal(t, uint16(0), h.FirstHeaderPointer)
			},
		},
		{
			desc:  "OCF flag set",
			input: []byte{0x05, 0xA1, 0x00, 0x00, 0x18, 0x00},
			check: func(t *testing.T, h tmframe.PrimaryHeader) {
				assert.True(t, h.OCFFlag)
			},
		},
		{
			desc:    "too short",
			input:   []byte{0x00, 0x00, 0x00},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "empty",
			input:   []byte{},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "bad version — bits 15:14 are 0b01",
			input:   []byte{0x40, 0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr: errors.ErrBadVersion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tmframe.DecodePrimary(tc.input)
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
	hdr := defaultHeader()

	tests := []struct {
		desc      string
		frame     tmframe.TransferFrame
		frameSize int
		wantErr   error
		check     func(t *testing.T, b []byte)
	}{
		{
			desc: "valid frame 264 bytes",
			frame: tmframe.TransferFrame{
				Primary:   hdr,
				DataField: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			},
			frameSize: 264,
			check: func(t *testing.T, b []byte) {
				assert.Equal(t, 264, len(b))
				assert.True(t, crc.Verify(b), "CRC should be valid")
			},
		},
		{
			desc: "frame size smaller than minimum",
			frame: tmframe.TransferFrame{
				Primary:   hdr,
				DataField: []byte{0x01},
			},
			frameSize: 5,
			wantErr:   errors.ErrFrameTooSmall,
		},
		{
			desc: "data field larger than available space",
			frame: tmframe.TransferFrame{
				Primary:   hdr,
				DataField: make([]byte, 300),
			},
			frameSize: 264,
			wantErr:   errors.ErrFrameTooLarge,
		},
		{
			desc: "with OCF flag and OCF data",
			frame: tmframe.TransferFrame{
				Primary: func() tmframe.PrimaryHeader {
					h := defaultHeader()
					h.OCFFlag = true
					return h
				}(),
				DataField: []byte{0x01},
				OCF:       &[4]byte{0x00, 0x01, 0x02, 0x03},
			},
			frameSize: 264,
			check: func(t *testing.T, b []byte) {
				assert.Equal(t, 264, len(b))
				assert.True(t, crc.Verify(b))
			},
		},
		{
			desc: "OCF flag set but OCF nil",
			frame: tmframe.TransferFrame{
				Primary: func() tmframe.PrimaryHeader {
					h := defaultHeader()
					h.OCFFlag = true
					return h
				}(),
				DataField: []byte{0x01},
				OCF:       nil,
			},
			frameSize: 264,
			wantErr:   errors.ErrInvalidField,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tmframe.Encode(tc.frame, tc.frameSize)
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

func TestDecode(t *testing.T) {
	goodFrame := func() []byte {
		f := tmframe.TransferFrame{
			Primary:   defaultHeader(),
			DataField: []byte{0xAB, 0xCD},
		}
		b, _ := tmframe.Encode(f, 128)
		return b
	}()

	tests := []struct {
		desc    string
		input   []byte
		wantErr error
		check   func(t *testing.T, f tmframe.TransferFrame)
	}{
		{
			desc:  "valid frame",
			input: goodFrame,
			check: func(t *testing.T, f tmframe.TransferFrame) {
				assert.Equal(t, uint16(0x5A), f.Primary.SCID)
			},
		},
		{
			desc:    "CRC corrupted",
			input:   func() []byte { b := make([]byte, len(goodFrame)); copy(b, goodFrame); b[len(b)-1] ^= 0xFF; return b }(),
			wantErr: errors.ErrCRCMismatch,
		},
		{
			desc:    "too short",
			input:   []byte{0x01, 0x02, 0x03},
			wantErr: errors.ErrFrameTooSmall,
		},
		{
			desc:    "empty",
			input:   []byte{},
			wantErr: errors.ErrFrameTooSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tmframe.Decode(tc.input)
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
	hdr := tmframe.PrimaryHeader{
		SCID:                    0x5A,
		VCID:                    2,
		OCFFlag:                 false,
		MasterChannelFrameCount: 77,
		VirtualChannelFrameCount: 33,
		SegmentLengthID:         0b11,
		FirstHeaderPointer:      0,
	}
	f := tmframe.TransferFrame{
		Primary:   hdr,
		DataField: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}

	encoded, err := tmframe.Encode(f, 256)
	require.NoError(t, err)

	decoded, err := tmframe.Decode(encoded)
	require.NoError(t, err)

	assert.Equal(t, hdr.SCID, decoded.Primary.SCID)
	assert.Equal(t, hdr.VCID, decoded.Primary.VCID)
	assert.Equal(t, hdr.MasterChannelFrameCount, decoded.Primary.MasterChannelFrameCount)
	assert.Equal(t, hdr.VirtualChannelFrameCount, decoded.Primary.VirtualChannelFrameCount)
	// Data field is zero-padded to fill the frame.
	assert.Equal(t, f.DataField, decoded.DataField[:len(f.DataField)])
}

// ─── ExtractSpacePackets ─────────────────────────────────────────────────────

func buildSpacePacket(apid uint16, userData []byte) []byte {
	pkt := spacepacket.SpacePacket{
		Primary: spacepacket.PrimaryHeader{
			Type:          spacepacket.Telemetry,
			APID:          apid,
			GroupingFlags: spacepacket.Unsegmented,
		},
		UserData: userData,
	}
	b, _ := spacepacket.Encode(pkt)
	return b
}

func TestExtractSpacePackets(t *testing.T) {
	pkt1 := buildSpacePacket(0x100, []byte{0x01, 0x02, 0x03})
	pkt2 := buildSpacePacket(0x101, []byte{0x04, 0x05})

	tests := []struct {
		desc      string
		dataField []byte
		fhp       uint16
		pending   []byte
		wantPkts  int
		wantErr   bool
	}{
		{
			desc:      "FHP=0 single complete packet",
			dataField: pkt1,
			fhp:       0,
			wantPkts:  1,
		},
		{
			desc:      "FHP=0 two consecutive packets",
			dataField: append(append([]byte{}, pkt1...), pkt2...),
			fhp:       0,
			wantPkts:  2,
		},
		{
			desc:      "FHP=FHPOnlyIdle — no extraction",
			dataField: pkt1,
			fhp:       tmframe.FHPOnlyIdle,
			wantPkts:  0,
		},
		{
			desc: "pending fragment completed by this frame",
			// Split pkt1 at byte 5: first 5 bytes as pending
			pending:   pkt1[:5],
			dataField: pkt1[5:],
			fhp:       tmframe.FHPNoPacketStart,
			wantPkts:  1,
		},
		{
			desc:      "FHP skips leading fragment bytes",
			dataField: append([]byte{0xFF, 0xFF}, pkt1...),
			fhp:       2, // first 2 bytes are tail of previous packet
			wantPkts:  1,
		},
		{
			desc:      "FHPNoPacketStart with no pending — discards entire data field",
			dataField: pkt1,
			fhp:       tmframe.FHPNoPacketStart,
			pending:   nil,
			wantPkts:  0,
		},
		{
			desc:      "complete pending packet (remaining=0) emits packet without error",
			// pkt1 is 9 bytes; passing it as pending with empty dataField means
			// remaining=0 → packet is already complete and should be emitted.
			pending:   append([]byte{}, pkt1...),
			dataField: []byte{},
			fhp:       tmframe.FHPNoPacketStart,
			wantPkts:  1,
		},
		{
			desc: "malformed pending packet: more bytes buffered than declared length — returns error",
			// pkt1 is 9 bytes with PacketDataLength=2 (totalLen=9).
			// Appending one extra byte gives len(pending)=10 > totalLen=9 → remaining=-1 → error.
			pending:   append(append([]byte{}, pkt1...), 0xFF),
			dataField: []byte{},
			fhp:       tmframe.FHPNoPacketStart,
			wantPkts:  0,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			pkts, _, err := tmframe.ExtractSpacePackets(tc.dataField, tc.fhp, tc.pending)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPkts, len(pkts))
		})
	}
}
