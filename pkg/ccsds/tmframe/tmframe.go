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

	"github.com/absmach/satlyt-demo/pkg/ccsds/crc"
	"github.com/absmach/satlyt-demo/pkg/ccsds/spacepacket"
	"github.com/absmach/satlyt-demo/pkg/errors"
)

const (
	TransferFrameVersion = 0

	PrimaryHeaderSize = 6
	FECFSize          = 2
	OCFSize           = 4

	MinDataFieldSize = 1
	MaxFrameSize     = 2048
	MinFrameSize     = PrimaryHeaderSize + MinDataFieldSize + FECFSize

	FHPNoPacketStart uint16 = 0x07FE
	FHPOnlyIdle      uint16 = 0x07FF

	SegLenIDBits = 0b11
)

type PrimaryHeader struct {
	SCID                     uint16
	VCID                     uint8
	OCFFlag                  bool
	MasterChannelFrameCount  uint8
	VirtualChannelFrameCount uint8
	SecondaryHeaderFlag      bool
	SyncFlag                 bool
	PacketOrderFlag          bool
	SegmentLengthID          uint8
	FirstHeaderPointer       uint16
}

type TransferFrame struct {
	Primary   PrimaryHeader
	DataField []byte
	OCF       *[OCFSize]byte
}

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

	w0 := uint16(TransferFrameVersion)<<14 |
		(h.SCID&0x03FF)<<4 |
		uint16(h.VCID&0x07)<<1 |
		boolBit(h.OCFFlag)
	binary.BigEndian.PutUint16(b[0:], w0)

	b[2] = h.MasterChannelFrameCount
	b[3] = h.VirtualChannelFrameCount

	w2 := boolBit(h.SecondaryHeaderFlag)<<15 |
		boolBit(h.SyncFlag)<<14 |
		boolBit(h.PacketOrderFlag)<<13 |
		uint16(h.SegmentLengthID&0x03)<<11 |
		h.FirstHeaderPointer&0x07FF
	binary.BigEndian.PutUint16(b[4:], w2)

	return b, nil
}

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
		SCID:                     (w0 >> 4) & 0x03FF,
		VCID:                     uint8((w0 >> 1) & 0x07),
		OCFFlag:                  w0&0x01 == 1,
		MasterChannelFrameCount:  b[2],
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

func Encode(f TransferFrame, frameSize int) ([]byte, error) {
	overhead := PrimaryHeaderSize + FECFSize
	if f.Primary.OCFFlag {
		overhead += OCFSize
	}
	if frameSize < overhead+MinDataFieldSize {
		return nil, errors.Wrap(errors.ErrFrameTooSmall,
			errors.New("frameSize too small for header + at least 1 data byte + FECF"))
	}
	if frameSize > MaxFrameSize {
		return nil, errors.ErrFrameTooLarge
	}

	dataFieldSize := frameSize - overhead
	if len(f.DataField) > dataFieldSize {
		return nil, errors.Wrap(errors.ErrFrameTooLarge,
			errors.New("DataField exceeds available space in frame"))
	}

	buf := make([]byte, frameSize)

	hdr, err := EncodePrimary(f.Primary)
	if err != nil {
		return nil, err
	}
	copy(buf[:PrimaryHeaderSize], hdr[:])

	copy(buf[PrimaryHeaderSize:PrimaryHeaderSize+dataFieldSize], f.DataField)

	if f.Primary.OCFFlag {
		if f.OCF == nil {
			return nil, errors.Wrap(errors.ErrInvalidField, errors.New("OCFFlag set but OCF is nil"))
		}
		copy(buf[PrimaryHeaderSize+dataFieldSize:], f.OCF[:])
	}

	fecfPos := frameSize - FECFSize
	v := crc.CCITT16(buf[:fecfPos])
	buf[fecfPos] = byte(v >> 8)
	buf[fecfPos+1] = byte(v)

	return buf, nil
}

func Decode(b []byte) (TransferFrame, error) {
	if len(b) < MinFrameSize {
		return TransferFrame{}, errors.Wrap(errors.ErrFrameTooSmall,
			errors.New("TM frame shorter than minimum 9 bytes"))
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

	trailerSize := FECFSize
	if primary.OCFFlag {
		trailerSize += OCFSize
	}
	dataEnd := len(b) - trailerSize
	if dataEnd <= PrimaryHeaderSize {
		return TransferFrame{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("no room for data field after headers and trailer"))
	}

	tf := TransferFrame{
		Primary:   primary,
		DataField: make([]byte, dataEnd-PrimaryHeaderSize),
	}
	copy(tf.DataField, b[PrimaryHeaderSize:dataEnd])

	if primary.OCFFlag {
		var ocf [OCFSize]byte
		copy(ocf[:], b[dataEnd:dataEnd+OCFSize])
		tf.OCF = &ocf
	}

	return tf, nil
}

func ExtractSpacePackets(dataField []byte, fhp uint16, pending []byte) (pkts [][]byte, leftover []byte, err error) {
	if fhp == FHPOnlyIdle {
		return nil, nil, nil
	}

	pos := 0

	if len(pending) > 0 {

		if len(pending) < spacepacket.PrimaryHeaderSize {

			needed := spacepacket.PrimaryHeaderSize - len(pending)
			if needed > len(dataField) {
				return nil, append(pending, dataField...), nil
			}
			pending = append(pending, dataField[:needed]...)
			pos = needed
		}
		if len(pending) >= spacepacket.PrimaryHeaderSize {
			ph, decErr := spacepacket.DecodePrimary(pending[:spacepacket.PrimaryHeaderSize])
			if decErr != nil {
				return nil, nil, decErr
			}
			totalLen := spacepacket.PrimaryHeaderSize + int(ph.PacketDataLength) + 1
			remaining := totalLen - len(pending)
			if remaining < 0 {
				return nil, nil, errors.New("tmframe: malformed pending packet: more bytes buffered than declared length")
			}
			if remaining > len(dataField)-pos {

				return nil, append(pending, dataField[pos:]...), nil
			}
			pending = append(pending, dataField[pos:pos+remaining]...)
			pkts = append(pkts, pending)
			pos += remaining
		}
	} else if fhp == FHPNoPacketStart {

		return nil, nil, nil
	} else {

		pos = int(fhp)
	}

	for pos < len(dataField) {
		if len(dataField)-pos < spacepacket.PrimaryHeaderSize {
			leftover = append(leftover, dataField[pos:]...)
			break
		}
		ph, decErr := spacepacket.DecodePrimary(dataField[pos : pos+spacepacket.PrimaryHeaderSize])
		if decErr != nil {
			return nil, nil, decErr
		}
		totalLen := spacepacket.PrimaryHeaderSize + int(ph.PacketDataLength) + 1
		if pos+totalLen > len(dataField) {

			leftover = append(leftover, dataField[pos:]...)
			break
		}
		pkts = append(pkts, dataField[pos:pos+totalLen])
		pos += totalLen
	}

	return pkts, leftover, nil
}

func boolBit(v bool) uint16 {
	if v {
		return 1
	}
	return 0
}
