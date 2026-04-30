package inference_test

import (
	"testing"

	"github.com/absmach/zenith-link/spacecraft/inference"
	"github.com/stretchr/testify/assert"
)

func nominalFrame() inference.Frame {
	return inference.Frame{
		BatV:      7500, // mV — nominal 3S LiPo
		SolarV:    5200, // mV — sunlight
		TempC:     20,   // °C
		RSSI:      -75,  // dBm
		AttRoll:   0.1,
		AttPitch:  0.1,
		AngVelMag: 0.05, // deg/s
	}
}

// warmup pushes n nominal frames so the detector has a baseline.
func warmup(d *inference.Detector, n int) {
	f := nominalFrame()
	for range n {
		d.Push(f)
	}
}

func TestPush_Warmup(t *testing.T) {
	var d inference.Detector
	f := nominalFrame()
	// Fewer than 5 frames → always NOMINAL with reduced confidence.
	for i := range 4 {
		res := d.Push(f)
		assert.Equal(t, inference.NOMINAL, res.Class, "frame %d should be NOMINAL during warmup", i)
		assert.Equal(t, uint8(128), res.Confidence, "warmup confidence should be 128")
	}
}

func TestPush_EclipseEntry(t *testing.T) {
	var d inference.Detector
	warmup(&d, 10)

	f := nominalFrame()
	f.SolarV = 0 // eclipse — SolarV < 100 mV
	res := d.Push(f)
	assert.Equal(t, inference.ECLIPSE_ENTRY, res.Class)
}

func TestPush_EclipseCompute(t *testing.T) {
	var d inference.Detector
	warmup(&d, 10)

	f := nominalFrame()
	f.SolarV = 0
	// Need 3+ consecutive shadow frames to settle to ECLIPSE_COMPUTE.
	d.Push(f)
	d.Push(f)
	res := d.Push(f)
	assert.Equal(t, inference.ECLIPSE_COMPUTE, res.Class)
	assert.GreaterOrEqual(t, res.Confidence, uint8(200))
}

func TestPush_PowerAnomaly(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	f.BatV = 5000 // severe voltage drop — should trigger z-score well beyond 2σ
	res := d.Push(f)
	assert.Equal(t, inference.POWER_ANOMALY, res.Class)
	assert.Less(t, res.Channels.BatV.Z, -2.0)
}

func TestPush_ThermalEvent(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	f.TempC = 60 // large positive deviation
	res := d.Push(f)
	assert.Equal(t, inference.THERMAL_EVENT, res.Class)
	assert.Greater(t, res.Channels.ChassisC.Z, 2.0)
}

func TestPush_AttitudeInstab(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	f.AngVelMag = 15 // reaction wheel saturation
	res := d.Push(f)
	assert.Equal(t, inference.ATTITUDE_INSTABILITY, res.Class)
	assert.Greater(t, res.Channels.AttVel.Z, 2.0)
}

func TestPush_RFDegradation(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	f.RSSI = -110 // link margin collapsing
	res := d.Push(f)
	assert.Equal(t, inference.RF_DEGRADATION, res.Class)
	assert.Less(t, res.Channels.RSSI.Z, -2.0)
}

func TestRollingStats_Floor(t *testing.T) {
	// A perfectly constant signal should not produce an infinite z-score.
	// The stddev floor prevents division by zero.
	var d inference.Detector
	f := nominalFrame()
	for range 35 {
		d.Push(f)
	}
	res := d.Push(f)
	assert.Equal(t, inference.NOMINAL, res.Class, "constant signal must stay NOMINAL despite zero variance")
	// Channels should be finite z-scores.
	assert.False(t, res.Channels.BatV.Z != res.Channels.BatV.Z, "BatV z should not be NaN")
	assert.False(t, res.Channels.ChassisC.Z != res.Channels.ChassisC.Z, "ChassisC z should not be NaN")
}

func TestLastResult(t *testing.T) {
	var d inference.Detector
	// Before any push, LastResult returns zero value (NOMINAL/0).
	r0 := d.LastResult()
	assert.Equal(t, inference.NOMINAL, r0.Class)

	warmup(&d, 10)
	f := nominalFrame()
	f.RSSI = -110
	pushed := d.Push(f)
	last := d.LastResult()
	// LastResult should return the same classification as the most recent Push.
	assert.Equal(t, pushed.Class, last.Class)
	assert.Equal(t, pushed.Confidence, last.Confidence)
}

func TestResultChannels(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	res := d.Push(f)
	// All channel stds must be positive (floor applied).
	assert.Greater(t, res.Channels.BatV.Std, 0.0)
	assert.Greater(t, res.Channels.ChassisC.Std, 0.0)
	assert.Greater(t, res.Channels.RSSI.Std, 0.0)
	assert.Greater(t, res.Channels.AttVel.Std, 0.0)
	// Means must be near the pushed nominal values.
	assert.InDelta(t, 7500.0, res.Channels.BatV.Mean, 10.0)
	assert.InDelta(t, 20.0, res.Channels.ChassisC.Mean, 1.0)
	assert.InDelta(t, -75.0, res.Channels.RSSI.Mean, 1.0)
}

func TestPreFaultClass(t *testing.T) {
	var d inference.Detector
	warmup(&d, 10)

	// Gradually drift BatV downward to trigger a pre-fault trend on bat_v.
	f := nominalFrame()
	for i := range 15 {
		f.BatV = 7500 - float64(i)*40 // slow decline
		d.Push(f)
	}
	res := d.LastResult()
	// The result should be either a pre-fault warning or an actual POWER_ANOMALY.
	// We accept either: the point is that the detector is tracking the trend.
	if res.Class == inference.NOMINAL {
		assert.NotEmpty(t, res.PreFaultChan, "drifting BatV should generate a pre-fault signal before threshold")
	}
}
