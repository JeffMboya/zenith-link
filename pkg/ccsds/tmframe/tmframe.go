// Package tmframe implements the CCSDS TM Transfer Frame protocol per
// CCSDS 132.0-B-3 (TM Synchronization and Channel Coding).
//
// Wire layout:
//
//	┌───────────────────── Primary Header (6 bytes) ─────────────────────────┐
//	│ bits [15:14] TFVN=00 │ [13:4] SCID │ [3:1] VCID │ [0] OCF flag        │
//	│ byte 2: Master Channel Frame Count                                      │
//	│ byte 3: Virtual Channel Frame Count                                     │
//	│ bits [15] SecHdrFlag │ [14] SyncFlag │ [13] PktOrderFlag               │
//	│ bits [12:11] SegLenID │ [10:0] First Header Pointer (11 bits)          │
//	└─────────────────────────────────────────────────────────────────────────┘
//	┌─── Data Field (variable, carries Space Packets) ────────────────────────┐
//	└─────────────────────────────────────────────────────────────────────────┘
//	┌─── Operational Control Field (4 bytes, present when OCF flag=1) ────────┐
//	└─────────────────────────────────────────────────────────────────────────┘
//	┌─── Frame Error Control Field (2 bytes, CRC-16/CCITT-FALSE) ─────────────┐
//	└─────────────────────────────────────────────────────────────────────────┘
//
// Special First Header Pointer values:
//
//	0x000–0x7FE : byte offset of first packet primary header in the data field
//	0x7FE       : FHPNoPacketStart — no packet start in this frame
//	0x7FF       : FHPOnlyIdle — only idle data
package tmframe

import (
	"encoding/binary"

	"github.com/absmach/zenith-link/pkg/ccsds/crc"
	"github.com/absmach/zenith-link/pkg/ccsds/spacepacket"
	"github.com/absmach/zenith-link/pkg/errors"
)

// Protocol constants.
const (
	TransferFrameVersion = 0    // binary 00

	PrimaryHeaderSize = 6
	FECFSize          = 2  // Frame Error Control Field (CRC)
	OCFSize           = 4  // Operational Control Field (CLCW)

	MinDataFieldSize = 1
	MaxFrameSize     = 2048
	MinFrameSize     = PrimaryHeaderSize + MinDataFieldSize + FECFSize // 9 bytes

	FHPNoPacketStart uint16 = 0x07FE // no packet starts in this frame
	FHPOnlyIdle      uint16 = 0x07FF // only idle packets

	// SegmentLengthID values (bits [12:11] of the Data Field Status word).
	SegLenIDBits = 0b11 // octet data with CCSDS packets (standard choice)
)

// PrimaryHeader holds the decoded fields of the TM Transfer Frame primary header.
type PrimaryHeader struct {
	SCID                    uint16 // 10-bit Spacecraft ID
	VCID                    uint8  // 3-bit Virtual Channel ID
	OCFFlag                 bool   // Operational Control Field present
	MasterChannelFrameCount uint8  // modulo-256 counter for master channel
	VirtualChannelFrameCount uint8 // modulo-256 counter for VC
	SecondaryHeaderFlag     bool
	SyncFlag                bool   // false = octet-synchronised packet mode
	PacketOrderFlag         bool
	SegmentLengthID         uint8  // 2 bits; normally 0b11 for packets
	FirstHeaderPointer      uint16 // 11-bit offset; FHPNoPacketStart or FHPOnlyIdle
}

// TransferFrame is a fully decoded TM Transfer Frame.
type TransferFrame struct {
	Primary   PrimaryHeader
	DataField []byte
	OCF       *[OCFSize]byte // nil when OCFFlag is false
}

// EncodePrimary serialises the 6-byte primary header.
func EncodePrimary(h PrimaryHeader) ([PrimaryHeaderSize]byte, error) {
	if h.SCID > 0x03FF {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidField, errors.New("SCID exceeds 10-bit maximum"))
	}
	if h.VCID > 0x07 {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidField, errors.New("VCID exceeds 3-bit maximum"))
	}
	if h.FirstHeaderPointer > FHPOnlyIdle {
		return [6]byte{}, errors.Wrap(errors.ErrInvalidField, errors.New("FirstHeaderPointer exceeds 11-bit maximum"))
	}

	var b [PrimaryHeaderSize]byte

	// Byte 0-1: TFVN(2) | SCID(10) | VCID(3) | OCF(1)
	w0 := uint16(TransferFrameVersion)<<14 |
		(h.SCID&0x03FF)<<4 |
		uint16(h.VCID&0x07)<<1 |
		boolBit(h.OCFFlag)
	binary.BigEndian.PutUint16(b[0:], w0)

	b[2] = h.MasterChannelFrameCount
	b[3] = h.VirtualChannelFrameCount

	// Byte 4-5: SecHdrFlag(1) | SyncFlag(1) | PktOrderFlag(1) | SegLenID(2) | FHP(11)
	w2 := boolBit(h.SecondaryHeaderFlag)<<15 |
		boolBit(h.SyncFlag)<<14 |
		boolBit(h.PacketOrderFlag)<<13 |
		uint16(h.SegmentLengthID&0x03)<<11 |
		h.FirstHeaderPointer&0x07FF
	binary.BigEndian.PutUint16(b[4:], w2)

	return b, nil
}

// DecodePrimary parses 6 bytes into a PrimaryHeader.
func DecodePrimary(b []byte) (PrimaryHeader, error) {
	if len(b) < PrimaryHeaderSize {
		return PrimaryHeader{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("TM frame primary header requires 6 bytes"))
	}

	w0 := binary.BigEndian.Uint16(b[0:])
	tfvn := (w0 >> 14) & 0x03
	if tfvn != TransferFrameVersion {
		return PrimaryHeader{}, errors.Wrap(errors.ErrBadVersion,
			errors.New("TM frame version must be 0b00"))
	}

	h := PrimaryHeader{
		SCID:                    (w0 >> 4) & 0x03FF,
		VCID:                    uint8((w0 >> 1) & 0x07),
		OCFFlag:                 w0&0x01 == 1,
		MasterChannelFrameCount: b[2],
		VirtualChannelFrameCount: b[3],
	}

	w2 := binary.BigEndian.Uint16(b[4:])
	h.SecondaryHeaderFlag = w2>>15 == 1
	h.SyncFlag = (w2>>14)&0x01 == 1
	h.PacketOrderFlag = (w2>>13)&0x01 == 1
	h.SegmentLengthID = uint8((w2 >> 11) & 0x03)
	h.FirstHeaderPointer = w2 & 0x07FF
	return h, nil
}
