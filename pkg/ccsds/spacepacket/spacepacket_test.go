package spacepacket_test

import (
	"testing"
	"time"

	"github.com/absmach/zenith-link/pkg/ccsds/spacepacket"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── EncodePrimary ──────────────────────────────────────────────────────────

func TestEncodePrimary(t *testing.T) {
	tests := []struct {
		desc    string
		hdr     spacepacket.PrimaryHeader
		want    [6]byte
		wantErr error
	}{
		{
			desc: "telemetry APID=0x100 standalone seq=0 no secondary header",
			hdr: spacepacket.PrimaryHeader{
				Type:               spacepacket.Telemetry,
				HasSecondaryHeader: false,
				APID:               0x100,
				GroupingFlags:      spacepacket.Unsegmented,
				SequenceCount:      0,
				PacketDataLength:   7,
			},
			// Byte 0-1: 000|0|0|001_0000_0000 = 0x0100
			// Byte 2-3: 11|00_0000_0000_0000  = 0xC000
			// Byte 4-5: 0x0007
			want: [6]byte{0x01, 0x00, 0xC0, 0x00, 0x00, 0x07},
		},
		{
			desc: "telemetry APID=0x100 with secondary header",
			hdr: spacepacket.PrimaryHeader{
				Type:               spacepacket.Telemetry,
				HasSecondaryHeader: true,
				APID:               0x100,
				GroupingFlags:      spacepacket.Unsegmented,
				SequenceCount:      0,
				PacketDataLength:   7,
			},
			// Byte 0-1: 000|0|1|001_0000_0000 = 0x0900
			want: [6]byte{0x09, 0x00, 0xC0, 0x00, 0x00, 0x07},
		},
		{
			desc: "telecommand APID=0x001 first segment seq=1",
			hdr: spacepacket.PrimaryHeader{
				Type:               spacepacket.Telecommand,
				HasSecondaryHeader: false,
				APID:               0x001,
				GroupingFlags:      spacepacket.FirstSegment,
				SequenceCount:      1,
				PacketDataLength:   0,
			},
			// Byte 0-1: 000|1|0|000_0000_0001 = 0x1001
			// Byte 2-3: 01|00_0000_0000_0001  = 0x4001
			want: [6]byte{0x10, 0x01, 0x40, 0x01, 0x00, 0x00},
		},
		{
			desc: "APID at maximum 0x7FF idle packet",
			hdr: spacepacket.PrimaryHeader{
				APID:          spacepacket.MaxAPID,
				GroupingFlags: spacepacket.Unsegmented,
			},
			// Byte 0-1: 000|0|0|111_1111_1111 = 0x07FF
			want: [6]byte{0x07, 0xFF, 0xC0, 0x00, 0x00, 0x00},
		},
		{
			desc: "SequenceCount at maximum 0x3FFF",
			hdr: spacepacket.PrimaryHeader{
				GroupingFlags: spacepacket.Unsegmented,
				SequenceCount: spacepacket.MaxSeqCount,
			},
			// Byte 2-3: 11|11_1111_1111_1111 = 0xFFFF
			want: [6]byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00},
		},
		{
			desc: "GroupingFlags continuation",
			hdr: spacepacket.PrimaryHeader{
				GroupingFlags: spacepacket.Continuation,
			},
			// Byte 2-3: 00|00_0000_0000_0000 = 0x0000
			want: [6]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			desc: "GroupingFlags last segment",
			hdr: spacepacket.PrimaryHeader{
				GroupingFlags: spacepacket.LastSegment,
			},
			// Byte 2-3: 10|00_0000_0000_0000 = 0x8000
			want: [6]byte{0x00, 0x00, 0x80, 0x00, 0x00, 0x00},
		},
		{
			desc:    "APID exceeds 11-bit max",
			hdr:     spacepacket.PrimaryHeader{APID: 0x0800},
			wantErr: errors.ErrInvalidAPID,
		},
		{
			desc:    "SequenceCount exceeds 14-bit max",
			hdr:     spacepacket.PrimaryHeader{SequenceCount: 0x4000},
			wantErr: errors.ErrInvalidSeqCount,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := spacepacket.EncodePrimary(tc.hdr)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr),
					"expected %v to contain %v", err, tc.wantErr)
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
		want    spacepacket.PrimaryHeader
		wantErr error
	}{
		{
			desc:  "TM with secondary header",
			input: []byte{0x09, 0x00, 0xC0, 0x00, 0x00, 0x07},
			want: spacepacket.PrimaryHeader{
				Type:               spacepacket.Telemetry,
				HasSecondaryHeader: true,
				APID:               0x100,
				GroupingFlags:      spacepacket.Unsegmented,
				SequenceCount:      0,
				PacketDataLength:   7,
			},
		},
		{
			desc:  "TC no secondary header",
			input: []byte{0x10, 0x01, 0x40, 0x01, 0x00, 0x00},
			want: spacepacket.PrimaryHeader{
				Type:               spacepacket.Telecommand,
				HasSecondaryHeader: false,
				APID:               0x001,
				GroupingFlags:      spacepacket.FirstSegment,
				SequenceCount:      1,
				PacketDataLength:   0,
			},
		},
		{
			desc:  "all zeros — version valid, rest zero",
			input: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			want: spacepacket.PrimaryHeader{
				GroupingFlags: spacepacket.Continuation,
			},
		},
		{
			desc:  "max APID idle packet",
			input: []byte{0x07, 0xFF, 0xC0, 0x00, 0x00, 0x00},
			want: spacepacket.PrimaryHeader{
				APID:          spacepacket.MaxAPID,
				GroupingFlags: spacepacket.Unsegmented,
			},
		},
		{
			desc:    "too short — 5 bytes",
			input:   []byte{0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "empty input",
			input:   []byte{},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "invalid version — bit 15 set",
			input:   []byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr: errors.ErrBadVersion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := spacepacket.DecodePrimary(tc.input)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ─── CDS Timestamp ──────────────────────────────────────────────────────────

func TestCDSFromTime(t *testing.T) {
	tests := []struct {
		desc    string
		input   time.Time
		wantDay uint16
		wantMs  uint32
	}{
		{
			desc:    "Unix epoch — days=4383 from CDS epoch, ms=0",
			input:   time.Unix(0, 0).UTC(),
			wantDay: 4383,
			wantMs:  0,
		},
		{
			desc:    "One day after Unix epoch",
			input:   time.Unix(86400, 0).UTC(),
			wantDay: 4384,
			wantMs:  0,
		},
		{
			desc:    "Unix epoch + 1 second",
			input:   time.Unix(1, 0).UTC(),
			wantDay: 4383,
			wantMs:  1000,
		},
		{
			desc:    "Unix epoch + 1 minute",
			input:   time.Unix(60, 0).UTC(),
			wantDay: 4383,
			wantMs:  60_000,
		},
		{
			desc:  "2024-01-01T00:00:00 UTC",
			input: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			// Days from 1958-01-01 to 2024-01-01:
			// = 4383 (to Unix epoch 1970-01-01) + 54*365 + 14 leap years
			// 54 years 1970→2024: leap years: 1972,1976,1980,1984,1988,1992,1996,2000,2004,2008,2012,2016,2020 = 13
			// = 4383 + 54*365 + 13 = 4383 + 19710 + 13 = 24106
			wantDay: 24106, // = days since 1958-01-01 for 2024-01-01
			wantMs:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := spacepacket.CDSFromTime(tc.input)
			assert.Equal(t, tc.wantDay, got.Days, "Days mismatch")
			assert.Equal(t, tc.wantMs, got.MillisecondsDay, "MillisecondsDay mismatch")
		})
	}
}

func TestCDSRoundTrip(t *testing.T) {
	cases := []time.Time{
		time.Unix(0, 0).UTC(),
		time.Unix(1_700_000_000, 0).UTC(),
		time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC),
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, ts := range cases {
		t.Run(ts.String(), func(t *testing.T) {
			// Sub-millisecond precision is rounded in CDS encoding,
			// so compare only to millisecond granularity.
			encoded := spacepacket.CDSFromTime(ts)
			decoded := spacepacket.CDSToTime(encoded)
			diff := ts.Sub(decoded)
			if diff < 0 {
				diff = -diff
			}
			assert.Less(t, diff.Nanoseconds(), int64(time.Millisecond),
				"round-trip error %v exceeds 1 ms for input %v", diff, ts)
		})
	}
}

// ─── Full Packet Encode/Decode ───────────────────────────────────────────────

func TestEncode(t *testing.T) {
	sh := spacepacket.CDSFromTime(time.Unix(0, 0).UTC())

	tests := []struct {
		desc     string
		pkt      spacepacket.SpacePacket
		wantLen  int
		wantErr  error
	}{
		{
			desc: "TM with secondary header and user data",
			pkt: spacepacket.SpacePacket{
				Primary: spacepacket.PrimaryHeader{
					Type:               spacepacket.Telemetry,
					HasSecondaryHeader: true,
					APID:               0x100,
					GroupingFlags:      spacepacket.Unsegmented,
				},
				Secondary: &sh,
				UserData:  []byte{0xDE, 0xAD, 0xBE, 0xEF},
			},
			// 6 primary + 8 secondary + 4 user data = 18
			wantLen: 18,
		},
		{
			desc: "TM no secondary header with user data",
			pkt: spacepacket.SpacePacket{
				Primary: spacepacket.PrimaryHeader{
					Type:          spacepacket.Telemetry,
					APID:          0x200,
					GroupingFlags: spacepacket.Unsegmented,
				},
				UserData: []byte{0x01},
			},
			// 6 primary + 1 user data = 7
			wantLen: 7,
		},
		{
			desc: "secondary header flag set but Secondary nil",
			pkt: spacepacket.SpacePacket{
				Primary: spacepacket.PrimaryHeader{
					HasSecondaryHeader: true,
					APID:               0x100,
				},
				Secondary: nil,
				UserData:  []byte{0x01},
			},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc: "empty user data and no secondary header",
			pkt: spacepacket.SpacePacket{
				Primary: spacepacket.PrimaryHeader{APID: 0x100},
			},
			wantErr: errors.ErrMalformedFrame,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := spacepacket.Encode(tc.pkt)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLen, len(got))
		})
	}
}

func TestDecode(t *testing.T) {
	goodPacket := func() []byte {
		sh := spacepacket.CDSFromTime(time.Unix(1_700_000_000, 0).UTC())
		pkt := spacepacket.SpacePacket{
			Primary: spacepacket.PrimaryHeader{
				Type:               spacepacket.Telemetry,
				HasSecondaryHeader: true,
				APID:               0x100,
				GroupingFlags:      spacepacket.Unsegmented,
				SequenceCount:      42,
			},
			Secondary: &sh,
			UserData:  []byte{0xAB, 0xCD, 0xEF},
		}
		b, _ := spacepacket.Encode(pkt)
		return b
	}()

	tests := []struct {
		desc    string
		input   []byte
		wantErr error
	}{
		{
			desc:  "valid TM packet with secondary header",
			input: goodPacket,
		},
		{
			desc:    "empty input",
			input:   []byte{},
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "truncated at primary header — 3 bytes",
			input:   goodPacket[:3],
			wantErr: errors.ErrMalformedFrame,
		},
		{
			desc:    "truncated before secondary header end",
			input:   goodPacket[:spacepacket.PrimaryHeaderSize+4],
			wantErr: errors.ErrDataLenMismatch,
		},
		{
			desc:    "DataLength field claims more bytes than buffer holds",
			input:   append(goodPacket[:5:5], 0xFF, 0xFF), // claim 65535 data bytes
			wantErr: errors.ErrDataLenMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := spacepacket.Decode(tc.input)
			if tc.wantErr != nil {
				assert.True(t, errors.Contains(err, tc.wantErr))
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	ts := time.Date(2024, 3, 14, 15, 9, 26, 535_000_000, time.UTC)
	sh := spacepacket.CDSFromTime(ts)

	tests := []struct {
		desc string
		pkt  spacepacket.SpacePacket
	}{
		{
			desc: "TM with secondary header",
			pkt: spacepacket.SpacePacket{
				Primary: spacepacket.PrimaryHeader{
					Type:               spacepacket.Telemetry,
					HasSecondaryHeader: true,
					APID:               0x100,
					GroupingFlags:      spacepacket.Unsegmented,
					SequenceCount:      1234,
				},
				Secondary: &sh,
				UserData:  []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			},
		},
		{
			desc: "TC without secondary header",
			pkt: spacepacket.SpacePacket{
				Primary: spacepacket.PrimaryHeader{
					Type:          spacepacket.Telecommand,
					APID:          0x001,
					GroupingFlags: spacepacket.Unsegmented,
					SequenceCount: 0x3FFF,
				},
				UserData: []byte{0xDE, 0xAD},
			},
		},
		{
			desc: "idle packet APID=0x7FF",
			pkt: spacepacket.SpacePacket{
				Primary: spacepacket.PrimaryHeader{
					APID:          spacepacket.IdleAPID,
					GroupingFlags: spacepacket.Unsegmented,
				},
				UserData: []byte{0xFF},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			encoded, err := spacepacket.Encode(tc.pkt)
			require.NoError(t, err)

			decoded, err := spacepacket.Decode(encoded)
			require.NoError(t, err)

			assert.Equal(t, tc.pkt.Primary.Type, decoded.Primary.Type)
			assert.Equal(t, tc.pkt.Primary.HasSecondaryHeader, decoded.Primary.HasSecondaryHeader)
			assert.Equal(t, tc.pkt.Primary.APID, decoded.Primary.APID)
			assert.Equal(t, tc.pkt.Primary.GroupingFlags, decoded.Primary.GroupingFlags)
			assert.Equal(t, tc.pkt.Primary.SequenceCount, decoded.Primary.SequenceCount)
			assert.Equal(t, tc.pkt.UserData, decoded.UserData)

			if tc.pkt.Secondary != nil {
				require.NotNil(t, decoded.Secondary)
				assert.Equal(t, tc.pkt.Secondary.Days, decoded.Secondary.Days)
				assert.Equal(t, tc.pkt.Secondary.MillisecondsDay, decoded.Secondary.MillisecondsDay)
			}
		})
	}
}
