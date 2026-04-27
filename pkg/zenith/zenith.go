// Package zenith implements the Zenith-Link binary telemetry protocol v2.
//
// Wire format:
//
//	┌─── Header (10 bytes) ──────────────────────────────────────────────────┐
//	│ Magic     uint16  0x5A4C ("ZL")                                        │
//	│ Version   uint8   2                                                     │
//	│ Flags     uint8   bitmask (FLAG_STORE_FWD | FLAG_COMPRESSED | ...)     │
//	│ Sequence  uint16  monotonically incrementing frame counter              │
//	│ Timestamp uint32  Unix seconds                                          │
//	├─── Presence bitmask (2 bytes) ─────────────────────────────────────────┤
//	│ Bit N = 1 means field N is present in the payload section              │
//	├─── Payload (variable, only present fields) ────────────────────────────┤
//	│ LatE7          int32   geodetic latitude  × 1e7 [degrees]              │
//	│ LonE7          int32   geodetic longitude × 1e7 [degrees]              │
//	│ AltM           int32   altitude [m]                                     │
//	│ AttRoll        int16   roll  [0.01 deg LSB]                            │
//	│ AttPitch       int16   pitch [0.01 deg LSB]                            │
//	│ AttYaw         int16   yaw   [0.01 deg LSB]                            │
//	│ AngVelX        int16   angular velocity X [0.01 deg/s LSB]             │
//	│ AngVelY        int16   angular velocity Y [0.01 deg/s LSB]             │
//	│ AngVelZ        int16   angular velocity Z [0.01 deg/s LSB]             │
//	│ BatV           uint16  battery voltage [mV]                            │
//	│ SolarV         uint16  solar panel voltage [mV]                         │
//	│ TempC          int16   temperature [0.01 °C LSB]                       │
//	│ RSSI           int16   signal strength [dBm × 10]                      │
//	│ InferenceClass uint8   onboard AI classification result (0–6)          │
//	│ InferenceConf  uint8   confidence [0=0%, 255=100%]                     │
//	├─── HMAC-SHA256 trailer (32 bytes) ─────────────────────────────────────┤
//	│ HMAC over all bytes preceding the trailer (magic through payload)      │
//	└────────────────────────────────────────────────────────────────────────┘
//
// Presence bits (uint16, big-endian, bit 15 = MSB):
//
//	Bit 15: LatE7 / LonE7 / AltM (always together)
//	Bit 14: AttRoll / AttPitch / AttYaw (always together)
//	Bit 13: AngVelX / AngVelY / AngVelZ (always together)
//	Bit 12: BatV
//	Bit 11: SolarV
//	Bit 10: TempC
//	Bit 9:  RSSI
//	Bit 8:  InferenceClass / InferenceConf (always together)
package zenith

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"

	"github.com/absmach/zenith-link/pkg/errors"
)

// Protocol constants.
const (
	Magic   uint16 = 0x5A4C
	Version uint8  = 2

	HeaderSize   = 10
	PresenceSize = 2
	HMACSize     = 32

	MinFrameSize = HeaderSize + PresenceSize + HMACSize // 44 bytes — no payload fields

	// Flag bits (Flags byte).
	FlagStoreFwd   uint8 = 0x01 // frame was stored and forwarded
	FlagCompressed uint8 = 0x02 // payload is delta-compressed

	// Presence bitmask positions (bit 15 = MSB of the uint16).
	PresencePosition  uint16 = 1 << 15 // LatE7 / LonE7 / AltM
	PresenceAttitude  uint16 = 1 << 14 // roll / pitch / yaw
	PresenceAngVel    uint16 = 1 << 13 // angular velocity X/Y/Z
	PresenceBatV      uint16 = 1 << 12
	PresenceSolarV    uint16 = 1 << 11
	PresenceTempC     uint16 = 1 << 10
	PresenceRSSI      uint16 = 1 << 9
	PresenceInference uint16 = 1 << 8 // InferenceClass / InferenceConf
)

// InferenceClassNames maps an InferenceClass value to a human-readable label.
// Values ≥ 7 are reserved; index 7 ("unknown") is the catch-all.
var InferenceClassNames = [8]string{
	"cloud", "ocean", "land", "urban", "vegetation", "ice", "desert", "unknown",
}

// InferenceClassName returns the label for a class byte, defaulting to "unknown"
// for any value ≥ 7.
func InferenceClassName(class uint8) string {
	if int(class) < len(InferenceClassNames) {
		return InferenceClassNames[class]
	}
	return "unknown"
}

// Telemetry holds the decoded telemetry fields from a Zenith-Link frame.
// Zero value for a field means the field was absent unless the corresponding
// presence bit is set.
type Telemetry struct {
	Sequence  uint16
	Timestamp uint32
	Flags     uint8
	Presence  uint16

	// Position (present when PresencePosition bit is set).
	LatE7 int32 // latitude  × 1e7
	LonE7 int32 // longitude × 1e7
	AltM  int32 // altitude  [m]

	// Attitude (present when PresenceAttitude bit is set).
	AttRoll  int16 // [0.01 deg]
	AttPitch int16 // [0.01 deg]
	AttYaw   int16 // [0.01 deg]

	// Angular velocity (present when PresenceAngVel bit is set).
	AngVelX int16 // [0.01 deg/s]
	AngVelY int16 // [0.01 deg/s]
	AngVelZ int16 // [0.01 deg/s]

	// Power (individual presence bits).
	BatV   uint16 // battery voltage [mV]
	SolarV uint16 // solar voltage [mV]

	// Environmental.
	TempC int16 // temperature [0.01 °C]
	RSSI  int16 // RSSI [dBm × 10]

	// Onboard AI inference result (present when PresenceInference bit is set).
	InferenceClass uint8 // 0–6; see InferenceClassNames
	InferenceConf  uint8 // confidence [0=0%, 255≈100%]
}

// Encode serialises a Telemetry struct into a Zenith-Link v2 frame and
// appends the 32-byte HMAC-SHA256 trailer using hmacKey.
// hmacKey must not be nil or empty.
