// Package cloudcover simulates atmospheric cloud cover probability over ground stations.
//
// The model is deterministic and time-varying — a combination of sinusoidal components
// keyed to ground-station coordinates. Returned values range 0.0 (clear) to 1.0 (overcast).
// A coverage index at or above DefaultThreshold is considered link-impairing for ISL routing.
package cloudcover

import (
	"math"
	"time"
)

// DefaultThreshold is the cloud cover index at which downlink is considered impaired.
const DefaultThreshold = 0.65

// Index returns a simulated cloud cover probability [0, 1] for the given
// ground-station location at time t.
//
// The model combines a slow diurnal wave, high-frequency convective turbulence keyed
// to latitude, and an hourly weather-pattern drift. All components are deterministic
// so that multiple relay nodes computing the same GS at the same moment agree.
func Index(gsLat, gsLon float64, t time.Time) float64 {
	dayFrac := float64(t.Hour()*3600+t.Minute()*60+t.Second()) / 86400.0
	lonRad := gsLon * math.Pi / 180
	latRad := gsLat * math.Pi / 90

	slow := 0.5 + 0.25*math.Sin(2*math.Pi*dayFrac+lonRad)
	fast := 0.15 * math.Sin(2*math.Pi*dayFrac*7.3+latRad*3)
	drift := 0.10 * math.Sin(float64(t.Unix()) * 2 * math.Pi / 3600)

	v := slow + fast + drift
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// IsImpaired returns true when the cloud cover index meets or exceeds threshold.
func IsImpaired(gsLat, gsLon float64, t time.Time, threshold float64) bool {
	return Index(gsLat, gsLon, t) >= threshold
}
