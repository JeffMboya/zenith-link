// Package inference implements an onboard spacecraft health anomaly detector.
//
// A rolling 30-frame baseline maintains per-channel mean and standard deviation.
// Each new telemetry frame is classified by comparing its channel values against
// the rolling baseline using z-scores with per-channel stddev floors — the same
// pattern Satlyt deployed in STL-01 over constrained 20 KB/s links.
//
// Eclipse state is detected directly from SolarV (< 100 mV = shadow), which is
// unambiguous and avoids false positives from the power-channel z-score during
// the sunlight/eclipse transition step. Once the eclipse settles (3+ consecutive
// frames), the classification upgrades from ECLIPSE_ENTRY to ECLIPSE_COMPUTE —
// the Star Catcher use case: shadow = thermally cold, RF-quiet, no solar noise,
// optimal window for compute-intensive workloads.
package inference

import (
	"math"
	"sync"
)

// Class identifies the spacecraft health classification output by the detector.
type Class uint8

const (
	NOMINAL              Class = 0
	POWER_ANOMALY        Class = 1
	THERMAL_EVENT        Class = 2
	ATTITUDE_INSTABILITY Class = 3
	RF_DEGRADATION       Class = 4
	ECLIPSE_ENTRY        Class = 5
	ECLIPSE_COMPUTE      Class = 6
)

// ClassNames maps Class values to labels. Index-stable — used in the wire protocol.
var ClassNames = [7]string{
	"NOMINAL", "POWER_ANOMALY", "THERMAL_EVENT",
	"ATTITUDE_INSTABILITY", "RF_DEGRADATION", "ECLIPSE_ENTRY", "ECLIPSE_COMPUTE",
}

// ClassName returns the label for a Class value.
func ClassName(c Class) string {
	if int(c) < len(ClassNames) {
		return ClassNames[c]
	}
	return "UNKNOWN"
}

// Frame holds engineering-unit telemetry values for one detector sample.
// Callers must convert raw wire-protocol integers to engineering units before pushing.
type Frame struct {
	BatV      float64 // battery voltage [mV]
	SolarV    float64 // solar panel voltage [mV]
	TempC     float64 // chassis temperature [°C]
	RSSI      float64 // received signal strength [dBm]
	AttRoll   float64 // roll angle [deg]
	AttPitch  float64 // pitch angle [deg]
	AngVelMag float64 // angular velocity magnitude [deg/s]
}

// ChannelDetail holds the z-score and baseline stats for one telemetry channel.
type ChannelDetail struct {
	Z    float64 `json:"z"`    // current z-score against rolling baseline
	Mean float64 `json:"mean"` // rolling mean
	Std  float64 `json:"std"`  // rolling std (floored)
}

// ResultChannels carries per-channel z-scores for the UI tooltip and pre-fault logic.
// Fixed struct (not map) — zero heap allocation per telemetry frame.
type ResultChannels struct {
	BatV     ChannelDetail `json:"bat_v"`
	ChassisC ChannelDetail `json:"chassis_c"`
	RSSI     ChannelDetail `json:"rssi"`
	AttVel   ChannelDetail `json:"att_vel"` // angular velocity magnitude z-score
}

// Result is the detector output for one frame.
type Result struct {
	Class          Class
	Confidence     uint8 // 0–255 (255 ≈ 100%)
	Channels       ResultChannels
	PreFaultClass  string  // non-empty when a channel is trending toward threshold
	PreFaultChan   string  // which channel is trending ("bat_v", "chassis_c", etc.)
	ModelError     float64 // autoencoder reconstruction MSE (0 until warmed)
	ModelThreshold float64 // calibrated MSE threshold (0 until warmup complete)
}

const (
	windowSize    = 30
	zThreshold    = 2.0
	eclipseSettle = 3   // consecutive shadow frames before ECLIPSE_COMPUTE fires
	preFaultSlope = 0.3 // z-score increase per frame that triggers a PRE-FAULT warning
)

// Per-channel stddev floors prevent z-score explosions on near-constant signals
// (e.g. BatV step-functions between 7400 and 7600 mV at eclipse boundaries).
const (
	stdFloorBatV  = 50.0 // mV
	stdFloorTempC = 1.0  // °C
	stdFloorRSSI  = 3.0  // dBm
	stdFloorAtt   = 1.0  // deg/s (applied to AngVelMag)
)

type chanStats struct{ mean, std float64 }

// zHistory tracks the last 3 z-scores per channel to detect rising trends.
// Indices: 0=BatV, 1=ChassisC, 2=RSSI, 3=AttVel. Oldest→newest left→right.
// Ring: head points to the slot for the next write (oldest value).
const zHistLen = 3

// Detector maintains a rolling telemetry baseline and classifies spacecraft health.
// Safe for concurrent use.
type Detector struct {
	mu            sync.Mutex
	history       [windowSize]Frame
	count         int
	head          int
	eclipseStreak int // consecutive frames with SolarV < 100 mV
	lastResult    Result
	zHistory      [4][zHistLen]float64 // ring buffer: [channel][frame], oldest first
	zHead         int                  // next write slot in zHistory ring
	ae            *Autoencoder         // onboard neural-net anomaly supplement
}

// NewDetector creates a Detector with a freshly initialised Autoencoder.
// Use this instead of a bare struct literal so the autoencoder is ready to train.
func NewDetector() *Detector {
	return &Detector{ae: newAutoencoder()}
}

// Push adds a telemetry frame to the rolling window and returns the health classification.
// The first 4 frames return NOMINAL with reduced confidence while the window warms up.
func (d *Detector) Push(f Frame) Result {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.history[d.head] = f
	d.head = (d.head + 1) % windowSize
	if d.count < windowSize {
		d.count++
	}

	if f.SolarV < 100 {
		d.eclipseStreak++
	} else {
		d.eclipseStreak = 0
	}

	if d.count < 5 {
		d.lastResult = Result{Class: NOMINAL, Confidence: 128}
		return d.lastResult
	}
	d.lastResult = d.classify(f)
	return d.lastResult
}

// LastResult returns the cached result from the most recent Push call.
// Does not re-classify. Returns NOMINAL/128 if no frame has been pushed yet.
func (d *Detector) LastResult() Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastResult
}

// Latest returns the classification of the last pushed frame without modifying state.
// Returns NOMINAL/128 if fewer than 5 frames have been pushed.
// Deprecated: use LastResult() for the cached result without re-classification.
func (d *Detector) Latest() Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.count < 5 {
		return Result{Class: NOMINAL, Confidence: 128}
	}
	prev := (d.head - 1 + windowSize) % windowSize
	return d.classify(d.history[prev])
}

func (d *Detector) classify(f Frame) Result {
	// Train the autoencoder on every frame — including eclipse frames — to maintain
	// training continuity across all operating modes.
	var aeMSE float64
	if d.ae != nil {
		aeMSE, _ = d.ae.Push(f.BatV, f.TempC, f.RSSI, f.AngVelMag)
	}

	// Eclipse is detected from SolarV — unambiguous and avoids BatV transition noise.
	if f.SolarV < 100 {
		var res Result
		if d.eclipseStreak >= eclipseSettle {
			res = Result{Class: ECLIPSE_COMPUTE, Confidence: 220}
		} else {
			res = Result{Class: ECLIPSE_ENTRY, Confidence: 200}
		}
		if d.ae != nil {
			res.ModelError = aeMSE
			res.ModelThreshold = d.ae.Threshold()
		}
		return res
	}

	batSt  := d.rollingStats(func(fr Frame) float64 { return fr.BatV }, stdFloorBatV)
	tempSt := d.rollingStats(func(fr Frame) float64 { return fr.TempC }, stdFloorTempC)
	rssiSt := d.rollingStats(func(fr Frame) float64 { return fr.RSSI }, stdFloorRSSI)
	attSt  := d.rollingStats(func(fr Frame) float64 { return fr.AngVelMag }, stdFloorAtt)

	batZ  := (f.BatV - batSt.mean) / batSt.std
	tempZ := (f.TempC - tempSt.mean) / tempSt.std
	rssiZ := (f.RSSI - rssiSt.mean) / rssiSt.std
	attZ  := (f.AngVelMag - attSt.mean) / attSt.std

	// Update zHistory ring — channel order: BatV, ChassisC, RSSI, AttVel.
	d.zHistory[0][d.zHead] = batZ
	d.zHistory[1][d.zHead] = tempZ
	d.zHistory[2][d.zHead] = rssiZ
	d.zHistory[3][d.zHead] = attZ
	d.zHead = (d.zHead + 1) % zHistLen

	channels := ResultChannels{
		BatV:     ChannelDetail{Z: batZ, Mean: batSt.mean, Std: batSt.std},
		ChassisC: ChannelDetail{Z: tempZ, Mean: tempSt.mean, Std: tempSt.std},
		RSSI:     ChannelDetail{Z: rssiZ, Mean: rssiSt.mean, Std: rssiSt.std},
		AttVel:   ChannelDetail{Z: attZ, Mean: attSt.mean, Std: attSt.std},
	}

	// Pre-fault detection: check if any channel's z-score slope is rising toward threshold.
	preFaultClass, preFaultChan := d.checkPreFault()

	var res Result
	switch {
	case batZ < -zThreshold:
		res = Result{Class: POWER_ANOMALY, Confidence: clamp(200 + int((-batZ-zThreshold)*20))}
	case tempZ > zThreshold:
		res = Result{Class: THERMAL_EVENT, Confidence: clamp(200 + int((tempZ-zThreshold)*20))}
	case attZ > zThreshold:
		res = Result{Class: ATTITUDE_INSTABILITY, Confidence: clamp(195 + int((attZ-zThreshold)*15))}
	case rssiZ < -zThreshold:
		res = Result{Class: RF_DEGRADATION, Confidence: clamp(195 + int((-rssiZ-zThreshold)*15))}
	default:
		maxZ := math.Max(math.Max(-batZ, tempZ), math.Max(attZ, -rssiZ))
		res = Result{Class: NOMINAL, Confidence: clamp(255 - int(maxZ*30))}
	}

	res.Channels = channels
	res.PreFaultClass = preFaultClass
	res.PreFaultChan = preFaultChan

	// Autoencoder supplement: if the network is warmed and reconstruction error
	// exceeds the calibrated threshold, flag MODEL_ANOMALY on an otherwise-NOMINAL frame.
	// This catches correlated multi-channel drift that the per-channel z-score misses.
	if d.ae != nil {
		res.ModelError = aeMSE
		res.ModelThreshold = d.ae.Threshold()
		if d.ae.Warmed() && aeMSE > d.ae.Threshold() && res.Class == NOMINAL {
			res.PreFaultClass = "MODEL_ANOMALY"
			res.PreFaultChan = "autoencoder"
		}
	}

	return res
}

// checkPreFault checks the zHistory ring for a rising slope on any channel.
// Returns the anomaly class and channel name if a trend is detected, empty strings otherwise.
// Caller must hold d.mu.
func (d *Detector) checkPreFault() (class, chanName string) {
	// Need at least zHistLen pushes to have a valid slope.
	if d.count < zHistLen+5 {
		return "", ""
	}
	// Read oldest→newest from ring. zHead points to the next write slot (oldest value).
	type channelCheck struct {
		name      string
		class     string
		negate    bool // true = negative direction is anomalous (BatV, RSSI drop)
	}
	checks := [4]channelCheck{
		{"bat_v", "POWER_ANOMALY", true},
		{"chassis_c", "THERMAL_EVENT", false},
		{"rssi", "RF_DEGRADATION", true},
		{"att_vel", "ATTITUDE_INSTABILITY", false},
	}
	for i, ch := range checks {
		z0 := d.zHistory[i][d.zHead]                    // oldest
		z1 := d.zHistory[i][(d.zHead+1)%zHistLen]       // middle
		z2 := d.zHistory[i][(d.zHead+2)%zHistLen]       // newest (just written)
		slope := (z2 - z0) / 2.0
		if ch.negate {
			slope = -slope
		}
		if slope >= preFaultSlope && math.Abs(z2) > 0.8 && math.Abs(z2) < zThreshold {
			_ = z1
			return ch.class, ch.name
		}
	}
	return "", ""
}

func (d *Detector) rollingStats(extract func(Frame) float64, floor float64) chanStats {
	n := d.count
	sum := 0.0
	for i := range n {
		sum += extract(d.history[i])
	}
	mean := sum / float64(n)

	varSum := 0.0
	for i := range n {
		dx := extract(d.history[i]) - mean
		varSum += dx * dx
	}
	std := math.Sqrt(varSum / float64(n))
	if std < floor {
		std = floor
	}
	return chanStats{mean: mean, std: std}
}

func clamp(v int) uint8 {
	if v > 255 {
		return 255
	}
	if v < 0 {
		return 0
	}
	return uint8(v)
}
