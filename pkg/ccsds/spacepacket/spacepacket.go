// Package spacepacket implements the CCSDS Space Packet Protocol per
// CCSDS 133.0-B-2 (Space Packet Protocol, Blue Book).
//
// Wire layout of a Version-1 Space Packet:
//
//	┌──────────────────────── Primary Header (6 bytes) ─────────────────────────┐
//	│ bits [15:13] Version (000) │ [12] Type │ [11] SHF │ [10:0] APID          │
//	│ bits [15:14] GroupingFlags │ [13:0] SequenceCount                         │
//	│ bits [15:0]  PacketDataLength  (Packet Data Field length − 1)             │
//	└────────────────────────────────────────────────────────────────────────────┘
//	┌───── Secondary Header (8 bytes, present when SHF=1) ──────────────────────┐
//	│ Days (uint16) │ MillisecondsOfDay (uint32) │ SubMilliseconds (uint16)     │
//	│ CCSDS Day Segmented (CDS) time code, epoch = 1958-01-01T00:00:00 TAI      │
//	└────────────────────────────────────────────────────────────────────────────┘
//	┌─── User Data (variable) ────────────────────────────────────────────────────┐
//	│ Mission-defined payload bytes                                               │
//	└─────────────────────────────────────────────────────────────────────────────┘
//
// PacketDataLength is: len(SecondaryHeader) + len(UserData) − 1.
// The primary header itself is NOT included in PacketDataLength.
package spacepacket

import (
	"encoding/binary"
	"time"

	"github.com/absmach/zenith-link/pkg/errors"
)

// Protocol constants.
const (
	Version            uint8  = 0       // Version-1 per CCSDS 133.0-B-2
	PrimaryHeaderSize         = 6       // bytes
	SecondaryHeaderSize       = 8       // bytes (CDS format)
	MaxAPID            uint16 = 0x07FF  // 11-bit field maximum; 0x7FF = idle packet
	IdleAPID           uint16 = 0x07FF
	MaxSeqCount        uint16 = 0x3FFF  // 14-bit field maximum (16383)
	MaxDataLength      uint16 = 0xFFFF  // 16-bit field maximum
)

// PacketType distinguishes telemetry (downlink) from telecommand (uplink).
type PacketType uint8

const (
	Telemetry    PacketType = 0
	Telecommand  PacketType = 1
)

// GroupingFlags describes the position of a packet within a sequence.
type GroupingFlags uint8

const (
	Continuation  GroupingFlags = 0b00
	FirstSegment  GroupingFlags = 0b01
	LastSegment   GroupingFlags = 0b10
	Unsegmented   GroupingFlags = 0b11
)

// ccsdsCDSEpochDays is the number of days between the CCSDS CDS epoch
// (1958-01-01T00:00:00 TAI) and the Unix epoch (1970-01-01T00:00:00 UTC).
// Computed as: 12 years × 365.25 days/year ≈ 4383 days.
// (Exact: 1958→1970 = 365+365+366+365+365+365+366+365+365+365+366+365 = 4383 days)
const ccsdsCDSEpochDays = 4383

// PrimaryHeader holds the decoded fields of the 6-byte CCSDS primary header.
type PrimaryHeader struct {
	Type                PacketType
	HasSecondaryHeader  bool
	APID                uint16
	GroupingFlags       GroupingFlags
	SequenceCount       uint16
	// PacketDataLength is the value stored in the wire field:
	// len(SecondaryHeader)+len(UserData)−1. Callers normally use Packet.Encode
	// which computes this automatically.
	PacketDataLength uint16
}

// SecondaryHeader is a CCSDS Day Segmented (CDS) time code.
type SecondaryHeader struct {
	Days            uint16 // days since 1958-01-01
	MillisecondsDay uint32 // milliseconds since midnight (0–86_399_999)
	SubMilliseconds uint16 // sub-ms resolution: 1 unit = 1/65536 ms ≈ 15.26 ns
}

// SpacePacket is a fully decoded CCSDS Space Packet.
type SpacePacket struct {
	Primary   PrimaryHeader
	Secondary *SecondaryHeader // nil when HasSecondaryHeader == false
	UserData  []byte
}

// EncodePrimary serialises the primary header into exactly 6 bytes.
// It does NOT compute PacketDataLength — callers must set it correctly.
func EncodePrimary(h PrimaryHeader) ([PrimaryHeaderSize]byte, error) {
	if h.APID > MaxAPID {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidAPID, errors.New("value exceeds 11-bit maximum 0x7FF"))
	}
	if h.SequenceCount > MaxSeqCount {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidSeqCount, errors.New("value exceeds 14-bit maximum 0x3FFF"))
	}

	var b [PrimaryHeaderSize]byte

	// Octet 0-1: version(3) | type(1) | shf(1) | APID(11)
	w0 := uint16(Version)<<13 |
		uint16(h.Type)<<12 |
		boolBit(h.HasSecondaryHeader)<<11 |
		h.APID&MaxAPID
	binary.BigEndian.PutUint16(b[0:], w0)

	// Octet 2-3: grouping_flags(2) | sequence_count(14)
	w1 := uint16(h.GroupingFlags)<<14 | h.SequenceCount&MaxSeqCount
	binary.BigEndian.PutUint16(b[2:], w1)

	// Octet 4-5: packet_data_length(16)
	binary.BigEndian.PutUint16(b[4:], h.PacketDataLength)

	return b, nil
}

// DecodePrimary parses exactly 6 bytes into a PrimaryHeader.
func DecodePrimary(b []byte) (PrimaryHeader, error) {
	if len(b) < PrimaryHeaderSize {
		return PrimaryHeader{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("primary header requires 6 bytes"))
	}

	w0 := binary.BigEndian.Uint16(b[0:])
	ver := uint8(w0 >> 13)
	if ver != Version {
		return PrimaryHeader{}, errors.Wrap(errors.ErrBadVersion,
			errors.New("only Version-1 (0b000) is supported"))
	}

	h := PrimaryHeader{
		Type:               PacketType((w0 >> 12) & 0x01),
		HasSecondaryHeader: (w0>>11)&0x01 == 1,
		APID:               w0 & MaxAPID,
	}

	w1 := binary.BigEndian.Uint16(b[2:])
	h.GroupingFlags = GroupingFlags((w1 >> 14) & 0x03)
	h.SequenceCount = w1 & MaxSeqCount

	h.PacketDataLength = binary.BigEndian.Uint16(b[4:])
	return h, nil
}

// EncodeSecondary serialises a SecondaryHeader into exactly 8 bytes.
func EncodeSecondary(h SecondaryHeader) [SecondaryHeaderSize]byte {
	var b [SecondaryHeaderSize]byte
	binary.BigEndian.PutUint16(b[0:], h.Days)
	binary.BigEndian.PutUint32(b[2:], h.MillisecondsDay)
	binary.BigEndian.PutUint16(b[6:], h.SubMilliseconds)
	return b
}

// DecodeSecondary parses 8 bytes into a SecondaryHeader.
func DecodeSecondary(b []byte) (SecondaryHeader, error) {
	if len(b) < SecondaryHeaderSize {
		return SecondaryHeader{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("secondary header requires 8 bytes"))
	}
	return SecondaryHeader{
		Days:            binary.BigEndian.Uint16(b[0:]),
		MillisecondsDay: binary.BigEndian.Uint32(b[2:]),
		SubMilliseconds: binary.BigEndian.Uint16(b[6:]),
	}, nil
}

// CDSFromTime converts a time.Time to a CCSDS CDS secondary header.
func CDSFromTime(t time.Time) SecondaryHeader {
	t = t.UTC()
	// Days since CDS epoch
	unixSec := t.Unix()
	totalSec := unixSec + int64(ccsdsCDSEpochDays)*86400
	days := uint16(totalSec / 86400)

	secOfDay := totalSec % 86400
	msOfDay := uint32(secOfDay*1000) + uint32(t.Nanosecond()/int(time.Millisecond))

	// Sub-milliseconds: nanoseconds within current ms → units of 1/65536 ms
	nsWithinMs := t.Nanosecond() % int(time.Millisecond)
	subMs := uint16(int64(nsWithinMs) * 65536 / int64(time.Millisecond))

	return SecondaryHeader{
		Days:            days,
		MillisecondsDay: msOfDay,
		SubMilliseconds: subMs,
	}
}

// CDSToTime converts a CCSDS CDS secondary header to a time.Time (UTC).
func CDSToTime(h SecondaryHeader) time.Time {
	daysFromUnix := int64(h.Days) - ccsdsCDSEpochDays
	totalMs := daysFromUnix*86400_000 + int64(h.MillisecondsDay)
	sec := totalMs / 1000
	ns := (totalMs%1000)*int64(time.Millisecond) +
		int64(h.SubMilliseconds)*int64(time.Millisecond)/65536
	return time.Unix(sec, ns).UTC()
}

// Encode serialises the full Space Packet to a byte slice. The
// PacketDataLength field in the primary header is computed automatically and
// does NOT need to be set by the caller.
