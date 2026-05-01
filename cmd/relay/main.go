// Command relay runs the Zenith-Link ISL (Inter-Satellite Link) relay service.
//
// The relay satellite polls a primary spacecraft (SC-1) for its latest Zenith-Link
// telemetry frame, wraps it in a DTN bundle, buffers it in the DTN store, and
// forwards it to a ground station when the relay is within a contact window of
// the ground station. Forwarded frames carry the X-Relayed-By header so the
// ground station can log the relay source.
//
// This demonstrates the store-and-forward relay concept at the heart of
// inter-satellite networking: SC-1 may not be visible to the ground; SC-2
// (this relay, in a complementary orbit) bridges the gap.
//
// Configuration is read from environment variables:
//
//	RELAY_ADDR        HTTP listen address for health endpoint (default: :8082)
//	SC1_ADDR          Primary spacecraft base URL (required)
//	GS_ADDR           Ground station base URL (required)
//	RELAY_SCID        This relay's CCSDS Spacecraft ID (default: 91)
//	GS_LAT            Ground station geodetic latitude  [degrees] (default: -1.2864, Nairobi)
//	GS_LON            Ground station geodetic longitude [degrees] (default: 36.8172, Nairobi)
//	MIN_ELEV_DEG      Minimum elevation angle for contact [degrees] (default: 5.0)
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
	"syscall"
	"time"

	"github.com/absmach/zenith-link/pkg/dtn"
	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/caarlos0/env/v11"
)

type config struct {
	Addr            string  `env:"RELAY_ADDR"         envDefault:":8082"`
	SC1Addr         string  `env:"SC1_ADDR,required"`
	GSAddr          string  `env:"GS_ADDR,required"`
	RelaySCID       uint16  `env:"RELAY_SCID"         envDefault:"91"`
	GSLat           float64 `env:"GS_LAT"             envDefault:"-1.2864"`
	GSLon           float64 `env:"GS_LON"             envDefault:"36.8172"`
	MinElevDeg      float64 `env:"MIN_ELEV_DEG"       envDefault:"5.0"`
	PollIntervalSec int     `env:"POLL_INTERVAL_SEC"  envDefault:"30"`
}

// relayNode holds the DTN bundle store and orbital parameters for this relay.
type relayNode struct {
	store    *dtn.Store
	elements orbital.Elements
	scid     uint16
}

// extractPriority reads the Zenith-Link v2 Flags byte (offset 3 per the wire
// format) and returns 2 if FlagPriority (0x04) is set, else 1 (normal).
func extractPriority(frame []byte) uint8 {
	if len(frame) < zenith.HeaderSize {
		return 1
	}
	// Wire layout: Magic[0:2] | Version[2] | Flags[3] | Sequence[4:6] | Timestamp[6:10]
	if frame[3]&zenith.FlagPriority != 0 {
		return 2 // expedited
	}
	return 1 // normal
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

	// Sun-synchronous polar orbit at 700 km — complements the ISS-like SC-1.
	// Inclination 98° gives retrograde sun-sync; RAAN chosen for coverage offset.
	// MeanAnomaly is adjusted at startup so the first Nairobi contact window
	// opens within ~90 seconds, making the ISL demo reliable without warping physics.
	relayElements := adjustForEarlyContact(orbital.Elements{
		SemiMajorAxis: 7_078_000,   // 700 km altitude
		Eccentricity:  0.0001,
		Inclination:   98.0 * math.Pi / 180,
		RAAN:          math.Pi / 2, // 90° offset from SC-1 for complementary coverage
		ArgPerigee:    0.0,
		MeanAnomaly:   0.0,
		Epoch:         time.Now().UTC(),
	}, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg, 90, logger)

	node := &relayNode{
		store:    dtn.NewStore(),
		elements: relayElements,
		scid:     cfg.RelaySCID,
	}

	client := &http.Client{Timeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Poll SC-1 for its latest Zenith-Link frame and wrap it in a DTN bundle.
	go pollSC1(ctx, client, cfg, node, logger)

	// Forward bundles to GS when in contact.
	go forwardLoop(ctx, client, cfg, node, logger)

	// Periodically prune expired bundles from the store.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n := node.store.PruneExpired()
				if n > 0 {
					logger.Info("relay: pruned expired bundles", slog.Int("count", n))
				}
			}
		}
	}()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		bundleCount := node.store.Len()
		hasData := node.store.HasData()
		oldestAgeSec := int64(node.store.OldestAge().Seconds())

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":                "ok",
			"relay_scid":            cfg.RelaySCID,
			"sc1_addr":              cfg.SC1Addr,
			"gs_addr":               cfg.GSAddr,
			"buffer_has_data":       hasData,
			"bundle_count":          bundleCount,
			"oldest_bundle_age_sec": oldestAgeSec,
			"bundle_ttl_sec":        7200,
			"hop_count_max":         8,
		})
	})

	// Expose the payload of the next queued bundle for peer-to-peer relay routing.
	// Relay-2 (SC-3) calls this to fetch a frame from SC-2's DTN store before
	// polling SC-1 directly, reducing load on the primary spacecraft.
	mux.HandleFunc("/frame/zenith", func(w http.ResponseWriter, _ *http.Request) {
		b := node.store.Next()
		if b == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b.Payload)
	})

	// /windows returns upcoming ground-station contact windows for SC-2 (Relay-1).
	// Used by the frontend to show AOS countdown in the ISL mesh tile.
	mux.HandleFunc("/windows", relayWindowsHandler(relayElements, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg))

	// /telemetry returns a live simulated health snapshot for SC-2 (Relay-1).
	// Orbital state is derived from relayElements; values reflect the 700 km
	// sun-synchronous polar orbit.
	mux.HandleFunc("/telemetry", relayTelemetryHandler(relayElements, cfg.RelaySCID))

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("relay service starting",
			slog.String("addr", cfg.Addr),
			slog.Uint64("scid", uint64(cfg.RelaySCID)),
			slog.String("sc1_addr", cfg.SC1Addr),
			slog.String("gs_addr", cfg.GSAddr),
			slog.Float64("gs_lat", cfg.GSLat),
			slog.Float64("gs_lon", cfg.GSLon),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("relay shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// pollSC1 fetches SC-1's raw Zenith-Link frame every PollIntervalSec seconds,
// wraps it in a DTN bundle, and places it in the node's DTN store.
func pollSC1(ctx context.Context, client *http.Client, cfg config, node *relayNode, logger *slog.Logger) {
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	// Fetch immediately on startup.
	fetchAndStore(ctx, client, cfg, node, logger)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetchAndStore(ctx, client, cfg, node, logger)
		}
	}
}

func fetchAndStore(ctx context.Context, client *http.Client, cfg config, node *relayNode, logger *slog.Logger) {
	url := cfg.SC1Addr + "/frame/zenith"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Warn("relay poll: build request failed", slog.Any("error", err))
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("relay poll: SC-1 unreachable", slog.String("url", url), slog.Any("error", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("relay poll: SC-1 returned non-200", slog.Int("status", resp.StatusCode))
		return
	}

	frame, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		logger.Warn("relay poll: failed to read SC-1 response body", slog.Any("error", err))
		return
	}
	if len(frame) == 0 {
		logger.Warn("relay poll: SC-1 returned empty frame body")
		return
	}

	b := &dtn.Bundle{
		ID:          uint64(time.Now().UnixNano()),
		Source:      dtn.EID{Node: 1, Service: 1}, // SC-1, telemetry
		Destination: dtn.EID{Node: 0, Service: 1}, // Ground station, telemetry
		CreatedAt:   time.Now().UTC(),
		Lifetime:    2 * time.Hour,
		Payload:     frame,
		Priority:    extractPriority(frame),
	}
	node.store.Put(b)
	logger.Info("relay poll: SC-1 frame wrapped in DTN bundle and stored",
		slog.Int("bytes", len(frame)),
		slog.Uint64("bundle_id", b.ID),
		slog.Uint64("priority", uint64(b.Priority)),
		slog.Int("bundle_count", node.store.Len()),
	)
}

// forwardLoop checks contact windows every 10 seconds and forwards the highest-
// priority bundle in the DTN store to the ground station when the relay is in view.
func forwardLoop(ctx context.Context, client *http.Client, cfg config, node *relayNode, logger *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastForwarded time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b := node.store.Next()
			if b == nil {
				continue
			}

			inContact, err := orbital.IsInContact(node.elements, cfg.GSLat, cfg.GSLon, time.Now().UTC(), cfg.MinElevDeg)
			if err != nil {
				logger.Warn("relay: contact check failed", slog.Any("error", err))
				continue
			}

			if !inContact {
				logger.Debug("relay: not in contact — bundle buffered",
					slog.Uint64("bundle_id", b.ID),
					slog.Int("bundle_count", node.store.Len()),
				)
				continue
			}

			// Drain all buffered bundles during contact window (rate-limit: 5s between
			// successive forwards so the GS isn't flooded but link is kept busy).
			for {
				if time.Since(lastForwarded) < 5*time.Second {
					break
				}
				bfwd := node.store.Next()
				if bfwd == nil {
					break
				}
				if err := forwardFrame(ctx, client, cfg, bfwd.Payload); err != nil {
					logger.Warn("relay: forward failed",
						slog.Uint64("bundle_id", bfwd.ID),
						slog.Any("error", err),
					)
					break
				}
				node.store.Remove(bfwd.ID)
				lastForwarded = time.Now()
				logger.Info("relay: bundle forwarded to GS via ISL",
					slog.Uint64("bundle_id", bfwd.ID),
					slog.Int("bytes", len(bfwd.Payload)),
					slog.Duration("bundle_age", time.Since(bfwd.CreatedAt)),
					slog.Float64("gs_lat", cfg.GSLat),
					slog.Float64("gs_lon", cfg.GSLon),
				)
			}
		}
	}
}

// relayTelemetryHandler returns a handler that emits a live telemetry snapshot
// for SC-2 (Relay-1). Values are derived from orbital phase at request time.
func relayTelemetryHandler(elem orbital.Elements, scid uint16) http.HandlerFunc {
	const orbitalPeriodSec = 5913.0 // 700 km sun-sync → ~98.5-min period

	return func(w http.ResponseWriter, _ *http.Request) {
		elapsed := time.Since(elem.Epoch).Seconds()
		phase := math.Mod(elapsed/orbitalPeriodSec, 1.0)

		// Sun-sync at 700 km: ~38% of orbit in eclipse
		inEclipse := phase > 0.62

		var batV, solarV, tempC float64
		if inEclipse {
			solarV = 0
			shadowFrac := (phase - 0.62) / 0.38
			batV = 7450 - 280*shadowFrac
			tempC = 16 - 14*shadowFrac
		} else {
			solarV = 5200 + 150*math.Sin(2*math.Pi*phase)
			batV = 7480
			tempC = 20 + 6*math.Sin(2*math.Pi*phase)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"scid":          scid,
			"name":          "SC-2 (Relay-1)",
			"orbit":         "700km/98deg/sun-sync",
			"bat_mv":        batV,
			"solar_mv":      solarV,
			"temp_c":        tempC,
			"rssi_dbm":      -74.0,
			"in_eclipse":    inEclipse,
			"orbital_phase": phase,
			"ts":            time.Now().UTC(),
		})
	}
}

// relayWindowsHandler returns the next contact windows for this relay as seen
// from the ground station. The frontend uses this to show the AOS countdown
// in the ISL mesh tile, replacing a generic "no data" state with an orbital ETA.
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

// adjustForEarlyContact ensures the first Nairobi contact window opens within
// targetLeadSec seconds of startup by advancing the mean anomaly. The orbital
// shape and period are unchanged — the satellite simply starts at a different
// point in its orbit. This makes ISL relay forwarding reliable for demos
// without waiting up to 100 minutes for a contact window to naturally open.
func adjustForEarlyContact(elem orbital.Elements, gsLat, gsLon, minElevDeg, targetLeadSec float64, logger *slog.Logger) orbital.Elements {
	now := time.Now().UTC()
	windows, err := orbital.ContactWindows(elem, gsLat, gsLon, now, now.Add(120*time.Minute), minElevDeg)
	if err != nil || len(windows) == 0 {
		logger.Warn("relay: startup contact scan failed — using default mean anomaly", slog.Any("error", err))
		return elem
	}

	firstAOS := windows[0].AOS
	timeToAOS := firstAOS.Sub(now).Seconds()

	if timeToAOS <= targetLeadSec {
		logger.Info("relay: first contact window within target lead — no adjustment needed",
			slog.Float64("time_to_aos_sec", timeToAOS))
		return elem
	}

	// T = 2π√(a³/μ); advance by (timeToAOS - targetLeadSec) seconds of orbit.
	const mu = 3.986004418e14 // Earth gravitational parameter [m³/s²]
	a := elem.SemiMajorAxis
	T := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	advance := timeToAOS - targetLeadSec
	elem.MeanAnomaly = math.Mod(elem.MeanAnomaly+2*math.Pi*advance/T, 2*math.Pi)

	logger.Info("relay: mean anomaly adjusted for early startup contact window",
		slog.Float64("original_aos_sec", timeToAOS),
		slog.Float64("target_lead_sec", targetLeadSec),
		slog.Float64("advance_sec", advance))

	return elem
}

func forwardFrame(ctx context.Context, client *http.Client, cfg config, frame []byte) error {
	url := cfg.GSAddr + "/receive"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(frame))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	// Identify the relay source so the ground station can log it.
	req.Header.Set("X-Relayed-By", "SC-2-relay")

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
