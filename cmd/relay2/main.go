// Command relay2 runs the Zenith-Link second ISL relay node (SC-3).
//
// SC-3 sits in a medium-inclination orbit at 550 km, offset 180° in RAAN from
// SC-1, filling the coverage gap between SC-1 (ISS-like, 400 km, 51.6°) and
// Relay-1 (SC-2, sun-sync polar, 700 km, 98°). Together the three nodes form
// a peer-to-peer ISL mesh — direct analog to Satlyt's IAC Paper 1 and NASA
// ACO Topic 3 "Autonomous Edge Computing for Small Spacecraft Networks."
//
// SC-3 prefers to fetch buffered frames from Relay-1 (SC-2) rather than
// polling SC-1 directly, reducing link load and demonstrating peer routing.
// It falls back to SC-1 if Relay-1 has no buffered data.
//
// Configuration is read from environment variables:
//
//	RELAY2_ADDR       HTTP listen address (default: :8083)
//	SC1_ADDR          Primary spacecraft base URL (required)
//	RELAY1_ADDR       Relay-1 (SC-2) base URL for peer health check (required)
//	GS_ADDR           Ground station base URL (required)
//	RELAY2_SCID       This relay's CCSDS Spacecraft ID (default: 92)
//	GS_LAT            Ground station latitude  [degrees] (default: -1.2864, Nairobi)
//	GS_LON            Ground station longitude [degrees] (default: 36.8172, Nairobi)
//	MIN_ELEV_DEG      Minimum elevation for contact [degrees] (default: 5.0)
//	POLL_INTERVAL_SEC SC-1 poll interval in seconds (default: 30)
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/absmach/zenith-link/pkg/link"
	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/caarlos0/env/v11"
)

type config struct {
	Addr            string  `env:"RELAY2_ADDR"        envDefault:":8083"`
	SC1Addr         string  `env:"SC1_ADDR,required"`
	Relay1Addr      string  `env:"RELAY1_ADDR,required"`
	GSAddr          string  `env:"GS_ADDR,required"`
	RelaySCID       uint16  `env:"RELAY2_SCID"        envDefault:"92"`
	GSLat           float64 `env:"GS_LAT"             envDefault:"-1.2864"`
	GSLon           float64 `env:"GS_LON"             envDefault:"36.8172"`
	MinElevDeg      float64 `env:"MIN_ELEV_DEG"       envDefault:"5.0"`
	PollIntervalSec int     `env:"POLL_INTERVAL_SEC"  envDefault:"30"`
}

type relayBuffer struct {
	mu          sync.RWMutex
	frame       []byte
	fetchedAt   time.Time
	upstreamNode string // "SC-2-relay" or "SC-1"
}

func (b *relayBuffer) store(frame []byte, node string) {
	b.mu.Lock()
	b.frame = frame
	b.fetchedAt = time.Now().UTC()
	b.upstreamNode = node
	b.mu.Unlock()
}

func (b *relayBuffer) load() ([]byte, time.Time, string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.frame, b.fetchedAt, b.upstreamNode
}

// relay1Health is a minimal subset of Relay-1's /health response.
type relay1Health struct {
	BufferHasData bool `json:"buffer_has_data"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		logger.Error("failed to parse config", slog.Any("error", err))
		os.Exit(1)
	}

	// Medium-inclination orbit at 550 km, 53°, RAAN=π (180° offset from SC-1).
	// Fills the coverage gap between ISS-like SC-1 (400 km, 51.6°) and polar
	// Relay-1 (700 km, 98°), giving the constellation three distinct ground tracks.
	// MeanAnomaly is adjusted at startup so the first Nairobi contact window
	// opens within ~90 seconds (offset from Relay-1 by targetLeadSec+30s to stagger).
	sc3Elements := adjustForEarlyContact(orbital.Elements{
		SemiMajorAxis: 6_921_000, // 550 km altitude
		Eccentricity:  0.0001,
		Inclination:   53.0 * math.Pi / 180,
		RAAN:          math.Pi, // 180° from SC-1, maximising coverage gap fill
		ArgPerigee:    0.0,
		MeanAnomaly:   0.0,
		Epoch:         time.Now().UTC(),
	}, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg, 120, logger) // 120s lead: staggered after Relay-1

	buf := &relayBuffer{}
	client := &http.Client{Timeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go pollLoop(ctx, client, cfg, buf, logger)
	go forwardLoop(ctx, client, cfg, sc3Elements, buf, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		frame, fetchedAt, upstream := buf.load()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":          "ok",
			"relay_scid":      cfg.RelaySCID,
			"sc1_addr":        cfg.SC1Addr,
			"relay1_addr":     cfg.Relay1Addr,
			"gs_addr":         cfg.GSAddr,
			"buffer_has_data": len(frame) > 0,
			"last_fetch_at":   fetchedAt,
			"upstream_node":   upstream,
		})
	})

	// /windows returns upcoming ground-station contact windows for SC-3 (Relay-2).
	mux.HandleFunc("/windows", relayWindowsHandler(sc3Elements, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg))

	// /telemetry returns a live simulated health snapshot of this relay node (SC-3).
	// Orbital state is propagated from sc3Elements; power and thermal values
	// reflect the 550 km medium-inclination orbit.
	mux.HandleFunc("/telemetry", relay2TelemetryHandler(sc3Elements, cfg.RelaySCID))

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("relay-2 (SC-3) starting",
			slog.String("addr", cfg.Addr),
			slog.Uint64("scid", uint64(cfg.RelaySCID)),
			slog.String("orbit", "550km/53deg/RAAN=180deg"),
			slog.String("gs_addr", cfg.GSAddr),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("relay-2 shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// pollLoop fetches frames on a timer, preferring Relay-1 over SC-1 direct.
// This implements the peer-to-peer routing decision: check whether SC-2 already
// has a buffered frame before going directly to SC-1. Reduces duplicate polling
// on the primary spacecraft and demonstrates mesh-layer routing.
func pollLoop(ctx context.Context, client *http.Client, cfg config, buf *relayBuffer, logger *slog.Logger) {
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	fetchBest(ctx, client, cfg, buf, logger)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetchBest(ctx, client, cfg, buf, logger)
		}
	}
}

// fetchBest tries Relay-1 first; falls back to SC-1 if Relay-1 has no data.
func fetchBest(ctx context.Context, client *http.Client, cfg config, buf *relayBuffer, logger *slog.Logger) {
	if relay1HasData(ctx, client, cfg.Relay1Addr) {
		if fetchFrom(ctx, client, cfg.Relay1Addr+"/frame/zenith", "SC-2-relay", buf, logger) {
			return
		}
	}
	fetchFrom(ctx, client, cfg.SC1Addr+"/frame/zenith", "SC-1", buf, logger)
}

func relay1HasData(ctx context.Context, client *http.Client, relay1Addr string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, relay1Addr+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var h relay1Health
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&h); err != nil {
		return false
	}
	return h.BufferHasData
}

func fetchFrom(ctx context.Context, client *http.Client, url, node string, buf *relayBuffer, logger *slog.Logger) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("relay-2 poll: upstream unreachable",
			slog.String("node", node), slog.String("url", url), slog.Any("error", err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}
	throttled := link.NewThrottle(io.LimitReader(resp.Body, 8192), 20480) // 20 KB/s ISL link budget
	frame, err := io.ReadAll(throttled)
	if err != nil || len(frame) == 0 {
		return false
	}
	buf.store(frame, node)
	logger.Info("relay-2 poll: frame buffered",
		slog.String("upstream", node), slog.Int("bytes", len(frame)))
	return true
}

func forwardLoop(ctx context.Context, client *http.Client, cfg config, elem orbital.Elements, buf *relayBuffer, logger *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastForwarded time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			frame, fetchedAt, upstream := buf.load()
			if len(frame) == 0 {
				continue
			}
			inContact, err := orbital.IsInContact(elem, cfg.GSLat, cfg.GSLon, time.Now().UTC(), cfg.MinElevDeg)
			if err != nil {
				logger.Warn("relay-2: contact check failed", slog.Any("error", err))
				continue
			}
			if !inContact {
				continue
			}
			if time.Since(lastForwarded) < 5*time.Second {
				continue
			}
			if err := forwardFrame(ctx, client, cfg, frame); err != nil {
				logger.Warn("relay-2: forward failed", slog.Any("error", err))
			} else {
				lastForwarded = time.Now()
				logger.Info("relay-2: frame forwarded to GS via ISL",
					slog.String("upstream", upstream),
					slog.Int("bytes", len(frame)),
					slog.Duration("frame_age", time.Since(fetchedAt)),
				)
			}
		}
	}
}

// relay2TelemetryHandler returns a handler that emits a live telemetry snapshot
// for SC-3, derived from orbital phase at request time. Lets Globe3D and the
// satellite selector show realistic SC-3 health data without a dedicated radio.
func relay2TelemetryHandler(elem orbital.Elements, scid uint16) http.HandlerFunc {
	const orbitalPeriodSec = 5700.0 // 550 km → ~95-min period

	return func(w http.ResponseWriter, _ *http.Request) {
		elapsed := time.Since(elem.Epoch).Seconds()
		phase := math.Mod(elapsed/orbitalPeriodSec, 1.0)

		// ~35% of orbit in eclipse for 550 km altitude
		inEclipse := phase > 0.65

		var batV, solarV, tempC float64
		if inEclipse {
			solarV = 0
			// slight discharge and cooling during eclipse shadow
			shadowFrac := (phase - 0.65) / 0.35
			batV = 7500 - 300*shadowFrac
			tempC = 18 - 12*shadowFrac
		} else {
			solarV = 5100 + 200*math.Sin(2*math.Pi*phase)
			batV = 7500
			tempC = 22 + 8*math.Sin(2*math.Pi*phase)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"scid":          scid,
			"name":          "SC-3 (Relay-2)",
			"orbit":         "550km/53deg/RAAN=180deg",
			"bat_mv":        batV,
			"solar_mv":      solarV,
			"temp_c":        tempC,
			"rssi_dbm":      -72.0,
			"in_eclipse":    inEclipse,
			"orbital_phase": phase,
			"ts":            time.Now().UTC(),
		})
	}
}

// relayWindowsHandler returns upcoming Nairobi contact windows for this relay.
func relayWindowsHandler(elem orbital.Elements, gsLat, gsLon, minElevDeg float64) http.HandlerFunc {
	type winRes struct {
		AOS         time.Time `json:"aos"`
		LOS         time.Time `json:"los"`
		DurationSec float64   `json:"duration_sec"`
		MaxElevDeg  float64   `json:"max_elevation_deg"`
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		now := time.Now().UTC()
		inContact, _ := orbital.IsInContact(elem, gsLat, gsLon, now, minElevDeg)
		windows, err := orbital.ContactWindows(elem, gsLat, gsLon, now, now.Add(2*time.Hour), minElevDeg)
		var wins []winRes
		if err == nil {
			for _, cw := range windows {
				wins = append(wins, winRes{
					AOS:         cw.AOS,
					LOS:         cw.LOS,
					DurationSec: cw.Duration().Seconds(),
					MaxElevDeg:  cw.MaxElevationDeg,
				})
			}
		}
		if wins == nil {
			wins = []winRes{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count":      len(wins),
			"windows":    wins,
			"in_contact": inContact,
		})
	}
}

// adjustForEarlyContact advances the mean anomaly so the first Nairobi contact
// window opens within targetLeadSec seconds of startup. Orbital shape is unchanged.
func adjustForEarlyContact(elem orbital.Elements, gsLat, gsLon, minElevDeg, targetLeadSec float64, logger *slog.Logger) orbital.Elements {
	now := time.Now().UTC()
	windows, err := orbital.ContactWindows(elem, gsLat, gsLon, now, now.Add(120*time.Minute), minElevDeg)
	if err != nil || len(windows) == 0 {
		logger.Warn("relay-2: startup contact scan failed — using default mean anomaly", slog.Any("error", err))
		return elem
	}

	timeToAOS := windows[0].AOS.Sub(now).Seconds()
	if timeToAOS <= targetLeadSec {
		logger.Info("relay-2: first contact window within target lead — no adjustment needed",
			slog.Float64("time_to_aos_sec", timeToAOS))
		return elem
	}

	const mu = 3.986004418e14
	a := elem.SemiMajorAxis
	T := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	advance := timeToAOS - targetLeadSec
	elem.MeanAnomaly = math.Mod(elem.MeanAnomaly+2*math.Pi*advance/T, 2*math.Pi)

	logger.Info("relay-2: mean anomaly adjusted for early startup contact window",
		slog.Float64("original_aos_sec", timeToAOS),
		slog.Float64("target_lead_sec", targetLeadSec),
		slog.Float64("advance_sec", advance))

	return elem
}

func forwardFrame(ctx context.Context, client *http.Client, cfg config, frame []byte) error {
	url := cfg.GSAddr + "/receive"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(frame))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Relayed-By", "SC-3-relay2")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GS returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
