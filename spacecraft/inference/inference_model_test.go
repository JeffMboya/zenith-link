package inference_test

import (
	"testing"

	"github.com/absmach/zenith-link/spacecraft/inference"
)

func nominalAE() (batV, tempC, rssi, angVel float64) {
	return 7500, 20.0, -75.0, 0.05
}

func warmupAE(ae *inference.Autoencoder, n int) {
	batV, tempC, rssi, angVel := nominalAE()
	for range n {
		ae.Push(batV, tempC, rssi, angVel)
	}
}

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

func TestAutoencoder_NominalLowError(t *testing.T) {
	ae := inference.NewAutoencoder()
	warmupAE(ae, 50)

	batV, tempC, rssi, angVel := nominalAE()

	for range 20 {
		ae.Push(batV, tempC, rssi, angVel)
	}

	mseVal := ae.Infer(batV, tempC, rssi, angVel)
	thresh := ae.Threshold()
	if mseVal >= thresh {
		t.Errorf("nominal frame MSE %v should be below threshold %v after warmup", mseVal, thresh)
	}
}

func TestAutoencoder_AnomalyHighError(t *testing.T) {
	ae := inference.NewAutoencoder()
	warmupAE(ae, 50)

	batV, tempC, rssi, angVel := nominalAE()
	for range 30 {
		ae.Push(batV, tempC, rssi, angVel)
	}

	mseVal := ae.Infer(3000, 70, -75.0, 0.05)
	thresh := ae.Threshold()
	if mseVal <= thresh {
		t.Errorf("anomalous frame MSE %v should exceed threshold %v", mseVal, thresh)
	}
}

func TestAutoencoder_NormalizationClamp(t *testing.T) {
	ae := inference.NewAutoencoder()

	extremeCases := [][4]float64{
		{0, -100, -200, -10},
		{20000, 200, 0, 1000},
		{-1e9, 1e9, -1e9, 1e9},
	}
	for _, c := range extremeCases {
		mseVal, _ := ae.Push(c[0], c[1], c[2], c[3])
		if mseVal != mseVal {
			t.Errorf("Push(%v) returned NaN MSE", c)
		}
		inferred := ae.Infer(c[0], c[1], c[2], c[3])
		if inferred != inferred {
			t.Errorf("Infer(%v) returned NaN MSE", c)
		}
	}
}

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

	var lastResult inference.Result
	for range 60 {
		lastResult = d.Push(f)
	}

	if lastResult.ModelThreshold == 0 {
		t.Error("ModelThreshold should be non-zero after autoencoder warmup (60 frames)")
	}

	if lastResult.ModelError < 0 || lastResult.ModelError != lastResult.ModelError {
		t.Errorf("ModelError should be finite and non-negative, got %v", lastResult.ModelError)
	}
}
