package api

import (
	"math"
	"net/http"
	"time"

	"github.com/absmach/orbitron/pkg/orbital"
)

const deg = math.Pi / 180

type constellationSat struct {
	id  string
	sma float64
	ecc float64
	inc float64
	ran float64
	ma  float64
}

var (
	constellationEpoch = time.Now().UTC()

	constellation16 = []constellationSat{
		{"AT-1", 6_878_000, 0.0001, 97.4 * deg, 0 * deg, 0 * deg},
	}
)

type constellationSatPos struct {
	ID     string  `json:"id"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	AltM   float64 `json:"alt_m"`
	Plane  string  `json:"plane"`
	Source string  `json:"source"`
}

type constellationRes struct {
	Satellites []constellationSatPos `json:"satellites"`
	Time       string                `json:"time"`
	Source     string                `json:"source"`
	Group      string                `json:"group"`
}

var islRelays = []constellationSat{
	{"SC-2", 7_078_000, 0.0001, 98.0 * deg, 90 * deg, 60 * deg},
	{"SC-3", 6_928_000, 0.0001, 53.0 * deg, 180 * deg, 45 * deg},
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

func constellationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()

		tleRecs, tleGroup := tleEntries()

		if len(tleRecs) > 0 {

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
					Plane:  tleGroup,
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
