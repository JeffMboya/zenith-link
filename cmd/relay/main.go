// Command relay runs the Orbitron ISL relay service.
//
// Each relay instance polls one or more primary spacecraft for telemetry frames,
// stores them in a DTN bundle store, and forwards them to whichever ground station
// has the best elevation angle and clearest atmospheric conditions at the time of
// contact. When cloud cover impairs all reachable ground stations, the relay hands
// bundles off to peer relays via the ISL mesh (/receive/bundle endpoint).
//
// The relay also polls /isl/frame on each primary spacecraft to collect frames that
// a peer spacecraft pushed to that primary via a direct cross-link (e.g. ISS → Tiangong).
//
// Configuration is read from environment variables:
//
//	RELAY_ADDR             HTTP listen address (default: :8082)
//	RELAY_NORAD_ID         NORAD catalog number for this relay's own orbit (e.g. 43013)
//	SC1_ADDR               Primary-1 spacecraft base URL (required)
//	SC2_ADDR               Primary-2 spacecraft base URL (optional)
//	SC3_ADDR               Primary-3 spacecraft base URL (optional)
//	GS_ADDR                Ground Station Nairobi base URL (required)
//	GS_LAT / GS_LON        Nairobi coordinates (default: -1.2864, 36.8172)
//	GS2_ADDR / GS2_LAT / GS2_LON  Svalbard (optional)
//	GS3_ADDR / GS3_LAT / GS3_LON  Punta Arenas (optional)
//	GS4_ADDR … GS6_ADDR    Additional ground stations (optional)
//	PEER_RELAY1_ADDR       Peer relay for ISL handoff when GS link is cloud-blocked (optional)
//	PEER_RELAY2_ADDR       Second peer relay (optional)
//	RELAY_SCID             This relay's CCSDS Spacecraft ID (default: 91)
//	MIN_ELEV_DEG           Minimum elevation for GS contact [degrees] (default: 5.0)
//	CLOUD_COVER_THRESHOLD  Cloud cover index [0–1] above which GS link is considered impaired (default: 0.65)
//	MAX_BUNDLE_DEPTH       Maximum DTN store depth before low-priority bundles are dropped (default: 100)
//	POLL_INTERVAL_SEC      Spacecraft poll interval in seconds (default: 30)
//	LINK_LOSS_RATE         Frame drop probability [0.0–1.0] to simulate BER (default: 0)
//	FALLBACK_SMA_M         Fallback semi-major axis in metres if TLE fetch fails (default: 7078000)
//	FALLBACK_INC_DEG       Fallback inclination in degrees if TLE fetch fails (default: 98.0)
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
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/absmach/orbitron/pkg/cloudcover"
	"github.com/absmach/orbitron/pkg/dtn"
	"github.com/absmach/orbitron/pkg/link"
	"github.com/absmach/orbitron/pkg/orbital"
	"github.com/absmach/orbitron/pkg/orbitron"
	pkgtle "github.com/absmach/orbitron/pkg/tle"
	"github.com/caarlos0/env/v11"
)

type config struct {
	Addr                 string  `env:"RELAY_ADDR"              envDefault:":8082"`
	NoradID              string  `env:"RELAY_NORAD_ID"          envDefault:""`
	SC1Addr              string  `env:"SC1_ADDR,required"`
	SC2Addr              string  `env:"SC2_ADDR"                envDefault:""`
	SC3Addr              string  `env:"SC3_ADDR"                envDefault:""`
	GSAddr               string  `env:"GS_ADDR,required"`
	RelaySCID            uint16  `env:"RELAY_SCID"              envDefault:"91"`
	GSLat                float64 `env:"GS_LAT"                  envDefault:"-1.2864"`
	GSLon                float64 `env:"GS_LON"                  envDefault:"36.8172"`
	GS2Addr              string  `env:"GS2_ADDR"                envDefault:""`
	GS2Lat               float64 `env:"GS2_LAT"                 envDefault:"78.2232"`
	GS2Lon               float64 `env:"GS2_LON"                 envDefault:"15.6267"`
	GS3Addr              string  `env:"GS3_ADDR"                envDefault:""`
	GS3Lat               float64 `env:"GS3_LAT"                 envDefault:"-53.1638"`
	GS3Lon               float64 `env:"GS3_LON"                 envDefault:"-70.9171"`
	GS4Addr              string  `env:"GS4_ADDR"                envDefault:""`
	GS4Lat               float64 `env:"GS4_LAT"                 envDefault:"64.8201"`
	GS4Lon               float64 `env:"GS4_LON"                 envDefault:"-147.7200"`
	GS5Addr              string  `env:"GS5_ADDR"                envDefault:""`
	GS5Lat               float64 `env:"GS5_LAT"                 envDefault:"12.9716"`
	GS5Lon               float64 `env:"GS5_LON"                 envDefault:"77.5946"`
	GS6Addr              string  `env:"GS6_ADDR"                envDefault:""`
	GS6Lat               float64 `env:"GS6_LAT"                 envDefault:"-31.9505"`
	GS6Lon               float64 `env:"GS6_LON"                 envDefault:"115.8605"`
	PeerRelay1Addr       string  `env:"PEER_RELAY1_ADDR"        envDefault:""`
	PeerRelay2Addr       string  `env:"PEER_RELAY2_ADDR"        envDefault:""`
	MinElevDeg           float64 `env:"MIN_ELEV_DEG"            envDefault:"5.0"`
	CloudCoverThreshold  float64 `env:"CLOUD_COVER_THRESHOLD"   envDefault:"0.65"`
	MaxBundleDepth       int     `env:"MAX_BUNDLE_DEPTH"        envDefault:"100"`
	PollIntervalSec      int     `env:"POLL_INTERVAL_SEC"       envDefault:"30"`
	LossRate             float64 `env:"LINK_LOSS_RATE"          envDefault:"0"`
	FallbackSMAM         float64 `env:"FALLBACK_SMA_M"          envDefault:"7078000"`
	FallbackIncDeg       float64 `env:"FALLBACK_INC_DEG"        envDefault:"98.0"`
}

func (cfg config) gsTargets() []gsTarget {
	gs := []gsTarget{{Name: "Ground Station Nairobi", Addr: cfg.GSAddr, Lat: cfg.GSLat, Lon: cfg.GSLon}}
	if cfg.GS2Addr != "" {
		gs = append(gs, gsTarget{Name: "Ground Station Svalbard", Addr: cfg.GS2Addr, Lat: cfg.GS2Lat, Lon: cfg.GS2Lon})
	}
	if cfg.GS3Addr != "" {
		gs = append(gs, gsTarget{Name: "Ground Station Punta Arenas", Addr: cfg.GS3Addr, Lat: cfg.GS3Lat, Lon: cfg.GS3Lon})
	}
	if cfg.GS4Addr != "" {
		gs = append(gs, gsTarget{Name: "Ground Station Fairbanks", Addr: cfg.GS4Addr, Lat: cfg.GS4Lat, Lon: cfg.GS4Lon})
	}
	if cfg.GS5Addr != "" {
		gs = append(gs, gsTarget{Name: "Ground Station Bangalore", Addr: cfg.GS5Addr, Lat: cfg.GS5Lat, Lon: cfg.GS5Lon})
	}
	if cfg.GS6Addr != "" {
		gs = append(gs, gsTarget{Name: "Ground Station Perth", Addr: cfg.GS6Addr, Lat: cfg.GS6Lat, Lon: cfg.GS6Lon})
	}
	return gs
}

func (cfg config) primarySources() []string {
	srcs := []string{cfg.SC1Addr}
	if cfg.SC2Addr != "" {
		srcs = append(srcs, cfg.SC2Addr)
	}
	if cfg.SC3Addr != "" {
		srcs = append(srcs, cfg.SC3Addr)
	}
	return srcs
}

func (cfg config) peerRelayAddrs() []string {
	var peers []string
	if cfg.PeerRelay1Addr != "" {
		peers = append(peers, cfg.PeerRelay1Addr)
	}
	if cfg.PeerRelay2Addr != "" {
		peers = append(peers, cfg.PeerRelay2Addr)
	}
	return peers
}

type gsTarget struct {
	Name string
	Addr string
	Lat  float64
	Lon  float64
}

type relayNode struct {
	store    *dtn.Store
	elements orbital.Elements
	scid     uint16
	satName  string

	partitionSC1 atomic.Bool
	partitionSC2 atomic.Bool
	partitionSC3 atomic.Bool
	partitionGS  atomic.Bool
	dropped      atomic.Int64
	stored       atomic.Int64
	forwarded    atomic.Int64
	expired      atomic.Int64
	pollTotal    atomic.Int64
	islForwarded atomic.Int64
}

type partitionReq struct {
	Link   string `json:"link"`
	Active bool   `json:"active"`
}

func extractPriority(frame []byte) uint8 {
	if len(frame) < orbitron.HeaderSize {
		return 1
	}
	if frame[3]&orbitron.FlagPriority != 0 {
		return 2
	}
	return 1
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

// selectGS returns the ground station with the best elevation angle that is also
// not cloud-covered. When cloudThreshold is 0 the cloud cover check is skipped.
func selectGS(elem orbital.Elements, targets []gsTarget, minElevDeg, cloudThreshold float64) *gsTarget {
	now := time.Now().UTC()
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
		if cloudThreshold > 0 && cloudcover.IsImpaired(targets[i].Lat, targets[i].Lon, now, cloudThreshold) {
			continue
		}
		if elevDeg > bestElev {
			bestElev = elevDeg
			best = &targets[i]
		}
	}
	return best
}

func fetchOwnElements(cfg config, logger *slog.Logger) (orbital.Elements, string) {
	if cfg.NoradID != "" {
		elem, name, err := pkgtle.FetchByNoradID(cfg.NoradID)
		if err == nil {
			logger.Info("relay: TLE fetched from CelesTrak",
				slog.String("norad_id", cfg.NoradID),
				slog.String("name", name),
				slog.Float64("sma_km", elem.SemiMajorAxis/1000),
				slog.Float64("inc_deg", elem.Inclination*180/math.Pi),
			)
			return elem, name
		}
		logger.Warn("relay: TLE fetch failed, using fallback elements",
			slog.String("norad_id", cfg.NoradID),
			slog.Any("error", err),
		)
	}
	return orbital.Elements{
		SemiMajorAxis: cfg.FallbackSMAM,
		Eccentricity:  0.0001,
		Inclination:   cfg.FallbackIncDeg * math.Pi / 180,
		RAAN:          math.Pi / 2,
		ArgPerigee:    0,
		MeanAnomaly:   0,
		Epoch:         time.Now().UTC(),
	}, fmt.Sprintf("RELAY-%s", cfg.NoradID)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		logger.Error("failed to parse config", slog.Any("error", err))
		os.Exit(1)
	}

	elem, satName := fetchOwnElements(cfg, logger)
	if cfg.NoradID == "" {
		elem = adjustForEarlyContact(elem, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg, 90, logger)
	}

	node := &relayNode{
		store:    dtn.NewStore(),
		elements: elem,
		scid:     cfg.RelaySCID,
		satName:  satName,
	}

	client := &http.Client{Timeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go pollLoop(ctx, client, cfg, node, logger)
	go forwardLoop(ctx, client, cfg, node, logger)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := node.store.PruneExpired(); n > 0 {
					node.expired.Add(int64(n))
					logger.Info("relay: pruned expired bundles", slog.Int("count", n))
				}
				if cfg.MaxBundleDepth > 0 {
					if n := node.store.DropLowPriority(cfg.MaxBundleDepth); n > 0 {
						node.expired.Add(int64(n))
						logger.Info("relay: dropped low-priority bundles (depth limit)",
							slog.Int("count", n),
							slog.Int("max_depth", cfg.MaxBundleDepth),
						)
					}
				}
			}
		}
	}()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":                "ok",
			"sat_name":              node.satName,
			"norad_id":              cfg.NoradID,
			"relay_scid":            cfg.RelaySCID,
			"sc1_addr":              cfg.SC1Addr,
			"sc2_addr":              cfg.SC2Addr,
			"sc3_addr":              cfg.SC3Addr,
			"gs_addr":               cfg.GSAddr,
			"buffer_has_data":       node.store.HasData(),
			"bundle_count":          node.store.Len(),
			"oldest_bundle_age_sec": int64(node.store.OldestAge().Seconds()),
			"bundle_ttl_sec":        7200,
			"hop_count_max":         8,
			"link_loss_rate":        cfg.LossRate,
			"cloud_cover_threshold": cfg.CloudCoverThreshold,
			"max_bundle_depth":      cfg.MaxBundleDepth,
			"peer_relay1":           cfg.PeerRelay1Addr,
			"peer_relay2":           cfg.PeerRelay2Addr,
			"partition_sc1":         node.partitionSC1.Load(),
			"partition_sc2":         node.partitionSC2.Load(),
			"partition_sc3":         node.partitionSC3.Load(),
			"partition_gs":          node.partitionGS.Load(),
			"isl_forwarded":         node.islForwarded.Load(),
		})
	})

	mux.HandleFunc("/frame/orbitron", func(w http.ResponseWriter, _ *http.Request) {
		b := node.store.Next()
		if b == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b.Payload)
	})

	mux.HandleFunc("/windows", relayWindowsHandler(elem, cfg.GSLat, cfg.GSLon, cfg.MinElevDeg))
	mux.HandleFunc("/telemetry", relayTelemetryHandler(node))

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
			logger.Info("relay: Primary-1 link partition updated", slog.Bool("active", req.Active))
		case "sc2":
			node.partitionSC2.Store(req.Active)
			logger.Info("relay: Primary-2 link partition updated", slog.Bool("active", req.Active))
		case "sc3":
			node.partitionSC3.Store(req.Active)
			logger.Info("relay: Primary-3 link partition updated", slog.Bool("active", req.Active))
		case "gs":
			node.partitionGS.Store(req.Active)
			logger.Info("relay: GS link partition updated", slog.Bool("active", req.Active))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "link must be 'sc1', 'sc2', 'sc3', or 'gs'"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"link": req.Link, "active": req.Active})
	})

	mux.HandleFunc("/receive/bundle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 65536))
		if err != nil || len(data) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b, err := dtn.DecodeBundle(data)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if b.HopCount >= 8 {
			w.WriteHeader(http.StatusConflict) // hop limit exceeded
			return
		}
		b.HopCount++
		node.store.Put(b)
		node.stored.Add(1)
		logger.Info("relay: bundle received from ISL peer relay",
			slog.Uint64("bundle_id", b.ID),
			slog.Int("bytes", len(b.Payload)),
			slog.Uint64("hop_count", uint64(b.HopCount)),
			slog.String("source_sc", b.SourceSC),
		)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":            node.satName,
			"norad_id":           cfg.NoradID,
			"role":               "relay",
			"scid":               cfg.RelaySCID,
			"protocols":          []string{"orbitron-v2", "dtn", "ccsds-tm", "ccsds-sp"},
			"bandwidth_bps":      20480,
			"dtn_bundle_ttl_sec": 7200,
			"dtn_max_hops":       8,
			"features": []string{
				"store_carry_forward",
				"isl_mesh",
				"isl_peer_relay",
				"cloud_cover_routing",
				"multi_primary",
				"isl_frame_poll",
				"link_throttle",
				"packet_loss_sim",
				"partition_sim",
				"multi_gs_downlink",
				"bandwidth_aware_drop",
			},
			"primaries":             cfg.primarySources(),
			"peer_relays":           cfg.peerRelayAddrs(),
			"cloud_cover_threshold": cfg.CloudCoverThreshold,
			"link_loss_rate":        cfg.LossRate,
			"partition_sc1":         node.partitionSC1.Load(),
			"partition_sc2":         node.partitionSC2.Load(),
			"partition_sc3":         node.partitionSC3.Load(),
			"partition_gs":          node.partitionGS.Load(),
		})
	})

	scid := fmt.Sprintf("%d", cfg.RelaySCID)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		partSC1, partSC2, partGS := 0.0, 0.0, 0.0
		if node.partitionSC1.Load() {
			partSC1 = 1
		}
		if node.partitionSC2.Load() {
			partSC2 = 1
		}
		if node.partitionGS.Load() {
			partGS = 1
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# HELP orbitron_bundles_stored_total Total DTN bundles stored\n")
		fmt.Fprintf(w, "# TYPE orbitron_bundles_stored_total counter\n")
		fmt.Fprintf(w, "orbitron_bundles_stored_total{scid=%q} %d\n", scid, node.stored.Load())
		fmt.Fprintf(w, "# HELP orbitron_bundles_forwarded_total Total DTN bundles forwarded\n")
		fmt.Fprintf(w, "# TYPE orbitron_bundles_forwarded_total counter\n")
		fmt.Fprintf(w, "orbitron_bundles_forwarded_total{scid=%q} %d\n", scid, node.forwarded.Load())
		fmt.Fprintf(w, "# HELP orbitron_bundles_expired_total Total DTN bundles expired\n")
		fmt.Fprintf(w, "# TYPE orbitron_bundles_expired_total counter\n")
		fmt.Fprintf(w, "orbitron_bundles_expired_total{scid=%q} %d\n", scid, node.expired.Load())
		fmt.Fprintf(w, "# HELP orbitron_bundle_store_depth Current bundle store depth\n")
		fmt.Fprintf(w, "# TYPE orbitron_bundle_store_depth gauge\n")
		fmt.Fprintf(w, "orbitron_bundle_store_depth{scid=%q} %d\n", scid, node.store.Len())
		fmt.Fprintf(w, "# HELP orbitron_frames_dropped_total Frames dropped due to simulated packet loss\n")
		fmt.Fprintf(w, "# TYPE orbitron_frames_dropped_total counter\n")
		fmt.Fprintf(w, "orbitron_frames_dropped_total{scid=%q} %d\n", scid, node.dropped.Load())
		fmt.Fprintf(w, "# HELP orbitron_isl_forwarded_total Total bundles handed off to peer relays via ISL mesh\n")
		fmt.Fprintf(w, "# TYPE orbitron_isl_forwarded_total counter\n")
		fmt.Fprintf(w, "orbitron_isl_forwarded_total{scid=%q} %d\n", scid, node.islForwarded.Load())
		fmt.Fprintf(w, "# HELP orbitron_poll_total Total upstream poll attempts\n")
		fmt.Fprintf(w, "# TYPE orbitron_poll_total counter\n")
		fmt.Fprintf(w, "orbitron_poll_total{scid=%q} %d\n", scid, node.pollTotal.Load())
		fmt.Fprintf(w, "# HELP orbitron_link_loss_rate Configured packet loss probability\n")
		fmt.Fprintf(w, "# TYPE orbitron_link_loss_rate gauge\n")
		fmt.Fprintf(w, "orbitron_link_loss_rate{scid=%q} %g\n", scid, cfg.LossRate)
		fmt.Fprintf(w, "# HELP orbitron_partition_sc1 1 if Primary-1 uplink is partitioned\n")
		fmt.Fprintf(w, "# TYPE orbitron_partition_sc1 gauge\n")
		fmt.Fprintf(w, "orbitron_partition_sc1{scid=%q} %g\n", scid, partSC1)
		fmt.Fprintf(w, "# HELP orbitron_partition_sc2 1 if Primary-2 uplink is partitioned\n")
		fmt.Fprintf(w, "# TYPE orbitron_partition_sc2 gauge\n")
		fmt.Fprintf(w, "orbitron_partition_sc2{scid=%q} %g\n", scid, partSC2)
		partSC3 := 0.0
		if node.partitionSC3.Load() {
			partSC3 = 1
		}
		fmt.Fprintf(w, "# HELP orbitron_partition_sc3 1 if Primary-3 uplink is partitioned\n")
		fmt.Fprintf(w, "# TYPE orbitron_partition_sc3 gauge\n")
		fmt.Fprintf(w, "orbitron_partition_sc3{scid=%q} %g\n", scid, partSC3)
		fmt.Fprintf(w, "# HELP orbitron_partition_gs 1 if GS downlink is partitioned\n")
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
		logger.Info("relay service starting",
			slog.String("addr", cfg.Addr),
			slog.String("sat_name", node.satName),
			slog.String("norad_id", cfg.NoradID),
			slog.Uint64("scid", uint64(cfg.RelaySCID)),
			slog.String("sc1_addr", cfg.SC1Addr),
			slog.String("sc2_addr", cfg.SC2Addr),
			slog.Float64("link_loss_rate", cfg.LossRate),
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

func pollLoop(ctx context.Context, client *http.Client, cfg config, node *relayNode, logger *slog.Logger) {
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()
	fetchAll(ctx, client, cfg, node, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetchAll(ctx, client, cfg, node, logger)
		}
	}
}

func fetchAll(ctx context.Context, client *http.Client, cfg config, node *relayNode, logger *slog.Logger) {
	node.pollTotal.Add(1)
	fetchFromPrimary(ctx, client, cfg.SC1Addr, "Primary-1", "sc1", node.partitionSC1.Load(), cfg, node, logger)
	if cfg.SC2Addr != "" {
		fetchFromPrimary(ctx, client, cfg.SC2Addr, "Primary-2", "sc2", node.partitionSC2.Load(), cfg, node, logger)
	}
	if cfg.SC3Addr != "" {
		fetchFromPrimary(ctx, client, cfg.SC3Addr, "Primary-3", "sc3", node.partitionSC3.Load(), cfg, node, logger)
	}
	// Poll ISL buffers on each primary: frames a peer spacecraft pushed to them via cross-link.
	fetchISLFrames(ctx, client, cfg.SC1Addr, "sc1", cfg, node, logger)
	if cfg.SC2Addr != "" {
		fetchISLFrames(ctx, client, cfg.SC2Addr, "sc2", cfg, node, logger)
	}
	if cfg.SC3Addr != "" {
		fetchISLFrames(ctx, client, cfg.SC3Addr, "sc3", cfg, node, logger)
	}
}

// fetchISLFrames polls /isl/frame on a primary spacecraft to collect any frames
// that a peer spacecraft pushed to it via a direct cross-link.
func fetchISLFrames(ctx context.Context, client *http.Client, addr, primaryTag string, cfg config, node *relayNode, logger *slog.Logger) {
	url := addr + "/isl/frame"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return
	}
	if resp.StatusCode != http.StatusOK {
		return
	}
	frame, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil || len(frame) == 0 {
		return
	}
	sourceSC := resp.Header.Get("X-Source-SC")
	if sourceSC == "" {
		sourceSC = "isl-" + primaryTag
	}
	if link.ShouldDrop(cfg.LossRate) {
		node.dropped.Add(1)
		return
	}
	b := &dtn.Bundle{
		ID:          uint64(time.Now().UnixNano()) ^ 0x1,
		Source:      dtn.EID{Node: 2, Service: 1},
		Destination: dtn.EID{Node: 0, Service: 1},
		CreatedAt:   time.Now().UTC(),
		Lifetime:    2 * time.Hour,
		Payload:     frame,
		Priority:    extractPriority(frame),
		SourceSC:    sourceSC,
	}
	node.store.Put(b)
	node.stored.Add(1)
	logger.Info("relay: ISL frame stored from primary cross-link",
		slog.String("primary", primaryTag),
		slog.String("source_sc", sourceSC),
		slog.Int("bytes", len(frame)),
	)
}

func fetchFromPrimary(ctx context.Context, client *http.Client, addr, name, sourceSC string, partitioned bool, cfg config, node *relayNode, logger *slog.Logger) {
	if partitioned {
		logger.Debug("relay: link partitioned — poll skipped", slog.String("primary", name))
		return
	}
	url := addr + "/frame/orbitron"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Warn("relay poll: build request failed", slog.String("primary", name), slog.Any("error", err))
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("relay poll: primary unreachable", slog.String("primary", name), slog.String("url", url), slog.Any("error", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		// Onboard pre-processor suppressed this frame — nothing to store.
		logger.Debug("relay poll: frame suppressed by onboard pre-processor", slog.String("primary", name))
		return
	}
	if resp.StatusCode != http.StatusOK {
		logger.Warn("relay poll: primary returned non-200", slog.String("primary", name), slog.Int("status", resp.StatusCode))
		return
	}
	frame, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil || len(frame) == 0 {
		logger.Warn("relay poll: empty or unreadable frame", slog.String("primary", name))
		return
	}
	if link.ShouldDrop(cfg.LossRate) {
		node.dropped.Add(1)
		logger.Info("relay poll: frame dropped (simulated link loss)", slog.String("primary", name), slog.Float64("loss_rate", cfg.LossRate))
		return
	}
	b := &dtn.Bundle{
		ID:          uint64(time.Now().UnixNano()),
		Source:      dtn.EID{Node: 1, Service: 1},
		Destination: dtn.EID{Node: 0, Service: 1},
		CreatedAt:   time.Now().UTC(),
		Lifetime:    2 * time.Hour,
		Payload:     frame,
		Priority:    extractPriority(frame),
		SourceSC:    sourceSC,
	}
	node.store.Put(b)
	node.stored.Add(1)
	logger.Info("relay poll: frame stored in DTN bundle",
		slog.String("primary", name),
		slog.Int("bytes", len(frame)),
		slog.Uint64("bundle_id", b.ID),
		slog.Int("bundle_count", node.store.Len()),
	)
}

func forwardLoop(ctx context.Context, client *http.Client, cfg config, node *relayNode, logger *slog.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	var lastForwarded time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !node.store.HasData() {
				continue
			}
			if node.partitionGS.Load() {
				logger.Debug("relay: GS link partitioned — bundle buffered")
				continue
			}
			gs := selectGS(node.elements, cfg.gsTargets(), cfg.MinElevDeg, cfg.CloudCoverThreshold)
			if gs == nil {
				// No GS in view or all reachable GS are cloud-blocked — try ISL peer relay handoff.
				if peers := cfg.peerRelayAddrs(); len(peers) > 0 {
					if b := node.store.Next(); b != nil {
						if ok := tryPeerRelays(ctx, client, peers, b, logger); ok {
							node.store.Remove(b.ID)
							node.forwarded.Add(1)
							node.islForwarded.Add(1)
							logger.Info("relay: bundle handed off to ISL peer relay",
								slog.Uint64("bundle_id", b.ID),
								slog.String("source_sc", b.SourceSC),
							)
						}
					}
				} else {
					logger.Debug("relay: not in contact with any clear GS — bundle buffered")
				}
				continue
			}
			for {
				if time.Since(lastForwarded) < 5*time.Second {
					break
				}
				bfwd := node.store.Next()
				if bfwd == nil {
					break
				}
				if err := forwardFrame(ctx, client, gs.Addr, node.satName, bfwd.SourceSC, bfwd.Payload); err != nil {
					logger.Warn("relay: forward failed",
						slog.Uint64("bundle_id", bfwd.ID),
						slog.String("gs", gs.Name),
						slog.Any("error", err),
					)
					break
				}
				node.store.Remove(bfwd.ID)
				node.forwarded.Add(1)
				lastForwarded = time.Now()
				logger.Info("relay: bundle forwarded to GS",
					slog.Uint64("bundle_id", bfwd.ID),
					slog.Int("bytes", len(bfwd.Payload)),
					slog.String("gs_name", gs.Name),
					slog.String("source_sc", bfwd.SourceSC),
				)
			}
		}
	}
}

// tryPeerRelays attempts to hand a bundle off to one of the configured peer relays.
// Returns true if a peer accepted the bundle.
func tryPeerRelays(ctx context.Context, client *http.Client, peers []string, b *dtn.Bundle, logger *slog.Logger) bool {
	if b.HopCount >= 8 {
		logger.Warn("relay: bundle hop limit reached — dropping", slog.Uint64("bundle_id", b.ID))
		return false
	}
	b.HopCount++
	encoded := b.Encode()
	for _, peer := range peers {
		url := peer + "/receive/bundle"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := client.Do(req)
		if err != nil {
			logger.Debug("relay: peer relay unreachable", slog.String("peer", peer), slog.Any("error", err))
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true
		}
	}
	b.HopCount-- // restore on failure
	return false
}

func forwardFrame(ctx context.Context, client *http.Client, targetAddr, relayID, sourceSC string, frame []byte) error {
	url := targetAddr + "/receive"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(frame))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Relayed-By", relayID)
	if sourceSC != "" {
		req.Header.Set("X-Source-SC", sourceSC)
	}
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

func relayTelemetryHandler(node *relayNode) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		elem := node.elements
		const orbitalPeriodSec = 5913.0
		elapsed := time.Since(elem.Epoch).Seconds()
		phase := math.Mod(elapsed/orbitalPeriodSec, 1.0)
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
			"scid":          node.scid,
			"name":          node.satName,
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

func relayWindowsHandler(elem orbital.Elements, defaultGSLat, defaultGSLon, minElevDeg float64) http.HandlerFunc {
	type winRes struct {
		AOS         time.Time `json:"aos"`
		LOS         time.Time `json:"los"`
		DurationSec float64   `json:"duration_sec"`
		MaxElevDeg  float64   `json:"max_elevation_deg"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		gsLat, gsLon := defaultGSLat, defaultGSLon
		if s := r.URL.Query().Get("gs_lat"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				gsLat = v
			}
		}
		if s := r.URL.Query().Get("gs_lon"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				gsLon = v
			}
		}
		now := time.Now().UTC()
		inContact, _ := orbital.IsInContact(elem, gsLat, gsLon, now, minElevDeg)
		windows, err := orbital.ContactWindows(elem, gsLat, gsLon, now, now.Add(6*time.Hour), minElevDeg)
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
		logger.Warn("relay: startup contact scan failed — using default mean anomaly", slog.Any("error", err))
		return elem
	}
	timeToAOS := windows[0].AOS.Sub(now).Seconds()
	if timeToAOS <= targetLeadSec {
		logger.Info("relay: first contact window within target lead — no adjustment needed", slog.Float64("time_to_aos_sec", timeToAOS))
		return elem
	}
	const mu = 3.986004418e14
	a := elem.SemiMajorAxis
	T := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	advance := timeToAOS - targetLeadSec
	elem.MeanAnomaly = math.Mod(elem.MeanAnomaly+2*math.Pi*advance/T, 2*math.Pi)
	logger.Info("relay: mean anomaly adjusted for early startup contact window",
		slog.Float64("advance_sec", advance),
		slog.Float64("original_aos_sec", timeToAOS),
	)
	return elem
}
