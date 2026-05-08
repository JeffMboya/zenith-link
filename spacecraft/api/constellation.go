package api

import (
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/absmach/orbitron/pkg/orbital"
	"github.com/absmach/orbitron/pkg/tle"
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
		{"Satellite-1", 6_878_000, 0.0001, 97.4 * deg, 0 * deg, 0 * deg},
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

// relayNoradIDs maps each ISL relay satellite's common name to its NORAD catalog number.
var relayNoradIDs = []struct{ name, norad string }{
	{"NOAA-20", "43013"},
	{"Sentinel-2A", "40697"},
	{"Landsat-9", "49260"},
	{"Aqua", "27424"},
	{"Terra", "25994"},
	{"NOAA-19", "33591"},
}

type islRelayEntry struct {
	name string
	elem orbital.Elements
}

var islRelayCache struct {
	sync.RWMutex
	entries []islRelayEntry
}

func init() {
	go refreshISLRelays()
}

func refreshISLRelays() {
	for {
		var entries []islRelayEntry
		for _, r := range relayNoradIDs {
			elem, name, err := tle.FetchByNoradID(r.norad)
			if err != nil {
				slog.Warn("constellation: ISL relay TLE fetch failed", slog.String("name", r.name), slog.Any("error", err))
				continue
			}
			entries = append(entries, islRelayEntry{name: name, elem: elem})
		}
		if len(entries) > 0 {
			islRelayCache.Lock()
			islRelayCache.entries = entries
			islRelayCache.Unlock()
		}
		time.Sleep(10 * time.Minute)
	}
}

func islPositions(now time.Time) []constellationSatPos {
	islRelayCache.RLock()
	entries := islRelayCache.entries
	islRelayCache.RUnlock()

	out := make([]constellationSatPos, 0, len(entries))
	for _, e := range entries {
		eci, err := orbital.Propagate(e.elem, now)
		if err != nil {
			continue
		}
		ecef := orbital.ECIToECEF(eci, now)
		geo := orbital.ECEFToGeodetic(ecef)
		out = append(out, constellationSatPos{
			ID:     e.name,
			Lat:    geo.LatitudeDeg,
			Lon:    geo.LongitudeDeg,
			AltM:   geo.AltitudeM,
			Plane:  "ISL",
			Source: "tle",
		})
	}
	return out
}

var satPlane = map[string]string{
	"Satellite-1": "A",
}

func planeID(id string) string {
	if p, ok := satPlane[id]; ok {
		return p
	}
	return ""
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
