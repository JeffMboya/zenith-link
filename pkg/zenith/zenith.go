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
	FlagPriority   uint8 = 0x04 // priority downlink — anomaly detected onboard

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
// Classes are spacecraft health states — index-stable, matches inference.ClassNames.
// Index 7 ("UNKNOWN") is the catch-all for any value ≥ 7.
var InferenceClassNames = [8]string{
	"NOMINAL", "POWER_ANOMALY", "THERMAL_EVENT", "ATTITUDE_INSTABILITY",
	"RF_DEGRADATION", "ECLIPSE_ENTRY", "ECLIPSE_COMPUTE", "UNKNOWN",
}

// InferenceClassName returns the label for a class byte, defaulting to "unknown"
// for any value ≥ 7.
func InferenceClassName(class uint8) string {
	if int(class) < len(InferenceClassNames) {
		return InferenceClassNames[class]
	}
	return "UNKNOWN"
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
func Encode(tm Telemetry, hmacKey []byte) ([]byte, error) {
	if len(hmacKey) == 0 {
		return nil, errors.Wrap(errors.ErrHMACFailure, errors.New("HMAC key must not be empty"))
	}

	payloadSize := presencePayloadSize(tm.Presence)
	totalSize := HeaderSize + PresenceSize + payloadSize + HMACSize

	buf := make([]byte, 0, totalSize)

	// ─── Header ──────────────────────────────────────────────────────────────
	buf = binary.BigEndian.AppendUint16(buf, Magic)
	buf = append(buf, Version)
	buf = append(buf, tm.Flags)
	buf = binary.BigEndian.AppendUint16(buf, tm.Sequence)
	buf = binary.BigEndian.AppendUint32(buf, tm.Timestamp)

	// ─── Presence bitmask ────────────────────────────────────────────────────
	buf = binary.BigEndian.AppendUint16(buf, tm.Presence)

	// ─── Payload ─────────────────────────────────────────────────────────────
	if tm.Presence&PresencePosition != 0 {
		buf = binary.BigEndian.AppendUint32(buf, uint32(tm.LatE7))
		buf = binary.BigEndian.AppendUint32(buf, uint32(tm.LonE7))
		buf = binary.BigEndian.AppendUint32(buf, uint32(tm.AltM))
	}
	if tm.Presence&PresenceAttitude != 0 {
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.AttRoll))
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.AttPitch))
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.AttYaw))
	}
	if tm.Presence&PresenceAngVel != 0 {
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.AngVelX))
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.AngVelY))
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.AngVelZ))
	}
	if tm.Presence&PresenceBatV != 0 {
		buf = binary.BigEndian.AppendUint16(buf, tm.BatV)
	}
	if tm.Presence&PresenceSolarV != 0 {
		buf = binary.BigEndian.AppendUint16(buf, tm.SolarV)
	}
	if tm.Presence&PresenceTempC != 0 {
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.TempC))
	}
	if tm.Presence&PresenceRSSI != 0 {
		buf = binary.BigEndian.AppendUint16(buf, uint16(tm.RSSI))
	}
	if tm.Presence&PresenceInference != 0 {
		buf = append(buf, tm.InferenceClass, tm.InferenceConf)
	}

	// ─── HMAC-SHA256 trailer ─────────────────────────────────────────────────
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(buf)
	buf = mac.Sum(buf)

	return buf, nil
}

// Decode parses and authenticates a Zenith-Link v2 frame.
// Returns ErrHMACFailure if the HMAC does not match.
// Returns ErrBadMagic if the magic number is wrong.
// Returns ErrBadVersion if the version byte is not 2.
func Decode(b []byte, hmacKey []byte) (Telemetry, error) {
	if len(hmacKey) == 0 {
		return Telemetry{}, errors.Wrap(errors.ErrHMACFailure, errors.New("HMAC key must not be empty"))
	}
	if len(b) < MinFrameSize {
		return Telemetry{}, errors.Wrap(errors.ErrFrameTooSmall,
			errors.New("frame shorter than minimum Zenith-Link size"))
	}

	// ─── HMAC verification (before any other parsing) ─────────────────────
	bodyEnd := len(b) - HMACSize
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(b[:bodyEnd])
	expected := mac.Sum(nil)
	if !hmac.Equal(b[bodyEnd:], expected) {
		return Telemetry{}, errors.ErrHMACFailure
	}

	// ─── Header ──────────────────────────────────────────────────────────────
	magic := binary.BigEndian.Uint16(b[0:])
	if magic != Magic {
		return Telemetry{}, errors.Wrap(errors.ErrBadMagic,
			errors.New("Zenith-Link magic mismatch"))
	}
	if b[2] != Version {
		return Telemetry{}, errors.Wrap(errors.ErrBadVersion,
			errors.New("Zenith-Link version mismatch"))
	}

	tm := Telemetry{
		Flags:     b[3],
		Sequence:  binary.BigEndian.Uint16(b[4:]),
		Timestamp: binary.BigEndian.Uint32(b[6:]),
	}

	// ─── Presence bitmask ────────────────────────────────────────────────────
	tm.Presence = binary.BigEndian.Uint16(b[HeaderSize:])

	// Verify payload length matches presence bits.
	expectedPayload := presencePayloadSize(tm.Presence)
	actualPayload := bodyEnd - HeaderSize - PresenceSize
	if actualPayload != expectedPayload {
		return Telemetry{}, errors.Wrap(errors.ErrDataLenMismatch,
			errors.New("payload length does not match presence bitmask"))
	}

	// ─── Payload ─────────────────────────────────────────────────────────────
	pos := HeaderSize + PresenceSize

	if tm.Presence&PresencePosition != 0 {
		tm.LatE7 = int32(binary.BigEndian.Uint32(b[pos:]))
		pos += 4
		tm.LonE7 = int32(binary.BigEndian.Uint32(b[pos:]))
		pos += 4
		tm.AltM = int32(binary.BigEndian.Uint32(b[pos:]))
		pos += 4
	}
	if tm.Presence&PresenceAttitude != 0 {
		tm.AttRoll = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
		tm.AttPitch = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
		tm.AttYaw = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
	}
	if tm.Presence&PresenceAngVel != 0 {
		tm.AngVelX = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
		tm.AngVelY = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
		tm.AngVelZ = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
	}
	if tm.Presence&PresenceBatV != 0 {
		tm.BatV = binary.BigEndian.Uint16(b[pos:])
		pos += 2
	}
	if tm.Presence&PresenceSolarV != 0 {
		tm.SolarV = binary.BigEndian.Uint16(b[pos:])
		pos += 2
	}
	if tm.Presence&PresenceTempC != 0 {
		tm.TempC = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
	}
	if tm.Presence&PresenceRSSI != 0 {
		tm.RSSI = int16(binary.BigEndian.Uint16(b[pos:]))
		pos += 2
	}
	if tm.Presence&PresenceInference != 0 {
		tm.InferenceClass = b[pos]
		tm.InferenceConf = b[pos+1]
	}

	return tm, nil
}

// presencePayloadSize returns the number of payload bytes required for the
// given presence bitmask.
func presencePayloadSize(presence uint16) int {
	size := 0
	if presence&PresencePosition != 0 {
		size += 12 // int32 × 3
	}
	if presence&PresenceAttitude != 0 {
		size += 6 // int16 × 3
	}
	if presence&PresenceAngVel != 0 {
		size += 6 // int16 × 3
	}
	if presence&PresenceBatV != 0 {
		size += 2
	}
	if presence&PresenceSolarV != 0 {
		size += 2
	}
	if presence&PresenceTempC != 0 {
		size += 2
	}
	if presence&PresenceRSSI != 0 {
		size += 2
	}
	if presence&PresenceInference != 0 {
		size += 2 // uint8 class + uint8 conf
	}
	return size
}
