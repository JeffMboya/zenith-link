// Package preprocess implements onboard telemetry reduction for bandwidth-constrained downlinks.
//
// The Filter applies two passes before each frame is encoded for transmission:
//
//  1. Channel masking — strips telemetry fields that carry no diagnostic value in the
//     current inference class. E.g., solar voltage (always 0 in eclipse), attitude
//     (irrelevant during a power anomaly), or angular velocity (irrelevant during RF
//     degradation). Clearing the presence bit eliminates the field from the wire frame.
//
//  2. Delta suppression — when the spacecraft is NOMINAL and all channel values are
//     within configured thresholds of the last transmitted frame, the frame is silently
//     dropped. A keepalive is emitted every keepaliveEvery suppressed frames to keep
//     ground-station clocks synchronised.
//
// The Filter is safe for concurrent use.
package preprocess

import (
	"sync"
	"sync/atomic"

	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/absmach/orbitron/spacecraft/inference"
)

// Delta suppression thresholds (raw wire units).
const (
	deltaBatV    = 100  // mV
	deltaSolarV  = 300  // mV
	deltaTempC   = 200  // centidegrees (2 °C)
	deltaRSSI    = 50   // ×10 dBm (5 dBm)
	deltaAttRoll  = 500  // centidegrees (5°)
	deltaAttPitch = 500
	deltaAttYaw   = 1000 // centidegrees (10°)
	deltaAngVel   = 30   // centideg/s (0.3 deg/s)

	// keepaliveEvery: emit one frame for every N suppressed frames to keep GS clocks in sync.
	keepaliveEvery = 10
)

// Stats is a snapshot of the filter's lifetime reduction counters.
type Stats struct {
	FramesIn         int64
	FramesOut        int64
	Suppressed       int64
	ChannelMaskApply int64
}

// Filter applies channel masking and delta suppression to Orbitron telemetry frames.
type Filter struct {
	mu sync.Mutex

	last      *orbitron.Telemetry
	suppCount int

	statsIn         atomic.Int64
	statsOut        atomic.Int64
	statsSuppressed atomic.Int64
	statsMasked     atomic.Int64
}

// Apply returns a (possibly channel-masked) telemetry struct and whether to transmit it.
// When the second return value is false the caller should skip encoding and transmission.
func (f *Filter) Apply(tm orbitron.Telemetry, class inference.Class) (orbitron.Telemetry, bool) {
	f.statsIn.Add(1)

	masked := applyMask(tm, class)
	if masked.Presence != tm.Presence {
		f.statsMasked.Add(1)
	}

	// Delta suppression only applies to nominal frames.
	if class == inference.NOMINAL {
		f.mu.Lock()
		last := f.last
		suppCount := f.suppCount
		f.mu.Unlock()

		if last != nil && !exceedsDelta(masked, *last) {
			f.mu.Lock()
			f.suppCount++
			count := f.suppCount
			f.mu.Unlock()

			f.statsSuppressed.Add(1)

			if count%keepaliveEvery != 0 {
				return masked, false
			}
		}
		_ = suppCount // used via f.suppCount above
	}

	cp := masked
	f.mu.Lock()
	f.last = &cp
	f.suppCount = 0
	f.mu.Unlock()

	f.statsOut.Add(1)
	return masked, true
}

// Stats returns a snapshot of reduction counters.
func (f *Filter) Stats() Stats {
	return Stats{
		FramesIn:         f.statsIn.Load(),
		FramesOut:        f.statsOut.Load(),
		Suppressed:       f.statsSuppressed.Load(),
		ChannelMaskApply: f.statsMasked.Load(),
	}
}

// applyMask clears presence bits for channels that carry no diagnostic value
// under the given inference class.
func applyMask(tm orbitron.Telemetry, class inference.Class) orbitron.Telemetry {
	switch class {
	case inference.RF_DEGRADATION:
		// Minimal downlink: position + power + inference only.
		tm.Presence &^= orbitron.PresenceAttitude | orbitron.PresenceAngVel | orbitron.PresenceTempC
	case inference.POWER_ANOMALY:
		// Focus on power subsystem; drop kinematics.
		tm.Presence &^= orbitron.PresenceAttitude | orbitron.PresenceAngVel
	case inference.THERMAL_EVENT:
		// Focus on thermal; drop kinematics.
		tm.Presence &^= orbitron.PresenceAttitude | orbitron.PresenceAngVel
	case inference.ATTITUDE_INSTABILITY:
		// Focus on kinematics; drop power.
		tm.Presence &^= orbitron.PresenceBatV | orbitron.PresenceSolarV
	case inference.ECLIPSE_ENTRY, inference.ECLIPSE_COMPUTE:
		// SolarV is 0 in eclipse — no information content, save the 2 bytes.
		tm.Presence &^= orbitron.PresenceSolarV
	}
	return tm
}

// exceedsDelta returns true when any channel in curr differs from last
// by more than its configured threshold.
func exceedsDelta(curr, last orbitron.Telemetry) bool {
	return absDiffU16(curr.BatV, last.BatV) > deltaBatV ||
		absDiffU16(curr.SolarV, last.SolarV) > deltaSolarV ||
		absDiffI16(curr.TempC, last.TempC) > deltaTempC ||
		absDiffI16(curr.RSSI, last.RSSI) > deltaRSSI ||
		absDiffI16(curr.AttRoll, last.AttRoll) > deltaAttRoll ||
		absDiffI16(curr.AttPitch, last.AttPitch) > deltaAttPitch ||
		absDiffI16(curr.AttYaw, last.AttYaw) > deltaAttYaw ||
		absDiffI16(curr.AngVelX, last.AngVelX) > deltaAngVel ||
		absDiffI16(curr.AngVelY, last.AngVelY) > deltaAngVel ||
		absDiffI16(curr.AngVelZ, last.AngVelZ) > deltaAngVel
}

func absDiffU16(a, b uint16) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func absDiffI16(a, b int16) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}
