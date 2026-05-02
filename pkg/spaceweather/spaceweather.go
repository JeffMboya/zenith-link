// Package spaceweather polls NOAA's public space weather APIs and provides
// real-time geomagnetic and solar flux data for telemetry simulation.
//
// The planetary K-index (Kp) measures geomagnetic disturbances caused by solar
// wind — elevated Kp increases ionospheric scintillation and degrades RF link
// margins, mirroring Satlyt STL-01's RF degradation and downlink prioritisation.
// F10.7 solar flux tracks solar UV/EUV output; high flux increases ionospheric
// electron density and can affect link quality at UHF/VHF frequencies.
//
// No external dependencies — stdlib only. Runs fine on constrained edge hardware.
package spaceweather

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	kpURL        = "https://services.swpc.noaa.gov/json/planetary_k_index_1m.json"
	solarFluxURL = "https://services.swpc.noaa.gov/products/summary/10cm-flux.json"
	pollInterval = 15 * time.Minute
	httpTimeout  = 10 * time.Second
)

type Conditions struct {
	Kp        float64
	SolarFlux float64
	FetchedAt time.Time
	Available bool
}

type Monitor struct {
	mu         sync.RWMutex
	conditions Conditions
	client     *http.Client
}

func NewMonitor() *Monitor {
	return &Monitor{
		client: &http.Client{Timeout: httpTimeout},
	}
}

func (m *Monitor) Start(ctx context.Context) {

	go func() {
		m.fetch()
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.fetch()
			}
		}
	}()
}

func (m *Monitor) Current() Conditions {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conditions
}

func (m *Monitor) RSSIAdjustmentDB() float64 {
	c := m.Current()
	if !c.Available {
		return 0
	}

	var adj float64
	switch {
	case c.Kp >= 7:
		adj = -10
	case c.Kp >= 6:
		adj = -6
	case c.Kp >= 5:
		adj = -3
	case c.Kp >= 4:
		adj = -1
	default:
		adj = 0
	}

	if c.SolarFlux > 180 {
		adj += 1
	}
	return adj
}

func (m *Monitor) StormLevel() string {
	c := m.Current()
	if !c.Available {
		return "QUIET"
	}
	switch {
	case c.Kp >= 7:
		return "SEVERE"
	case c.Kp >= 6:
		return "STRONG"
	case c.Kp >= 5:
		return "MODERATE"
	case c.Kp >= 4:
		return "MINOR"
	default:
		return "QUIET"
	}
}

func (m *Monitor) fetch() {
	kp, err := m.fetchKp()
	if err != nil {
		slog.Warn("spaceweather: Kp fetch failed", slog.Any("error", err))
		return
	}

	flux, err := m.fetchSolarFlux()
	if err != nil {
		slog.Warn("spaceweather: solar flux fetch failed", slog.Any("error", err))
		return
	}

	m.mu.Lock()
	m.conditions = Conditions{
		Kp:        kp,
		SolarFlux: flux,
		FetchedAt: time.Now().UTC(),
		Available: true,
	}
	m.mu.Unlock()

	slog.Info("spaceweather: updated",
		slog.Float64("kp", kp),
		slog.Float64("solar_flux_sfu", flux),
		slog.String("storm_level", m.StormLevel()),
	)
}

type kpEntry struct {
	TimeTag     string  `json:"time_tag"`
	Kp          float64 `json:"kp"`
	EstimatedKp float64 `json:"estimated_kp"`
	KpIndex     int     `json:"kp_index"`
}

func (m *Monitor) fetchKp() (float64, error) {
	resp, err := m.client.Get(kpURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, &httpError{url: kpURL, code: resp.StatusCode}
	}

	var entries []kpEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, &httpError{url: kpURL, code: 0, msg: "empty response"}
	}
	return entries[len(entries)-1].Kp, nil
}

type solarFluxResponse struct {
	Flux      string `json:"Flux"`
	Frequency string `json:"Frequency"`
	Station   string `json:"Station"`
	TimeStamp string `json:"TimeStamp"`
}

func (m *Monitor) fetchSolarFlux() (float64, error) {
	resp, err := m.client.Get(solarFluxURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, &httpError{url: solarFluxURL, code: resp.StatusCode}
	}

	var sfr solarFluxResponse
	if err := json.NewDecoder(resp.Body).Decode(&sfr); err != nil {
		return 0, err
	}
	flux, err := strconv.ParseFloat(sfr.Flux, 64)
	if err != nil {
		return 0, err
	}
	return flux, nil
}

type httpError struct {
	url  string
	code int
	msg  string
}

func (e *httpError) Error() string {
	if e.msg != "" {
		return "spaceweather: " + e.url + ": " + e.msg
	}
	return "spaceweather: " + e.url + ": HTTP " + strconv.Itoa(e.code)
}
