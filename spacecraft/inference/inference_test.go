package inference_test

import (
	"testing"

	"github.com/absmach/satlyt-demo/spacecraft/inference"
	"github.com/stretchr/testify/assert"
)

func nominalFrame() inference.Frame {
	return inference.Frame{
		BatV:      7500,
		SolarV:    5200,
		TempC:     20,
		RSSI:      -75,
		AttRoll:   0.1,
		AttPitch:  0.1,
		AngVelMag: 0.05,
	}
}

func warmup(d *inference.Detector, n int) {
	f := nominalFrame()
	for range n {
		d.Push(f)
	}
}

func TestPush_Warmup(t *testing.T) {
	var d inference.Detector
	f := nominalFrame()

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
	f.SolarV = 0
	res := d.Push(f)
	assert.Equal(t, inference.ECLIPSE_ENTRY, res.Class)
}

func TestPush_EclipseCompute(t *testing.T) {
	var d inference.Detector
	warmup(&d, 10)

	f := nominalFrame()
	f.SolarV = 0

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
	f.BatV = 5000
	res := d.Push(f)
	assert.Equal(t, inference.POWER_ANOMALY, res.Class)
	assert.Less(t, res.Channels.BatV.Z, -2.0)
}

func TestPush_ThermalEvent(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	f.TempC = 60
	res := d.Push(f)
	assert.Equal(t, inference.THERMAL_EVENT, res.Class)
	assert.Greater(t, res.Channels.ChassisC.Z, 2.0)
}

func TestPush_AttitudeInstab(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	f.AngVelMag = 15
	res := d.Push(f)
	assert.Equal(t, inference.ATTITUDE_INSTABILITY, res.Class)
	assert.Greater(t, res.Channels.AttVel.Z, 2.0)
}

func TestPush_RFDegradation(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	f.RSSI = -110
	res := d.Push(f)
	assert.Equal(t, inference.RF_DEGRADATION, res.Class)
	assert.Less(t, res.Channels.RSSI.Z, -2.0)
}

func TestRollingStats_Floor(t *testing.T) {

	var d inference.Detector
	f := nominalFrame()
	for range 35 {
		d.Push(f)
	}
	res := d.Push(f)
	assert.Equal(t, inference.NOMINAL, res.Class, "constant signal must stay NOMINAL despite zero variance")

	assert.False(t, res.Channels.BatV.Z != res.Channels.BatV.Z, "BatV z should not be NaN")
	assert.False(t, res.Channels.ChassisC.Z != res.Channels.ChassisC.Z, "ChassisC z should not be NaN")
}

func TestLastResult(t *testing.T) {
	var d inference.Detector

	r0 := d.LastResult()
	assert.Equal(t, inference.NOMINAL, r0.Class)

	warmup(&d, 10)
	f := nominalFrame()
	f.RSSI = -110
	pushed := d.Push(f)
	last := d.LastResult()

	assert.Equal(t, pushed.Class, last.Class)
	assert.Equal(t, pushed.Confidence, last.Confidence)
}

func TestResultChannels(t *testing.T) {
	var d inference.Detector
	warmup(&d, 30)

	f := nominalFrame()
	res := d.Push(f)

	assert.Greater(t, res.Channels.BatV.Std, 0.0)
	assert.Greater(t, res.Channels.ChassisC.Std, 0.0)
	assert.Greater(t, res.Channels.RSSI.Std, 0.0)
	assert.Greater(t, res.Channels.AttVel.Std, 0.0)

	assert.InDelta(t, 7500.0, res.Channels.BatV.Mean, 10.0)
	assert.InDelta(t, 20.0, res.Channels.ChassisC.Mean, 1.0)
	assert.InDelta(t, -75.0, res.Channels.RSSI.Mean, 1.0)
}

func TestPreFaultClass(t *testing.T) {
	var d inference.Detector
	warmup(&d, 10)

	f := nominalFrame()
	for i := range 15 {
		f.BatV = 7500 - float64(i)*40
		d.Push(f)
	}
	res := d.LastResult()

	if res.Class == inference.NOMINAL {
		assert.NotEmpty(t, res.PreFaultChan, "drifting BatV should generate a pre-fault signal before threshold")
	}
}
