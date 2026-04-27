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
