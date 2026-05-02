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

var ClassNames = [7]string{
	"NOMINAL", "POWER_ANOMALY", "THERMAL_EVENT",
	"ATTITUDE_INSTABILITY", "RF_DEGRADATION", "ECLIPSE_ENTRY", "ECLIPSE_COMPUTE",
}

func ClassName(c Class) string {
	if int(c) < len(ClassNames) {
		return ClassNames[c]
	}
	return "UNKNOWN"
}

type Frame struct {
	BatV      float64
	SolarV    float64
	TempC     float64
	RSSI      float64
	AttRoll   float64
	AttPitch  float64
	AngVelMag float64
}

type ChannelDetail struct {
	Z    float64 `json:"z"`
	Mean float64 `json:"mean"`
	Std  float64 `json:"std"`
}

type ResultChannels struct {
	BatV     ChannelDetail `json:"bat_v"`
	ChassisC ChannelDetail `json:"chassis_c"`
	RSSI     ChannelDetail `json:"rssi"`
	AttVel   ChannelDetail `json:"att_vel"`
}

type Result struct {
	Class          Class
	Confidence     uint8
	Channels       ResultChannels
	PreFaultClass  string
	PreFaultChan   string
	ModelError     float64
	ModelThreshold float64
}

const (
	windowSize    = 30
	zThreshold    = 2.0
	eclipseSettle = 3
	preFaultSlope = 0.3
)

const (
	stdFloorBatV  = 50.0
	stdFloorTempC = 1.0
	stdFloorRSSI  = 3.0
	stdFloorAtt   = 1.0
)

type chanStats struct{ mean, std float64 }

const zHistLen = 3

type Detector struct {
	mu            sync.Mutex
	history       [windowSize]Frame
	count         int
	head          int
	eclipseStreak int
	lastResult    Result
	zHistory      [4][zHistLen]float64
	zHead         int
	ae            *Autoencoder
}

func NewDetector() *Detector {
	return &Detector{ae: newAutoencoder()}
}

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

func (d *Detector) LastResult() Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastResult
}

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

	var aeMSE float64
	if d.ae != nil {
		aeMSE, _ = d.ae.Push(f.BatV, f.TempC, f.RSSI, f.AngVelMag)
	}

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

	batSt := d.rollingStats(func(fr Frame) float64 { return fr.BatV }, stdFloorBatV)
	tempSt := d.rollingStats(func(fr Frame) float64 { return fr.TempC }, stdFloorTempC)
	rssiSt := d.rollingStats(func(fr Frame) float64 { return fr.RSSI }, stdFloorRSSI)
	attSt := d.rollingStats(func(fr Frame) float64 { return fr.AngVelMag }, stdFloorAtt)

	batZ := (f.BatV - batSt.mean) / batSt.std
	tempZ := (f.TempC - tempSt.mean) / tempSt.std
	rssiZ := (f.RSSI - rssiSt.mean) / rssiSt.std
	attZ := (f.AngVelMag - attSt.mean) / attSt.std

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

func (d *Detector) checkPreFault() (class, chanName string) {

	if d.count < zHistLen+5 {
		return "", ""
	}

	type channelCheck struct {
		name   string
		class  string
		negate bool
	}
	checks := [4]channelCheck{
		{"bat_v", "POWER_ANOMALY", true},
		{"chassis_c", "THERMAL_EVENT", false},
		{"rssi", "RF_DEGRADATION", true},
		{"att_vel", "ATTITUDE_INSTABILITY", false},
	}
	for i, ch := range checks {
		z0 := d.zHistory[i][d.zHead]
		z1 := d.zHistory[i][(d.zHead+1)%zHistLen]
		z2 := d.zHistory[i][(d.zHead+2)%zHistLen]
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
