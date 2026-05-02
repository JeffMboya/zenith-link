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
	"github.com/absmach/satlyt-demo/pkg/errors"
)

const (
	Size = 4

	CLCWVersion = 0

	ControlWordType = 0

	MaxVCID uint8 = 0x3F

	MaxStatus uint8 = 0x07

	MaxFARMBCounter uint8 = 0x03
)

type CLCW struct {
	StatusField uint8

	COPInEffect uint8

	VCIDField uint8

	NoRFAvail bool

	NoBitLock bool

	Lockout bool

	Wait bool

	Retransmit bool

	FARMBCounter uint8

	ReportType uint8

	ReportValue uint8
}

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

	b[0] = (ControlWordType&0x01)<<7 |
		(CLCWVersion&0x03)<<5 |
		(c.StatusField&0x07)<<2 |
		(c.COPInEffect & 0x03)

	b[1] = (c.VCIDField & 0x3F) << 2

	b[2] = boolBit(c.NoRFAvail)<<7 |
		boolBit(c.NoBitLock)<<6 |
		boolBit(c.Lockout)<<5 |
		boolBit(c.Wait)<<4 |
		boolBit(c.Retransmit)<<3 |
		(c.FARMBCounter&0x03)<<1 |
		(c.ReportType & 0x01)

	b[3] = c.ReportValue

	return b, nil
}

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
