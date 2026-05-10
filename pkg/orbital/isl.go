package orbital

import (
	"math"
	"time"
)

const islSampleStep = 30 * time.Second

// ISLContactWindows returns time intervals over [start, end) during which the
// distance between two satellites is at most maxDistKm. This approximates
// inter-satellite link (ISL) visibility for LEO constellations.
func ISLContactWindows(elemA, elemB Elements, start, end time.Time, maxDistKm float64) ([]ContactWindow, error) {
	maxDistM := maxDistKm * 1000

	var windows []ContactWindow
	var inContact bool
	var current ContactWindow

	for t := start; !t.After(end); t = t.Add(islSampleStep) {
		eciA, err := Propagate(elemA, t)
		if err != nil {
			return nil, err
		}
		eciB, err := Propagate(elemB, t)
		if err != nil {
			return nil, err
		}
		dx := eciA.X - eciB.X
		dy := eciA.Y - eciB.Y
		dz := eciA.Z - eciB.Z
		dist := math.Sqrt(dx*dx + dy*dy + dz*dz)

		if dist <= maxDistM {
			if !inContact {
				current = ContactWindow{AOS: t}
				inContact = true
			}
		} else {
			if inContact {
				current.LOS = t
				windows = append(windows, current)
				inContact = false
			}
		}
	}
	if inContact {
		current.LOS = end
		windows = append(windows, current)
	}
	return windows, nil
}
