package api

// Constellation endpoint: returns current geodetic positions for all 16 simulated
// satellites, computed from Keplerian elements using the same J2 propagator as the
// primary spacecraft.
//
// Design: 4 orbital planes × 4 satellites per plane, equally phased within each plane.
//
//	Plane A (51.6°)  — ISS-like, matches AT-1's family
//	Plane B (97.5°)  — Sun-synchronous, polar north-south tracks
//	Plane C (28.5°)  — Low-inclination, fast east-west motion
//	Plane D (70.0°)  — High-inclination, steep diagonal tracks

import (
	"math"
	"net/http"
	"time"

	"github.com/absmach/zenith-link/pkg/orbital"
)

const deg = math.Pi / 180

// constellationSat defines one member of the simulated constellation.
type constellationSat struct {
	id  string
	sma float64 // semi-major axis [m]
	ecc float64
	inc float64 // inclination [rad]
	ran float64 // RAAN [rad]
	ma  float64 // mean anomaly at epoch [rad]
}

// primarySats defines the primary spacecraft. AT-1 is the only simulated node
// with a live telemetry backend; AT-2..16 have been removed.
var (
	constellationEpoch = time.Now().UTC()

	constellation16 = []constellationSat{
		{"AT-1", 6_788_000, 0.0001, 51.6 * deg, 0 * deg, 0 * deg},
	}
)

type constellationSatPos struct {
	ID     string  `json:"id"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	AltM   float64 `json:"alt_m"`
	Plane  string  `json:"plane"` // A/B/C/D for simulated; group name for TLE
	Source string  `json:"source"` // "sim" | "tle"
}

type constellationRes struct {
	Satellites []constellationSatPos `json:"satellites"`
	Time       string                `json:"time"`
	Source     string                `json:"source"` // "sim" | "tle"
	Group      string                `json:"group"`  // e.g. "starlink"
}

// islRelays defines the two ISL relay satellites that always appear on the globe
// regardless of TLE mode. SC-2 matches cmd/relay/main.go orbital elements;
// SC-3 matches cmd/relay2/main.go (550 km, 53° inclination, RAAN=180°).
var islRelays = []constellationSat{
	{"SC-2", 7_078_000, 0.0001, 98.0 * deg,  90 * deg, 60 * deg}, // 700 km polar, sun-sync
	{"SC-3", 6_928_000, 0.0001, 53.0 * deg, 180 * deg, 45 * deg}, // 550 km, 53° medium-inc
}

var satPlane = map[string]string{
	"AT-1": "A",
	"SC-2": "ISL", "SC-3": "ISL",
}

func planeID(id string) string {
	if p, ok := satPlane[id]; ok {
		return p
	}
	return ""
}

// islPositions propagates the two ISL relay satellites to now and returns their positions.
func islPositions(now time.Time) []constellationSatPos {
	out := make([]constellationSatPos, 0, len(islRelays))
	for _, s := range islRelays {
		elem := orbital.Elements{
			SemiMajorAxis: s.sma,
			Eccentricity:  s.ecc,
			Inclination:   s.inc,
			RAAN:          s.ran,
			ArgPerigee:    0,
			MeanAnomaly:   s.ma,
			Epoch:         constellationEpoch,
		}
		eci, err := orbital.Propagate(elem, now)
		if err != nil {
			continue
		}
		ecef := orbital.ECIToECEF(eci, now)
		geo := orbital.ECEFToGeodetic(ecef)
		out = append(out, constellationSatPos{
			ID:     s.id,
			Lat:    geo.LatitudeDeg,
			Lon:    geo.LongitudeDeg,
			AltM:   geo.AltitudeM,
			Plane:  "ISL",
			Source: "sim",
		})
	}
	return out
}

// constellationHandler propagates orbital elements to the current time and returns
// geodetic positions.  When TLE data has been imported, it uses those elements;
// otherwise it falls back to the hardcoded 16-satellite simulation.
func constellationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()

		// Check if TLE data is loaded
		tleRecs, tleGroup := tleEntries()

		if len(tleRecs) > 0 {
			// ── TLE mode ──────────────────────────────────────────────────
			positions := make([]constellationSatPos, 0, len(tleRecs))
			for _, rec := range tleRecs {
				eci, err := orbital.Propagate(rec.Elements, now)
				if err != nil {
					continue
				}
				ecef := orbital.ECIToECEF(eci, now)
				geo := orbital.ECEFToGeodetic(ecef)
				positions = append(positions, constellationSatPos{
					ID:     rec.Name,
					Lat:    geo.LatitudeDeg,
					Lon:    geo.LongitudeDeg,
					AltM:   geo.AltitudeM,
					Plane:  tleGroup, // group name used as plane label in frontend
					Source: "tle",
				})
			}
			positions = append(positions, islPositions(now)...)
			writeJSON(w, http.StatusOK, constellationRes{
				Satellites: positions,
				Time:       now.Format(time.RFC3339),
				Source:     "tle",
				Group:      tleGroup,
			})
			return
		}

		// ── Simulation mode (default 16-satellite constellation) ──────────
		positions := make([]constellationSatPos, 0, len(constellation16))
		for _, s := range constellation16 {
			elem := orbital.Elements{
				SemiMajorAxis: s.sma,
				Eccentricity:  s.ecc,
				Inclination:   s.inc,
				RAAN:          s.ran,
				ArgPerigee:    0,
				MeanAnomaly:   s.ma,
				Epoch:         constellationEpoch,
			}
			eci, err := orbital.Propagate(elem, now)
			if err != nil {
				continue
			}
			ecef := orbital.ECIToECEF(eci, now)
			geo := orbital.ECEFToGeodetic(ecef)
			positions = append(positions, constellationSatPos{
				ID:     s.id,
				Lat:    geo.LatitudeDeg,
				Lon:    geo.LongitudeDeg,
				AltM:   geo.AltitudeM,
				Plane:  planeID(s.id),
				Source: "sim",
			})
		}
		positions = append(positions, islPositions(now)...)
		writeJSON(w, http.StatusOK, constellationRes{
			Satellites: positions,
			Time:       now.Format(time.RFC3339),
			Source:     "sim",
			Group:      "",
		})
	}
}
