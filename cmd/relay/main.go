// Command relay runs the Zenith-Link ISL (Inter-Satellite Link) relay service.
//
// The relay satellite polls a primary spacecraft (SC-1) for its latest Zenith-Link
// telemetry frame, buffers it, and forwards it to a ground station when the relay
// is within a contact window of the ground station. Forwarded frames carry the
// X-Relayed-By header so the ground station can log the relay source.
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
	"sync"
	"syscall"
	"time"

	"github.com/absmach/zenith-link/pkg/orbital"
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

// relayBuffer holds the most recently fetched SC-1 Zenith-Link frame.
type relayBuffer struct {
	mu        sync.RWMutex
	frame     []byte
	fetchedAt time.Time
}

func (b *relayBuffer) store(frame []byte) {
	b.mu.Lock()
	b.frame = frame
	b.fetchedAt = time.Now().UTC()
	b.mu.Unlock()
}

func (b *relayBuffer) load() ([]byte, time.Time) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.frame, b.fetchedAt
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
	relayElements := orbital.Elements{
		SemiMajorAxis: 7_078_000,         // 700 km altitude
		Eccentricity:  0.0001,
		Inclination:   98.0 * math.Pi / 180,
		RAAN:          math.Pi / 2,        // 90° offset from SC-1 for complementary coverage
		ArgPerigee:    0.0,
		MeanAnomaly:   math.Pi / 3,        // start at 60° true anomaly
		Epoch:         time.Now().UTC(),
	}

	buf := &relayBuffer{}
	client := &http.Client{Timeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Poll SC-1 for its latest Zenith-Link frame.
	go pollSC1(ctx, client, cfg, buf, logger)

	// Forward buffered frames to GS when in contact.
	go forwardLoop(ctx, client, cfg, relayElements, buf, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		frame, fetchedAt := buf.load()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":          "ok",
			"relay_scid":      cfg.RelaySCID,
			"sc1_addr":        cfg.SC1Addr,
			"gs_addr":         cfg.GSAddr,
			"buffer_has_data": len(frame) > 0,
			"last_fetch_at":   fetchedAt,
		})
	})

	// Expose buffered SC-1 frame for peer-to-peer relay routing.
	// Relay-2 (SC-3) calls this to fetch a frame from SC-2's buffer before
	// polling SC-1 directly, reducing load on the primary spacecraft.
	mux.HandleFunc("/frame/zenith", func(w http.ResponseWriter, _ *http.Request) {
		frame, _ := buf.load()
		if len(frame) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(frame)
	})

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

// pollSC1 fetches SC-1's raw Zenith-Link frame every PollIntervalSec seconds
// and stores it in the relay buffer.
func pollSC1(ctx context.Context, client *http.Client, cfg config, buf *relayBuffer, logger *slog.Logger) {
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	// Fetch immediately on startup.
	fetchAndStore(ctx, client, cfg, buf, logger)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetchAndStore(ctx, client, cfg, buf, logger)
		}
	}
}

func fetchAndStore(ctx context.Context, client *http.Client, cfg config, buf *relayBuffer, logger *slog.Logger) {
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

	buf.store(frame)
	logger.Info("relay poll: SC-1 frame buffered", slog.Int("bytes", len(frame)))
}

// forwardLoop checks contact windows every 10 seconds and forwards the buffered
// SC-1 frame to the ground station when the relay is in view.
func forwardLoop(ctx context.Context, client *http.Client, cfg config, elem orbital.Elements, buf *relayBuffer, logger *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastForwarded time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			frame, fetchedAt := buf.load()
			if len(frame) == 0 {
				continue
			}

			inContact, err := orbital.IsInContact(elem, cfg.GSLat, cfg.GSLon, time.Now().UTC(), cfg.MinElevDeg)
			if err != nil {
				logger.Warn("relay: contact check failed", slog.Any("error", err))
				continue
			}

			if !inContact {
				logger.Debug("relay: not in contact — frame buffered",
					slog.Time("buffered_at", fetchedAt))
				continue
			}

			// Avoid flooding — forward at most once per minute even if contact persists.
			if time.Since(lastForwarded) < time.Minute {
				continue
			}

			if err := forwardFrame(ctx, client, cfg, frame); err != nil {
				logger.Warn("relay: forward failed", slog.Any("error", err))
			} else {
				lastForwarded = time.Now()
				logger.Info("relay: SC-1 frame forwarded to GS via ISL",
					slog.Int("bytes", len(frame)),
					slog.Duration("frame_age", time.Since(fetchedAt)),
					slog.Float64("gs_lat", cfg.GSLat),
					slog.Float64("gs_lon", cfg.GSLon),
				)
			}
		}
	}
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

