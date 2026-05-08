// Command relay2 runs the Orbitron second ISL relay node (Satellite-3).
//
// Satellite-3 sits in a medium-inclination orbit at 550 km, offset 180° in RAAN from
// Satellite-1, filling the coverage gap between Satellite-1 (ISS-like, 400 km, 51.6°) and
// Relay-1 (Satellite-2, sun-sync polar, 700 km, 98°). Together the three nodes form
// a peer-to-peer ISL mesh — direct analog to Satlyt's IAC Paper 1 and NASA
// ACO Topic 3 "Autonomous Edge Computing for Small Spacecraft Networks."
//
// Satellite-3 prefers to fetch buffered frames from Relay-1 (Satellite-2) rather than
// polling Satellite-1 directly, reducing link load and demonstrating peer routing.
// It falls back to Satellite-1 if Relay-1 has no buffered data.
//
// Configuration is read from environment variables:
//
//	RELAY2_ADDR       HTTP listen address (default: :8083)
//	SC1_ADDR          Primary spacecraft base URL (required)
//	RELAY1_ADDR       Relay-1 (Satellite-2) base URL for peer health check (required)
//	GS_ADDR           Nairobi ground station base URL (required)
//	GS_LAT            Nairobi latitude  [degrees] (default: -1.2864)
//	GS_LON            Nairobi longitude [degrees] (default: 36.8172)
//	GS2_ADDR          Svalbard ground station base URL (optional)
//	GS2_LAT           Svalbard latitude  [degrees] (default: 78.2232)
//	GS2_LON           Svalbard longitude [degrees] (default: 15.6267)
//	GS3_ADDR          Punta Arenas ground station base URL (optional)
//	GS3_LAT           Punta Arenas latitude  [degrees] (default: -53.1638)
//	GS3_LON           Punta Arenas longitude [degrees] (default: -70.9171)
//	RELAY2_SCID       This relay's CCSDS Spacecraft ID (default: 92)
//	MIN_ELEV_DEG      Minimum elevation for contact [degrees] (default: 5.0)
//	POLL_INTERVAL_SEC Satellite-1 poll interval in seconds (default: 30)
//	LINK_LOSS_RATE    Frame drop probability [0.0–1.0] to simulate BER (default: 0)
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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/absmach/orbitron/pkg/link"
	"github.com/absmach/orbitron/pkg/orbital"
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
	GS2Addr         string  `env:"GS2_ADDR"           envDefault:""`
	GS2Lat          float64 `env:"GS2_LAT"            envDefault:"78.2232"`
	GS2Lon          float64 `env:"GS2_LON"            envDefault:"15.6267"`
	GS3Addr         string  `env:"GS3_ADDR"           envDefault:""`
	GS3Lat          float64 `env:"GS3_LAT"            envDefault:"-53.1638"`
	GS3Lon          float64 `env:"GS3_LON"            envDefault:"-70.9171"`
	MinElevDeg      float64 `env:"MIN_ELEV_DEG"       envDefault:"5.0"`
	PollIntervalSec int     `env:"POLL_INTERVAL_SEC"  envDefault:"30"`
	LossRate        float64 `env:"LINK_LOSS_RATE"     envDefault:"0"`
}

func (cfg config) gsTargets() []gsTarget {
	gs := []gsTarget{{Name: "Nairobi", Addr: cfg.GSAddr, Lat: cfg.GSLat, Lon: cfg.GSLon}}
	if cfg.GS2Addr != "" {
		gs = append(gs, gsTarget{Name: "Svalbard", Addr: cfg.GS2Addr, Lat: cfg.GS2Lat, Lon: cfg.GS2Lon})
	}
	if cfg.GS3Addr != "" {
		gs = append(gs, gsTarget{Name: "Punta-Arenas", Addr: cfg.GS3Addr, Lat: cfg.GS3Lat, Lon: cfg.GS3Lon})
	}
	return gs
}

type gsTarget struct {
	Name string
	Addr string
	Lat  float64
	Lon  float64
}

type relayBuffer struct {
	mu           sync.RWMutex
	frame        []byte
	fetchedAt    time.Time
	upstreamNode string
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

type relayNode struct {
	partitionSC1    atomic.Bool
	partitionRelay1 atomic.Bool
	partitionGS     atomic.Bool
	dropped         atomic.Int64
	buffered        atomic.Int64
	forwarded       atomic.Int64
	pollTotal       atomic.Int64
}

type relay1Health struct {
	BufferHasData bool `json:"buffer_has_data"`
}

type partitionReq struct {
	Link   string `json:"link"`
	Active bool   `json:"active"`
}

func elevationDegForGS(elem orbital.Elements, lat, lon float64) (float64, bool) {
	now := time.Now().UTC()
	eci, err := orbital.Propagate(elem, now)
	if err != nil {
		return 0, false
	}
	ecef := orbital.ECIToECEF(eci, now)
	elevRad, _ := orbital.ElevationAzimuth(ecef, lat, lon)
	return elevRad * 180 / math.Pi, true
}

// selectGS returns the ground station with the highest elevation above
// minElevDeg, or nil if none are currently in contact.
func selectGS(elem orbital.Elements, targets []gsTarget, minElevDeg float64) *gsTarget {
	var best *gsTarget
	bestElev := math.Inf(-1)
	for i := range targets {
		if targets[i].Addr == "" {
			continue
		}
		elevDeg, ok := elevationDegForGS(elem, targets[i].Lat, targets[i].Lon)
		if !ok || elevDeg < minElevDeg {
			continue
		}
		if elevDeg > bestElev {
			bestElev = elevDeg
			best = &targets[i]
		}
	}
	return best
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

	sc3Elements := adjustForEarlyContact(orbital.Elements{
		SemiMajorAxis: 6_921_000,
		Eccentricity:  0.0001,
		Inclination:   53.0 * math.Pi / 180,
		RAAN:          math.Pi,
		ArgPerigee:    0.0,
		MeanAnomaly:   0.0,
		Epoch:         time.Now().UTC(),
	}, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg, 90, logger)

	sc3Elements.MeanAnomaly = math.Mod(sc3Elements.MeanAnomaly+math.Pi, 2*math.Pi)

	buf := &relayBuffer{}
	node := &relayNode{}
	client := &http.Client{Timeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go pollLoop(ctx, client, cfg, node, buf, logger)
	go forwardLoop(ctx, client, cfg, sc3Elements, node, buf, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		frame, fetchedAt, upstream := buf.load()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":            "ok",
			"relay_scid":        cfg.RelaySCID,
			"sc1_addr":          cfg.SC1Addr,
			"relay1_addr":       cfg.Relay1Addr,
			"gs_addr":           cfg.GSAddr,
			"buffer_has_data":   len(frame) > 0,
			"last_fetch_at":     fetchedAt,
			"upstream_node":     upstream,
			"link_loss_rate":    cfg.LossRate,
			"partition_sc1":     node.partitionSC1.Load(),
			"partition_relay1":  node.partitionRelay1.Load(),
			"partition_gs":      node.partitionGS.Load(),
		})
	})

	mux.HandleFunc("/windows", relayWindowsHandler(sc3Elements, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg))

	mux.HandleFunc("/telemetry", relay2TelemetryHandler(sc3Elements, cfg.RelaySCID))

	mux.HandleFunc("/control/partition", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req partitionReq
		if err := json.NewDecoder(io.LimitReader(r.Body, 256)).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
			return
		}
		switch req.Link {
		case "sc1":
			node.partitionSC1.Store(req.Active)
			logger.Info("relay-2: Satellite-1 link partition updated", slog.Bool("active", req.Active))
		case "relay1":
			node.partitionRelay1.Store(req.Active)
			logger.Info("relay-2: Relay-1 link partition updated", slog.Bool("active", req.Active))
		case "gs":
			node.partitionGS.Store(req.Active)
			logger.Info("relay-2: GS link partition updated", slog.Bool("active", req.Active))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "link must be 'sc1', 'relay1', or 'gs'"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"link": req.Link, "active": req.Active})
	})

	mux.HandleFunc("/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":       "Satellite-3",
			"role":          "relay",
			"scid":          cfg.RelaySCID,
			"protocols":     []string{"orbitron-v2", "dtn", "ccsds-tm", "ccsds-sp"},
			"bandwidth_bps": 20480,
			"features": []string{
				"isl_mesh",
				"peer_routing",
				"link_throttle",
				"packet_loss_sim",
				"partition_sim",
				"multi_gs_downlink",
			},
			"link_loss_rate":   cfg.LossRate,
			"partition_sc1":    node.partitionSC1.Load(),
			"partition_relay1": node.partitionRelay1.Load(),
			"partition_gs":     node.partitionGS.Load(),
		})
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		scid := fmt.Sprintf("%d", cfg.RelaySCID)
		frame, _, _ := buf.load()
		hasData := 0.0
		if len(frame) > 0 {
			hasData = 1.0
		}
		partSC1, partR1, partGS := 0.0, 0.0, 0.0
		if node.partitionSC1.Load() {
			partSC1 = 1.0
		}
		if node.partitionRelay1.Load() {
			partR1 = 1.0
		}
		if node.partitionGS.Load() {
			partGS = 1.0
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# HELP orbitron_frames_buffered_total Total frames buffered since startup\n")
		fmt.Fprintf(w, "# TYPE orbitron_frames_buffered_total counter\n")
		fmt.Fprintf(w, "orbitron_frames_buffered_total{scid=%q} %d\n", scid, node.buffered.Load())
		fmt.Fprintf(w, "# HELP orbitron_frames_forwarded_total Total frames forwarded to a ground station\n")
		fmt.Fprintf(w, "# TYPE orbitron_frames_forwarded_total counter\n")
		fmt.Fprintf(w, "orbitron_frames_forwarded_total{scid=%q} %d\n", scid, node.forwarded.Load())
		fmt.Fprintf(w, "# HELP orbitron_frames_dropped_total Frames dropped due to simulated link packet loss\n")
		fmt.Fprintf(w, "# TYPE orbitron_frames_dropped_total counter\n")
		fmt.Fprintf(w, "orbitron_frames_dropped_total{scid=%q} %d\n", scid, node.dropped.Load())
		fmt.Fprintf(w, "# HELP orbitron_poll_total Total upstream poll attempts\n")
		fmt.Fprintf(w, "# TYPE orbitron_poll_total counter\n")
		fmt.Fprintf(w, "orbitron_poll_total{scid=%q} %d\n", scid, node.pollTotal.Load())
		fmt.Fprintf(w, "# HELP orbitron_buffer_has_data 1 if the frame buffer currently holds data\n")
		fmt.Fprintf(w, "# TYPE orbitron_buffer_has_data gauge\n")
		fmt.Fprintf(w, "orbitron_buffer_has_data{scid=%q} %g\n", scid, hasData)
		fmt.Fprintf(w, "# HELP orbitron_link_loss_rate Configured packet loss probability for this link\n")
		fmt.Fprintf(w, "# TYPE orbitron_link_loss_rate gauge\n")
		fmt.Fprintf(w, "orbitron_link_loss_rate{scid=%q} %g\n", scid, cfg.LossRate)
		fmt.Fprintf(w, "# HELP orbitron_partition_sc1 1 if the Satellite-1 uplink is currently partitioned\n")
		fmt.Fprintf(w, "# TYPE orbitron_partition_sc1 gauge\n")
		fmt.Fprintf(w, "orbitron_partition_sc1{scid=%q} %g\n", scid, partSC1)
		fmt.Fprintf(w, "# HELP orbitron_partition_relay1 1 if the Relay-1 peer link is currently partitioned\n")
		fmt.Fprintf(w, "# TYPE orbitron_partition_relay1 gauge\n")
		fmt.Fprintf(w, "orbitron_partition_relay1{scid=%q} %g\n", scid, partR1)
		fmt.Fprintf(w, "# HELP orbitron_partition_gs 1 if the ground station downlink is currently partitioned\n")
		fmt.Fprintf(w, "# TYPE orbitron_partition_gs gauge\n")
		fmt.Fprintf(w, "orbitron_partition_gs{scid=%q} %g\n", scid, partGS)
	})

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("relay-2 (Satellite-3) starting",
			slog.String("addr", cfg.Addr),
			slog.Uint64("scid", uint64(cfg.RelaySCID)),
			slog.String("orbit", "550km/53deg/RAAN=180deg"),
			slog.String("gs_addr", cfg.GSAddr),
			slog.Float64("link_loss_rate", cfg.LossRate),
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

func pollLoop(ctx context.Context, client *http.Client, cfg config, node *relayNode, buf *relayBuffer, logger *slog.Logger) {
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	fetchBest(ctx, client, cfg, node, buf, logger)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetchBest(ctx, client, cfg, node, buf, logger)
		}
	}
}

func fetchBest(ctx context.Context, client *http.Client, cfg config, node *relayNode, buf *relayBuffer, logger *slog.Logger) {
	node.pollTotal.Add(1)

	if !node.partitionRelay1.Load() && relay1HasData(ctx, client, cfg.Relay1Addr) {
		if fetchFrom(ctx, client, cfg.Relay1Addr+"/frame/orbitron", "Satellite-2", cfg.LossRate, node, buf, logger) {
			return
		}
	}

	if !node.partitionSC1.Load() {
		fetchFrom(ctx, client, cfg.SC1Addr+"/frame/orbitron", "Satellite-1", cfg.LossRate, node, buf, logger)
	}
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

func fetchFrom(ctx context.Context, client *http.Client, url, upstreamName string, lossRate float64, node *relayNode, buf *relayBuffer, logger *slog.Logger) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("relay-2 poll: upstream unreachable",
			slog.String("node", upstreamName), slog.String("url", url), slog.Any("error", err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}
	throttled := link.NewThrottle(io.LimitReader(resp.Body, 8192), 20480)
	frame, err := io.ReadAll(throttled)
	if err != nil || len(frame) == 0 {
		return false
	}

	if link.ShouldDrop(lossRate) {
		node.dropped.Add(1)
		logger.Info("relay-2 poll: frame dropped (simulated link loss)",
			slog.String("upstream", upstreamName),
			slog.Float64("loss_rate", lossRate),
			slog.Int64("total_dropped", node.dropped.Load()),
		)
		return false
	}

	buf.store(frame, upstreamName)
	node.buffered.Add(1)
	logger.Info("relay-2 poll: frame buffered",
		slog.String("upstream", upstreamName), slog.Int("bytes", len(frame)))
	return true
}

func forwardLoop(ctx context.Context, client *http.Client, cfg config, elem orbital.Elements, node *relayNode, buf *relayBuffer, logger *slog.Logger) {
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

			if node.partitionGS.Load() {
				logger.Debug("relay-2: GS link partitioned — frame held")
				continue
			}

			gs := selectGS(elem, cfg.gsTargets(), cfg.MinElevDeg)
			if gs == nil {
				continue
			}
			if time.Since(lastForwarded) < 5*time.Second {
				continue
			}
			if err := forwardFrame(ctx, client, gs.Addr, "Satellite-3", frame); err != nil {
				logger.Warn("relay-2: forward failed",
					slog.String("gs", gs.Name),
					slog.Any("error", err),
				)
			} else {
				node.forwarded.Add(1)
				lastForwarded = time.Now()
				logger.Info("relay-2: frame forwarded to GS via ISL",
					slog.String("upstream", upstream),
					slog.Int("bytes", len(frame)),
					slog.Duration("frame_age", time.Since(fetchedAt)),
					slog.String("gs_name", gs.Name),
					slog.Float64("gs_lat", gs.Lat),
					slog.Float64("gs_lon", gs.Lon),
				)
			}
		}
	}
}

func relay2TelemetryHandler(elem orbital.Elements, scid uint16) http.HandlerFunc {
	const orbitalPeriodSec = 5700.0

	return func(w http.ResponseWriter, _ *http.Request) {
		elapsed := time.Since(elem.Epoch).Seconds()
		phase := math.Mod(elapsed/orbitalPeriodSec, 1.0)
		inEclipse := phase > 0.65

		var batV, solarV, tempC float64
		if inEclipse {
			solarV = 0
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
			"name":          "Satellite-3",
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

func forwardFrame(ctx context.Context, client *http.Client, targetAddr, relayID string, frame []byte) error {
	url := targetAddr + "/receive"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(frame))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Relayed-By", relayID)

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
