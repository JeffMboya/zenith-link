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

// constellation16 defines the 16-satellite LEO constellation.
// Epoch is set once at package initialisation (server startup) so all satellites
// propagate consistently across repeated requests.
var (
	constellationEpoch = time.Now().UTC()

	constellation16 = []constellationSat{
		// ── Plane A: 51.6° inclination (ISS-like), RAAN = 0° ─────────────────────
		{"AT-1",  6_788_000, 0.0001, 51.6 * deg,   0 * deg,   0 * deg},
		{"AT-2",  6_788_000, 0.0001, 51.6 * deg,   0 * deg,  90 * deg},
		{"AT-3",  6_788_000, 0.0001, 51.6 * deg,   0 * deg, 180 * deg},
		{"AT-4",  6_788_000, 0.0001, 51.6 * deg,   0 * deg, 270 * deg},
		// ── Plane B: 97.5° inclination (sun-synchronous), RAAN = 45° ─────────────
		{"AT-5",  6_878_000, 0.0001, 97.5 * deg,  45 * deg,   0 * deg},
		{"AT-6",  6_878_000, 0.0001, 97.5 * deg,  45 * deg,  90 * deg},
		{"AT-7",  6_878_000, 0.0001, 97.5 * deg,  45 * deg, 180 * deg},
		{"AT-8",  6_878_000, 0.0001, 97.5 * deg,  45 * deg, 270 * deg},
		// ── Plane C: 28.5° inclination (low-inclination), RAAN = 90° ─────────────
		{"AT-9",  6_828_000, 0.0001, 28.5 * deg,  90 * deg,   0 * deg},
		{"AT-10", 6_828_000, 0.0001, 28.5 * deg,  90 * deg,  90 * deg},
		{"AT-11", 6_828_000, 0.0001, 28.5 * deg,  90 * deg, 180 * deg},
		{"AT-12", 6_828_000, 0.0001, 28.5 * deg,  90 * deg, 270 * deg},
		// ── Plane D: 70.0° inclination (high-inclination), RAAN = 135° ───────────
		{"AT-13", 6_858_000, 0.0001, 70.0 * deg, 135 * deg,   0 * deg},
		{"AT-14", 6_858_000, 0.0001, 70.0 * deg, 135 * deg,  90 * deg},
		{"AT-15", 6_858_000, 0.0001, 70.0 * deg, 135 * deg, 180 * deg},
		{"AT-16", 6_858_000, 0.0001, 70.0 * deg, 135 * deg, 270 * deg},
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
	"AT-1": "A", "AT-2": "A", "AT-3": "A", "AT-4": "A",
	"AT-5": "B", "AT-6": "B", "AT-7": "B", "AT-8": "B",
	"AT-9": "C", "AT-10": "C", "AT-11": "C", "AT-12": "C",
	"AT-13": "D", "AT-14": "D", "AT-15": "D", "AT-16": "D",
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
