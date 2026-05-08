package link

import "math/rand/v2"

// ShouldDrop returns true with probability lossRate, simulating frame loss from
// bit errors on a constrained ISL. lossRate is clamped to [0.0, 1.0].
func ShouldDrop(lossRate float64) bool {
	if lossRate <= 0 {
		return false
	}
	if lossRate >= 1 {
		return true
	}
	return rand.Float64() < lossRate
}
