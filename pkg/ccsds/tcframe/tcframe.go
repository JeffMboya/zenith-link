// Package tcframe implements the CCSDS TC Transfer Frame protocol per
// CCSDS 232.0-B-4 (TC Synchronization and Channel Coding).
//
// Wire layout (primary header = 6 bytes):
//
//	Byte 0:   [TFVN1][TFVN0][BYP][CTRL][RSVD][RSVD][SCID9][SCID8]
//	Byte 1:   [SCID7][SCID6][SCID5][SCID4][SCID3][SCID2][SCID1][SCID0]
//	Byte 2:   [RSVD][RSVD][VCID5][VCID4][VCID3][VCID2][VCID1][VCID0]
//	Bytes 3-4:[FrameLength(16)] — total frame length in octets
//	Byte 5:   [FrameSequenceNumber(8)] — modulo-256 N(S)
//
// The Frame Error Control Field (2 bytes, CRC-16/CCITT-FALSE) is optionally
// appended after the data field. CCSDS mandates it for most uplink scenarios.
package tcframe

import (
	"encoding/binary"

	"github.com/absmach/orbitron/pkg/ccsds/crc"
	"github.com/absmach/orbitron/pkg/errors"
)

const (
	TransferFrameVersion = 0

	PrimaryHeaderSize = 6
	FECFSize          = 2

	MinFrameSize = PrimaryHeaderSize + 1 + FECFSize
	MaxFrameSize = 1019

	MaxSCID uint16 = 0x03FF
	MaxVCID uint8  = 0x3F
)

type PrimaryHeader struct {
	BypassFlag          bool
	ControlCommandFlag  bool
	SCID                uint16
	VCID                uint8
	FrameLength         uint16
	FrameSequenceNumber uint8
}

type TransferFrame struct {
	Primary   PrimaryHeader
	DataField []byte
}

func EncodePrimary(h PrimaryHeader) ([PrimaryHeaderSize]byte, error) {
	if h.SCID > MaxSCID {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidField, errors.New("SCID exceeds 10-bit maximum"))
	}
	if h.VCID > MaxVCID {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidField, errors.New("VCID exceeds 6-bit maximum"))
	}

	var b [PrimaryHeaderSize]byte

	b[0] = byte(TransferFrameVersion)<<6 |
		boolByte(h.BypassFlag)<<5 |
		boolByte(h.ControlCommandFlag)<<4 |
		byte(h.SCID>>8)&0x03

	b[1] = byte(h.SCID & 0xFF)

	b[2] = h.VCID & MaxVCID

	binary.BigEndian.PutUint16(b[3:], h.FrameLength)

	b[5] = h.FrameSequenceNumber

	return b, nil
}

func DecodePrimary(b []byte) (PrimaryHeader, error) {
	if len(b) < PrimaryHeaderSize {
		return PrimaryHeader{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("TC frame primary header requires 6 bytes"))
	}

	tfvn := (b[0] >> 6) & 0x03
	if tfvn != TransferFrameVersion {
		return PrimaryHeader{}, errors.Wrap(errors.ErrBadVersion,
			errors.New("TC frame version must be 0b00"))
	}

	return PrimaryHeader{
		BypassFlag:          (b[0]>>5)&0x01 == 1,
		ControlCommandFlag:  (b[0]>>4)&0x01 == 1,
		SCID:                uint16(b[0]&0x03)<<8 | uint16(b[1]),
		VCID:                b[2] & MaxVCID,
		FrameLength:         binary.BigEndian.Uint16(b[3:]),
		FrameSequenceNumber: b[5],
	}, nil
}

func Encode(f TransferFrame) ([]byte, error) {
	totalLen := PrimaryHeaderSize + len(f.DataField) + FECFSize
	if totalLen < MinFrameSize {
		return nil, errors.ErrFrameTooSmall
	}
	if totalLen > MaxFrameSize {
		return nil, errors.ErrFrameTooLarge
	}

	f.Primary.FrameLength = uint16(totalLen)

	hdr, err := EncodePrimary(f.Primary)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, totalLen)
	copy(buf[:PrimaryHeaderSize], hdr[:])
	copy(buf[PrimaryHeaderSize:], f.DataField)

	fecfPos := totalLen - FECFSize
	v := crc.CCITT16(buf[:fecfPos])
	buf[fecfPos] = byte(v >> 8)
	buf[fecfPos+1] = byte(v)

	return buf, nil
}

func Decode(b []byte) (TransferFrame, error) {
	if len(b) < MinFrameSize {
		return TransferFrame{}, errors.ErrFrameTooSmall
	}
	if len(b) > MaxFrameSize {
		return TransferFrame{}, errors.ErrFrameTooLarge
	}

	if !crc.Verify(b) {
		return TransferFrame{}, errors.ErrCRCMismatch
	}

	primary, err := DecodePrimary(b[:PrimaryHeaderSize])
	if err != nil {
		return TransferFrame{}, err
	}

	if int(primary.FrameLength) != len(b) {
		return TransferFrame{}, errors.Wrap(errors.ErrDataLenMismatch,
			errors.New("FrameLength field does not match actual buffer size"))
	}

	dataEnd := len(b) - FECFSize
	data := make([]byte, dataEnd-PrimaryHeaderSize)
	copy(data, b[PrimaryHeaderSize:dataEnd])

	return TransferFrame{
		Primary:   primary,
		DataField: data,
	}, nil
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
