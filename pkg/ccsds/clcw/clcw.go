// Package clcw implements the CCSDS Communications Link Control Word (CLCW)
// as defined in CCSDS 232.0-B-4 §2.3 (TC Synchronization and Channel Coding).
//
// Wire layout (4 bytes, MSB first):
//
//	Byte 0: [CtrlWordType=0(1)] [CLCWVersion=00(2)] [StatusField(3)] [COP in effect(2)]
//	Byte 1: [VCIDField(6)] [Spare=00(2)]
//	Byte 2: [NoRFAvail(1)] [NoBitLock(1)] [Lockout(1)] [Wait(1)] [Retransmit(1)] [FARMBCounter(2)] [ReportType(1)]
//	Byte 3: [ReportValue(8)]  — N(R) for COP-1 (FARM-B report value)
//
// The CLCW is the 4-byte word placed in the Operational Control Field (OCF)
// of TM Transfer Frames or as a standalone Type-1 report.
package clcw

import (
	"github.com/absmach/zenith-link/pkg/errors"
)

const (
	// Size is the fixed wire size of a CLCW in bytes.
	Size = 4

	// CLCWVersion is the fixed 2-bit version field value (always 0b00).
	CLCWVersion = 0

	// ControlWordType is the fixed 1-bit type field (always 0 for CLCW).
	ControlWordType = 0

	// MaxVCID is the maximum valid value for the Virtual Channel ID field (6 bits).
	MaxVCID uint8 = 0x3F

	// MaxStatus is the maximum valid value for the StatusField (3 bits).
	MaxStatus uint8 = 0x07

	// MaxFARMBCounter is the maximum valid value for the FARM-B counter (2 bits).
	MaxFARMBCounter uint8 = 0x03
)

// CLCW holds the decoded fields of the Communications Link Control Word.
type CLCW struct {
	// StatusField is a 3-bit implementation-specific status field.
	StatusField uint8

	// COPInEffect identifies which COP protocol is active (2 bits):
	//   0 = COP-1, 1 = reserved, 2 = reserved, 3 = reserved.
	COPInEffect uint8

	// VCIDField identifies the virtual channel this CLCW pertains to (6 bits).
	VCIDField uint8

	// NoRFAvail indicates no radio-frequency signal available.
	NoRFAvail bool

	// NoBitLock indicates bit synchronisation has not been achieved.
	NoBitLock bool

	// Lockout indicates the COP-1 FARM has entered Lockout state.
	Lockout bool

	// Wait indicates the spacecraft receiver is busy (WAIT state).
	Wait bool

	// Retransmit indicates the last BC or BD frame was rejected and must be
	// retransmitted.
	Retransmit bool

	// FARMBCounter is the 2-bit FARM-B sliding window counter.
	FARMBCounter uint8

	// ReportType is the 1-bit report type (0 = Type-1 report).
	ReportType uint8

	// ReportValue (N(R)) is the 8-bit COP-1 expected next frame sequence number.
	ReportValue uint8
}

// Encode serialises the CLCW into exactly 4 bytes.
func Encode(c CLCW) ([Size]byte, error) {
	if c.StatusField > MaxStatus {
		return [Size]byte{}, errors.Wrap(errors.ErrInvalidField,
			errors.New("StatusField exceeds 3-bit maximum"))
	}
	if c.COPInEffect > 0x03 {
		return [Size]byte{}, errors.Wrap(errors.ErrInvalidField,
			errors.New("COPInEffect exceeds 2-bit maximum"))
	}
	if c.VCIDField > MaxVCID {
		return [Size]byte{}, errors.Wrap(errors.ErrInvalidField,
			errors.New("VCIDField exceeds 6-bit maximum"))
	}
	if c.FARMBCounter > MaxFARMBCounter {
		return [Size]byte{}, errors.Wrap(errors.ErrInvalidField,
			errors.New("FARMBCounter exceeds 2-bit maximum"))
	}

	var b [Size]byte

	// Byte 0: CtrlWordType(1) | CLCWVersion(2) | StatusField(3) | COPInEffect(2)
	b[0] = (ControlWordType&0x01)<<7 |
		(CLCWVersion&0x03)<<5 |
		(c.StatusField&0x07)<<2 |
		(c.COPInEffect & 0x03)

	// Byte 1: VCIDField(6) | Spare(2) — spare bits are always 0
	b[1] = (c.VCIDField & 0x3F) << 2

	// Byte 2: NoRFAvail(1) | NoBitLock(1) | Lockout(1) | Wait(1) | Retransmit(1) | FARMBCounter(2) | ReportType(1)
	b[2] = boolBit(c.NoRFAvail)<<7 |
		boolBit(c.NoBitLock)<<6 |
		boolBit(c.Lockout)<<5 |
		boolBit(c.Wait)<<4 |
		boolBit(c.Retransmit)<<3 |
		(c.FARMBCounter&0x03)<<1 |
		(c.ReportType & 0x01)

	// Byte 3: ReportValue N(R)
	b[3] = c.ReportValue

	return b, nil
}

// Decode parses 4 bytes into a CLCW.
func Decode(b []byte) (CLCW, error) {
	if len(b) < Size {
		return CLCW{}, errors.Wrap(errors.ErrMalformedFrame,
			errors.New("CLCW requires exactly 4 bytes"))
	}

	ctrlWordType := (b[0] >> 7) & 0x01
	if ctrlWordType != ControlWordType {
		return CLCW{}, errors.Wrap(errors.ErrBadVersion,
			errors.New("CLCW control word type must be 0"))
	}

	clcwVersion := (b[0] >> 5) & 0x03
	if clcwVersion != CLCWVersion {
		return CLCW{}, errors.Wrap(errors.ErrBadVersion,
			errors.New("CLCW version must be 0b00"))
	}

	return CLCW{
		StatusField:  (b[0] >> 2) & 0x07,
		COPInEffect:  b[0] & 0x03,
		VCIDField:    (b[1] >> 2) & 0x3F,
		NoRFAvail:    (b[2]>>7)&0x01 == 1,
		NoBitLock:    (b[2]>>6)&0x01 == 1,
		Lockout:      (b[2]>>5)&0x01 == 1,
		Wait:         (b[2]>>4)&0x01 == 1,
		Retransmit:   (b[2]>>3)&0x01 == 1,
		FARMBCounter: (b[2] >> 1) & 0x03,
		ReportType:   b[2] & 0x01,
		ReportValue:  b[3],
	}, nil
}

func boolBit(v bool) byte {
	if v {
		return 1
	}
	return 0
}
