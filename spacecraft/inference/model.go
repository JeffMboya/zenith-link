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

var normRanges = [4][2]float64{
	{5000, 9000},
	{-30, 80},
	{-130, -40},
	{0, 25},
}

func normalize(v, min, max float64) float64 {
	if v <= min {
		return 0.0
	}
	if v >= max {
		return 1.0
	}
	return (v - min) / (max - min)
}

type lcg struct{ state uint64 }

const (
	lcgMul = 6364136223846793005
	lcgAdd = 1442695040888963407
)

func newLCG(seed uint64) *lcg { return &lcg{state: seed} }

func (g *lcg) float61() float64 {
	g.state = g.state*lcgMul + lcgAdd

	return float64(int64(g.state>>1)) / float64(int64(1)<<62)
}

func (g *lcg) xavierWeight(fanIn, fanOut int) float64 {
	bound := math.Sqrt(6.0 / float64(fanIn+fanOut))
	return g.float61() * bound
}

type layer struct {
	in, out int
	w       [][]float64
	b       []float64
	vw      [][]float64
	vb      []float64

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

	}
	return l
}

func (l *layer) forward(x []float64, act func(float64) float64) []float64 {
	copy(l.lastIn, x)
	for o := range l.out {
		sum := l.b[o]
		for i := range l.in {
			sum += l.w[o][i] * x[i]
		}
		l.lastOut[o] = act(sum)
	}

	out := make([]float64, l.out)
	copy(out, l.lastOut)
	return out
}

func (l *layer) backward(dLoss []float64, actDeriv func(float64) float64, lr, momentum float64) []float64 {

	delta := make([]float64, l.out)
	for o := range l.out {
		delta[o] = dLoss[o] * actDeriv(l.lastOut[o])
	}

	dX := make([]float64, l.in)
	for o := range l.out {
		for i := range l.in {
			dX[i] += delta[o] * l.w[o][i]
		}
	}

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

func tanhAct(x float64) float64      { return math.Tanh(x) }
func tanhDeriv(y float64) float64    { return 1 - y*y }
func sigmoidAct(x float64) float64   { return 1 / (1 + math.Exp(-x)) }
func sigmoidDeriv(y float64) float64 { return y * (1 - y) }
func linearAct(x float64) float64    { return x }
func linearDeriv(_ float64) float64  { return 1 }

type Autoencoder struct {
	enc0 *layer
	enc1 *layer

	dec0 *layer
	dec1 *layer

	frameCount   int
	warmupDone   bool
	warmupErrSum float64
	warmupErrCnt int
	threshold    float64
	adaptCounter int
}

const (
	warmupFrames = 50

	warmupPasses = 10

	baseLR         = 0.05
	adaptLR        = 1e-4
	adaptInterval  = 10
	momentumCoeff  = 0.9
	lcgSeed        = 0xdeadbeef12345678
	fallbackThresh = 0.05

	warmupCalibStart = warmupFrames / 2
)

func NewAutoencoder() *Autoencoder {
	return newAutoencoder()
}

func newAutoencoder() *Autoencoder {
	g := newLCG(lcgSeed)
	return &Autoencoder{
		enc0: newLayer(4, 8, g),
		enc1: newLayer(8, 2, g),
		dec0: newLayer(2, 8, g),
		dec1: newLayer(8, 4, g),
	}
}

func (ae *Autoencoder) normalizeInputs(batV, tempC, rssi, angVel float64) [4]float64 {
	return [4]float64{
		normalize(batV, normRanges[0][0], normRanges[0][1]),
		normalize(tempC, normRanges[1][0], normRanges[1][1]),
		normalize(rssi, normRanges[2][0], normRanges[2][1]),
		normalize(angVel, normRanges[3][0], normRanges[3][1]),
	}
}

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

func mse(x, recon [4]float64) float64 {
	sum := 0.0
	for i := range 4 {
		d := x[i] - recon[i]
		sum += d * d
	}
	return sum / 4.0
}

func (ae *Autoencoder) backward(x, recon [4]float64, lr float64) {

	dOut := make([]float64, 4)
	for i := range 4 {
		dOut[i] = 2.0 * (recon[i] - x[i]) / 4.0
	}

	dH2 := ae.dec1.backward(dOut, sigmoidDeriv, lr, momentumCoeff)
	dLatent := ae.dec0.backward(dH2, tanhDeriv, lr, momentumCoeff)
	dH1 := ae.enc1.backward(dLatent, linearDeriv, lr, momentumCoeff)
	_ = ae.enc0.backward(dH1, tanhDeriv, lr, momentumCoeff)
}

func (ae *Autoencoder) Push(batV, tempC, rssi, angVel float64) (mseVal float64, trained bool) {
	x := ae.normalizeInputs(batV, tempC, rssi, angVel)
	ae.frameCount++

	if !ae.warmupDone {

		var recon [4]float64
		for range warmupPasses {
			recon = ae.forward(x)
			ae.backward(x, recon, baseLR)
		}
		mseVal = mse(x, ae.forward(x))
		trained = true

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

	recon := ae.forward(x)
	mseVal = mse(x, recon)

	ae.adaptCounter++
	if ae.adaptCounter >= adaptInterval {
		ae.adaptCounter = 0
		ae.backward(x, recon, adaptLR)
		trained = true
	}
	return
}

func (ae *Autoencoder) Infer(batV, tempC, rssi, angVel float64) float64 {
	x := ae.normalizeInputs(batV, tempC, rssi, angVel)
	recon := ae.forward(x)
	return mse(x, recon)
}

func (ae *Autoencoder) Threshold() float64 {
	return ae.threshold
}

func (ae *Autoencoder) Warmed() bool {
	return ae.warmupDone
}
