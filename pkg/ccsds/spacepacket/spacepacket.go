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

const (
	Version             uint8  = 0
	PrimaryHeaderSize          = 6
	SecondaryHeaderSize        = 8
	MaxAPID             uint16 = 0x07FF
	IdleAPID            uint16 = 0x07FF
	MaxSeqCount         uint16 = 0x3FFF
	MaxDataLength       uint16 = 0xFFFF
)

type PacketType uint8

const (
	Telemetry   PacketType = 0
	Telecommand PacketType = 1
)

type GroupingFlags uint8

const (
	Continuation GroupingFlags = 0b00
	FirstSegment GroupingFlags = 0b01
	LastSegment  GroupingFlags = 0b10
	Unsegmented  GroupingFlags = 0b11
)

const ccsdsCDSEpochDays = 4383

type PrimaryHeader struct {
	Type               PacketType
	HasSecondaryHeader bool
	APID               uint16
	GroupingFlags      GroupingFlags
	SequenceCount      uint16

	PacketDataLength uint16
}

type SecondaryHeader struct {
	Days            uint16
	MillisecondsDay uint32
	SubMilliseconds uint16
}

type SpacePacket struct {
	Primary   PrimaryHeader
	Secondary *SecondaryHeader
	UserData  []byte
}

func EncodePrimary(h PrimaryHeader) ([PrimaryHeaderSize]byte, error) {
	if h.APID > MaxAPID {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidAPID, errors.New("value exceeds 11-bit maximum 0x7FF"))
	}
	if h.SequenceCount > MaxSeqCount {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidSeqCount, errors.New("value exceeds 14-bit maximum 0x3FFF"))
	}

	var b [PrimaryHeaderSize]byte

	w0 := uint16(Version)<<13 |
		uint16(h.Type)<<12 |
		boolBit(h.HasSecondaryHeader)<<11 |
		h.APID&MaxAPID
	binary.BigEndian.PutUint16(b[0:], w0)

	w1 := uint16(h.GroupingFlags)<<14 | h.SequenceCount&MaxSeqCount
	binary.BigEndian.PutUint16(b[2:], w1)

	binary.BigEndian.PutUint16(b[4:], h.PacketDataLength)

	return b, nil
}

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

func EncodeSecondary(h SecondaryHeader) [SecondaryHeaderSize]byte {
	var b [SecondaryHeaderSize]byte
	binary.BigEndian.PutUint16(b[0:], h.Days)
	binary.BigEndian.PutUint32(b[2:], h.MillisecondsDay)
	binary.BigEndian.PutUint16(b[6:], h.SubMilliseconds)
	return b
}

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

func CDSFromTime(t time.Time) SecondaryHeader {
	t = t.UTC()

	unixSec := t.Unix()
	totalSec := unixSec + int64(ccsdsCDSEpochDays)*86400
	days := uint16(totalSec / 86400)

	secOfDay := totalSec % 86400
	msOfDay := uint32(secOfDay*1000) + uint32(t.Nanosecond()/int(time.Millisecond))

	nsWithinMs := t.Nanosecond() % int(time.Millisecond)
	subMs := uint16(int64(nsWithinMs) * 65536 / int64(time.Millisecond))

	return SecondaryHeader{
		Days:            days,
		MillisecondsDay: msOfDay,
		SubMilliseconds: subMs,
	}
}

func CDSToTime(h SecondaryHeader) time.Time {
	daysFromUnix := int64(h.Days) - ccsdsCDSEpochDays
	totalMs := daysFromUnix*86400_000 + int64(h.MillisecondsDay)
	sec := totalMs / 1000
	ns := (totalMs%1000)*int64(time.Millisecond) +
		int64(h.SubMilliseconds)*int64(time.Millisecond)/65536
	return time.Unix(sec, ns).UTC()
}

func Encode(pkt SpacePacket) ([]byte, error) {

	var dataField []byte
	if pkt.Primary.HasSecondaryHeader {
		if pkt.Secondary == nil {
			return nil, errors.Wrap(errors.ErrMalformedFrame,
				errors.New("HasSecondaryHeader is true but Secondary is nil"))
		}
		s := EncodeSecondary(*pkt.Secondary)
		dataField = append(dataField, s[:]...)
	}
	dataField = append(dataField, pkt.UserData...)

	if len(dataField) == 0 {
		return nil, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("packet data field must contain at least 1 byte"))
	}
	if len(dataField) > int(MaxDataLength)+1 {
		return nil, errors.ErrFrameTooLarge
	}

	pkt.Primary.PacketDataLength = uint16(len(dataField) - 1)

	hdr, err := EncodePrimary(pkt.Primary)
	if err != nil {
		return nil, err
	}

	out := make([]byte, PrimaryHeaderSize+len(dataField))
	copy(out[:PrimaryHeaderSize], hdr[:])
	copy(out[PrimaryHeaderSize:], dataField)
	return out, nil
}

func Decode(b []byte) (SpacePacket, error) {
	if len(b) < PrimaryHeaderSize {
		return SpacePacket{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("buffer shorter than primary header"))
	}

	primary, err := DecodePrimary(b[:PrimaryHeaderSize])
	if err != nil {
		return SpacePacket{}, err
	}

	expectedTotal := PrimaryHeaderSize + int(primary.PacketDataLength) + 1
	if len(b) < expectedTotal {
		return SpacePacket{}, errors.Wrap(errors.ErrDataLenMismatch,
			errors.New("buffer shorter than PacketDataLength indicates"))
	}

	dataField := b[PrimaryHeaderSize:expectedTotal]

	pkt := SpacePacket{Primary: primary}

	offset := 0
	if primary.HasSecondaryHeader {
		if len(dataField) < SecondaryHeaderSize {
			return SpacePacket{}, errors.Wrap(errors.ErrMalformedFrame,
				errors.New("secondary header flag set but insufficient data"))
		}
		sh, err := DecodeSecondary(dataField[:SecondaryHeaderSize])
		if err != nil {
			return SpacePacket{}, err
		}
		pkt.Secondary = &sh
		offset = SecondaryHeaderSize
	}

	pkt.UserData = make([]byte, len(dataField)-offset)
	copy(pkt.UserData, dataField[offset:])
	return pkt, nil
}

func boolBit(v bool) uint16 {
	if v {
		return 1
	}
	return 0
}
