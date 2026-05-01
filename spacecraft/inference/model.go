// Package inference — autoencoder model for onboard spacecraft health anomaly detection.
//
// Architecture: 4→8→2→8→4 fully-connected autoencoder trained online via SGD with
// momentum. No external dependencies — pure Go, importable on constrained hardware.
// Mirrors Satlyt's STL-01 deployment philosophy: a 26 KB-class statistical model that
// runs autonomously, every frame, with no ground contact required.
//
// The bottleneck latent space (2 neurons) forces the network to learn a compressed
// manifold of nominal spacecraft behaviour. Frames that reconstruct poorly indicate
// operating conditions the network has never seen — structural anomalies the z-score
// detector may miss (correlated multi-channel drift, subtle long-term trends).
package inference

import "math"

// normRanges defines the [min, max] clamp bounds for each input feature.
// Order matches the layer input: [BatV mV, TempC °C, RSSI dBm, AngVelMag deg/s].
var normRanges = [4][2]float64{
	{5000, 9000},   // BatV  [mV]
	{-30, 80},      // TempC [°C]
	{-130, -40},    // RSSI  [dBm]
	{0, 25},        // AngVelMag [deg/s]
}

// normalize maps a raw value into [0, 1] using pre-defined per-channel ranges.
// Values outside the range are clamped rather than extrapolated.
func normalize(v, min, max float64) float64 {
	if v <= min {
		return 0.0
	}
	if v >= max {
		return 1.0
	}
	return (v - min) / (max - min)
}

// lcg is a simple Linear Congruential Generator used for deterministic weight
// initialisation. Same seed on every cold-start → identical initial weights →
// reproducible behaviour across spacecraft resets.
type lcg struct{ state uint64 }

const (
	lcgMul = 6364136223846793005
	lcgAdd = 1442695040888963407
)

func newLCG(seed uint64) *lcg { return &lcg{state: seed} }

// float61 returns a float64 in [-1, 1].
func (g *lcg) float61() float64 {
	g.state = g.state*lcgMul + lcgAdd
	// Shift right 1 bit so the sign bit of int64 is always 0.
	return float64(int64(g.state>>1)) / float64(int64(1)<<62)
}

// xavierWeight draws one weight from Xavier/Glorot uniform distribution:
//   w ~ Uniform(-sqrt(6/(fi+fo)), +sqrt(6/(fi+fo)))
func (g *lcg) xavierWeight(fanIn, fanOut int) float64 {
	bound := math.Sqrt(6.0 / float64(fanIn+fanOut))
	return g.float61() * bound
}

// layer is a fully-connected layer with its weight matrix, bias, velocity buffers, and
// accumulated gradients (for the backward pass). Weight layout: w[out][in].
type layer struct {
	in, out int
	w       [][]float64 // [out][in]
	b       []float64   // [out]
	vw      [][]float64 // momentum for weights
	vb      []float64   // momentum for biases
	// forward-pass cache (reused between forward and backward).
	lastIn  []float64
	lastOut []float64
}

func newLayer(in, out int, g *lcg) *layer {
	l := &layer{
		in:      in,
		out:     out,
		w:       make([][]float64, out),
		b:       make([]float64, out),
		vw:      make([][]float64, out),
		vb:      make([]float64, out),
		lastIn:  make([]float64, in),
		lastOut: make([]float64, out),
	}
	for o := range out {
		l.w[o] = make([]float64, in)
		l.vw[o] = make([]float64, in)
		for i := range in {
			l.w[o][i] = g.xavierWeight(in, out)
		}
		// biases initialised to zero (standard practice).
	}
	return l
}

// forward computes y = activation(Wx + b), caches input and output.
func (l *layer) forward(x []float64, act func(float64) float64) []float64 {
	copy(l.lastIn, x)
	for o := range l.out {
		sum := l.b[o]
		for i := range l.in {
			sum += l.w[o][i] * x[i]
		}
		l.lastOut[o] = act(sum)
	}
	// Return a copy so the caller owns the slice.
	out := make([]float64, l.out)
	copy(out, l.lastOut)
	return out
}

// backward computes gradients and applies SGD with momentum.
// dLoss is the gradient of the loss w.r.t. this layer's output (∂L/∂y).
// Returns ∂L/∂x (gradient to propagate to the previous layer).
func (l *layer) backward(dLoss []float64, actDeriv func(float64) float64, lr, momentum float64) []float64 {
	// delta_o = dLoss_o * act'(lastOut_o)
	delta := make([]float64, l.out)
	for o := range l.out {
		delta[o] = dLoss[o] * actDeriv(l.lastOut[o])
	}

	// ∂L/∂x_i = Σ_o delta_o * w[o][i]
	dX := make([]float64, l.in)
	for o := range l.out {
		for i := range l.in {
			dX[i] += delta[o] * l.w[o][i]
		}
	}

	// SGD with momentum: v = momentum*v - lr*∂L/∂w; w += v
	for o := range l.out {
		for i := range l.in {
			grad := delta[o] * l.lastIn[i]
			l.vw[o][i] = momentum*l.vw[o][i] - lr*grad
			l.w[o][i] += l.vw[o][i]
		}
		l.vb[o] = momentum*l.vb[o] - lr*delta[o]
		l.b[o] += l.vb[o]
	}
	return dX
}

// --- Activation functions and their derivatives ---

func tanhAct(x float64) float64    { return math.Tanh(x) }
func tanhDeriv(y float64) float64  { return 1 - y*y } // deriv w.r.t. output (post-act)
func sigmoidAct(x float64) float64 { return 1 / (1 + math.Exp(-x)) }
func sigmoidDeriv(y float64) float64 { return y * (1 - y) }
func linearAct(x float64) float64   { return x }
func linearDeriv(_ float64) float64  { return 1 }

// Autoencoder is a 4→8→2→8→4 fully-connected autoencoder trained online.
// It maintains its own training lifecycle: warmup (first 50 frames) followed by
// slow continuous adaptation. Not safe for concurrent use; callers must synchronise.
type Autoencoder struct {
	// Encoder layers.
	enc0 *layer // 4→8 tanh
	enc1 *layer // 8→2 linear (latent)
	// Decoder layers.
	dec0 *layer // 2→8 tanh
	dec1 *layer // 8→4 sigmoid

	// Training state.
	frameCount    int
	warmupDone    bool
	warmupErrSum  float64
	warmupErrCnt  int
	threshold     float64
	adaptCounter  int // counts frames since last slow-adapt step
}

const (
	warmupFrames = 50
	// warmupPasses is the number of SGD gradient steps taken per telemetry frame
	// during the warmup phase. A spacecraft onboard computer runs multiple iterations
	// between telemetry ticks — this lets the 4→2 bottleneck converge in exactly
	// 50 frames (500 total gradient steps at baseLR=0.05).
	warmupPasses = 10
	// baseLR is the learning rate for warmup-phase gradient descent.
	// Combined with warmupPasses, 50 frames × 10 steps × lr=0.05 converges the
	// 4→2 bottleneck to a tight nominal manifold in ~500 gradient steps.
	baseLR        = 0.05
	adaptLR       = 1e-4
	adaptInterval = 10
	momentumCoeff = 0.9
	lcgSeed       = 0xdeadbeef12345678
	fallbackThresh = 0.05
	// warmupCalibStart is the frame index (inclusive) from which errors are
	// accumulated for threshold calibration. Using the second half of warmup
	// (frames 25–49) ensures only the converged-regime error is used; early
	// high-error frames would otherwise inflate the threshold above what an
	// anomaly can ever produce (MSE is bounded by [0,0.25] for sigmoid outputs).
	warmupCalibStart = warmupFrames / 2
)

// NewAutoencoder constructs a fresh Autoencoder with deterministic Xavier weights.
// Exported so tests and external diagnostics tools can instantiate one directly.
func NewAutoencoder() *Autoencoder {
	return newAutoencoder()
}

// newAutoencoder is the internal constructor used by NewDetector.
func newAutoencoder() *Autoencoder {
	g := newLCG(lcgSeed)
	return &Autoencoder{
		enc0: newLayer(4, 8, g),
		enc1: newLayer(8, 2, g),
		dec0: newLayer(2, 8, g),
		dec1: newLayer(8, 4, g),
	}
}

// normalizeInputs maps raw telemetry values to [0,1].
func (ae *Autoencoder) normalizeInputs(batV, tempC, rssi, angVel float64) [4]float64 {
	return [4]float64{
		normalize(batV,   normRanges[0][0], normRanges[0][1]),
		normalize(tempC,  normRanges[1][0], normRanges[1][1]),
		normalize(rssi,   normRanges[2][0], normRanges[2][1]),
		normalize(angVel, normRanges[3][0], normRanges[3][1]),
	}
}

// forward runs a full encoder→decoder pass and returns the reconstruction.
func (ae *Autoencoder) forward(x [4]float64) [4]float64 {
	in := x[:]
	h1 := ae.enc0.forward(in, tanhAct)
	latent := ae.enc1.forward(h1, linearAct)
	h2 := ae.dec0.forward(latent, tanhAct)
	out := ae.dec1.forward(h2, sigmoidAct)
	var rec [4]float64
	copy(rec[:], out)
	return rec
}

// mse computes mean squared error between input and reconstruction.
func mse(x, recon [4]float64) float64 {
	sum := 0.0
	for i := range 4 {
		d := x[i] - recon[i]
		sum += d * d
	}
	return sum / 4.0
}

// backward runs a backpropagation pass given the input and reconstruction, updating
// all weights with SGD+momentum at the given learning rate.
func (ae *Autoencoder) backward(x, recon [4]float64, lr float64) {
	// ∂MSE/∂recon_i = 2*(recon_i - x_i)/4
	dOut := make([]float64, 4)
	for i := range 4 {
		dOut[i] = 2.0 * (recon[i] - x[i]) / 4.0
	}

	// Propagate through dec1 (sigmoid), dec0 (tanh), enc1 (linear), enc0 (tanh).
	dH2 := ae.dec1.backward(dOut, sigmoidDeriv, lr, momentumCoeff)
	dLatent := ae.dec0.backward(dH2, tanhDeriv, lr, momentumCoeff)
	dH1 := ae.enc1.backward(dLatent, linearDeriv, lr, momentumCoeff)
	_ = ae.enc0.backward(dH1, tanhDeriv, lr, momentumCoeff)
}

// Push trains (if warming up) or slowly adapts, and returns the reconstruction MSE.
// trained=true if this call triggered a weight update.
//
// During warmup, each frame gets warmupPasses gradient-descent steps — multi-pass
// training over a single sample, the same pattern used by resource-constrained edge
// compute nodes that process telemetry faster than they receive new frames.
// Threshold calibration uses only the second half of warmup (frames warmupCalibStart..49)
// to avoid inflating the threshold with high-error early-training frames.
func (ae *Autoencoder) Push(batV, tempC, rssi, angVel float64) (mseVal float64, trained bool) {
	x := ae.normalizeInputs(batV, tempC, rssi, angVel)
	ae.frameCount++

	if !ae.warmupDone {
		// Multi-pass warmup: run warmupPasses gradient steps per frame.
		var recon [4]float64
		for range warmupPasses {
			recon = ae.forward(x)
			ae.backward(x, recon, baseLR)
		}
		mseVal = mse(x, ae.forward(x))
		trained = true

		// Calibrate threshold from the second half of warmup only (converged regime).
		if ae.frameCount > warmupCalibStart {
			ae.warmupErrSum += mseVal
			ae.warmupErrCnt++
		}

		if ae.frameCount >= warmupFrames {
			ae.warmupDone = true
			if ae.warmupErrCnt > 0 {
				ae.threshold = 3.0 * (ae.warmupErrSum / float64(ae.warmupErrCnt))
			} else {
				ae.threshold = fallbackThresh
			}
			if ae.threshold == 0 {
				ae.threshold = fallbackThresh
			}
		}
		return
	}

	// Post-warmup inference + slow adaptation.
	recon := ae.forward(x)
	mseVal = mse(x, recon)

	// Slow adaptation every adaptInterval frames.
	ae.adaptCounter++
	if ae.adaptCounter >= adaptInterval {
		ae.adaptCounter = 0
		ae.backward(x, recon, adaptLR)
		trained = true
	}
	return
}

// Infer returns the reconstruction MSE for the given inputs without updating weights.
func (ae *Autoencoder) Infer(batV, tempC, rssi, angVel float64) float64 {
	x := ae.normalizeInputs(batV, tempC, rssi, angVel)
	recon := ae.forward(x)
	return mse(x, recon)
}

// Threshold returns the calibrated anomaly threshold.
// Returns 0 until warmup is complete.
func (ae *Autoencoder) Threshold() float64 {
	return ae.threshold
}

// Warmed reports whether the training warmup phase has completed.
func (ae *Autoencoder) Warmed() bool {
	return ae.warmupDone
}
