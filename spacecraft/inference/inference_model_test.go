package inference_test

import (
	"testing"

	"github.com/absmach/zenith-link/spacecraft/inference"
)

// nominalAE returns typical nominal telemetry values for autoencoder tests.
func nominalAE() (batV, tempC, rssi, angVel float64) {
	return 7500, 20.0, -75.0, 0.05
}

// warmupAE pushes n nominal frames through the autoencoder to complete warmup.
func warmupAE(ae *inference.Autoencoder, n int) {
	batV, tempC, rssi, angVel := nominalAE()
	for range n {
		ae.Push(batV, tempC, rssi, angVel)
	}
}

// TestAutoencoder_WarmupTrains verifies that after 50 nominal frames the autoencoder
// reports Warmed()==true and Threshold()>0.
func TestAutoencoder_WarmupTrains(t *testing.T) {
	ae := inference.NewAutoencoder()
	if ae.Warmed() {
		t.Fatal("autoencoder should not be warmed before any frames")
	}
	if ae.Threshold() != 0 {
		t.Fatalf("threshold before warmup should be 0, got %v", ae.Threshold())
	}

	warmupAE(ae, 50)

	if !ae.Warmed() {
		t.Error("autoencoder should be warmed after 50 frames")
	}
	if ae.Threshold() <= 0 {
		t.Errorf("threshold should be positive after warmup, got %v", ae.Threshold())
	}
}

// TestAutoencoder_NominalLowError checks that nominal frames after warmup produce
// reconstruction error below the calibrated threshold.
func TestAutoencoder_NominalLowError(t *testing.T) {
	ae := inference.NewAutoencoder()
	warmupAE(ae, 50)

	batV, tempC, rssi, angVel := nominalAE()
	// Push a few post-warmup nominal frames so the network has had time to adapt.
	for range 20 {
		ae.Push(batV, tempC, rssi, angVel)
	}

	mseVal := ae.Infer(batV, tempC, rssi, angVel)
	thresh := ae.Threshold()
	if mseVal >= thresh {
		t.Errorf("nominal frame MSE %v should be below threshold %v after warmup", mseVal, thresh)
	}
}

// TestAutoencoder_AnomalyHighError verifies that a severely anomalous frame (extreme
// battery drop + high temperature) produces reconstruction error above the threshold.
func TestAutoencoder_AnomalyHighError(t *testing.T) {
	ae := inference.NewAutoencoder()
	warmupAE(ae, 50)
	// Push additional nominal frames to let the network settle.
	batV, tempC, rssi, angVel := nominalAE()
	for range 30 {
		ae.Push(batV, tempC, rssi, angVel)
	}

	// Severely anomalous: very low battery, very high temperature.
	mseVal := ae.Infer(3000, 70, -75.0, 0.05)
	thresh := ae.Threshold()
	if mseVal <= thresh {
		t.Errorf("anomalous frame MSE %v should exceed threshold %v", mseVal, thresh)
	}
}

// TestAutoencoder_NormalizationClamp verifies that out-of-range values don't panic
// and produce finite MSE values.
func TestAutoencoder_NormalizationClamp(t *testing.T) {
	ae := inference.NewAutoencoder()

	// Values far outside normalization ranges — should clamp, not panic.
	extremeCases := [][4]float64{
		{0, -100, -200, -10},    // all below minimum
		{20000, 200, 0, 1000},   // all above maximum
		{-1e9, 1e9, -1e9, 1e9}, // extreme values
	}
	for _, c := range extremeCases {
		mseVal, _ := ae.Push(c[0], c[1], c[2], c[3])
		if mseVal != mseVal { // NaN check
			t.Errorf("Push(%v) returned NaN MSE", c)
		}
		inferred := ae.Infer(c[0], c[1], c[2], c[3])
		if inferred != inferred {
			t.Errorf("Infer(%v) returned NaN MSE", c)
		}
	}
}

// TestNewDetector_HasAutoencoder verifies that NewDetector returns a Detector that
// integrates the autoencoder (ModelThreshold field populated after warmup).
func TestNewDetector_HasAutoencoder(t *testing.T) {
	d := inference.NewDetector()
	if d == nil {
		t.Fatal("NewDetector() returned nil")
	}

	f := inference.Frame{
		BatV:      7500,
		SolarV:    5200,
		TempC:     20,
		RSSI:      -75,
		AttRoll:   0.1,
		AttPitch:  0.1,
		AngVelMag: 0.05,
	}

	// Push enough frames to complete both Detector warmup (5) and autoencoder warmup (50).
	var lastResult inference.Result
	for range 60 {
		lastResult = d.Push(f)
	}

	// After 60 frames the autoencoder should be warmed and ModelThreshold should be set.
	if lastResult.ModelThreshold == 0 {
		t.Error("ModelThreshold should be non-zero after autoencoder warmup (60 frames)")
	}
	// ModelError should be a finite non-negative number.
	if lastResult.ModelError < 0 || lastResult.ModelError != lastResult.ModelError {
		t.Errorf("ModelError should be finite and non-negative, got %v", lastResult.ModelError)
	}
}
