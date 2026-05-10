// Package cloudcover provides cloud cover probability for ground stations,
// using live data from Open-Meteo (https://open-meteo.com) with a deterministic
// fallback model when the API is unreachable or the cache is stale.
//
// The Fetcher maintains a per-GS cache refreshed every 30 minutes. Routing
// decisions are never blocked on an HTTP call — the cache is updated by a
// background goroutine, and the fallback fires instantly when needed.
package cloudcover

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"
)

// DefaultThreshold is the cloud cover fraction [0, 1] above which a ground-station
// link is considered impaired for ISL routing decisions.
const DefaultThreshold = 0.65

const (
	openMeteoURL    = "https://api.open-meteo.com/v1/forecast"
	refreshInterval = 30 * time.Minute
	staleAfter      = 2 * time.Hour
	maxBodyBytes    = 64 * 1024
)

// GSCoord is a ground station lat/lon pair used to seed the Fetcher.
type GSCoord struct {
	Lat float64
	Lon float64
}

type gsKey struct{ lat, lon float64 }

type cacheEntry struct {
	index     float64 // [0, 1]
	fetchedAt time.Time
}

// Fetcher maintains a live cloud cover cache per ground station, refreshed from
// Open-Meteo. When the API is unavailable it falls back to the deterministic
// Index() model so relay routing is never blocked.
type Fetcher struct {
	mu     sync.RWMutex
	cache  map[gsKey]cacheEntry
	client *http.Client
}

// NewFetcher returns a Fetcher using the provided HTTP client.
// Passing nil uses a default 10-second-timeout client.
func NewFetcher(client *http.Client) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Fetcher{
		cache:  make(map[gsKey]cacheEntry),
		client: client,
	}
}

// Start launches a background refresh goroutine for each ground station.
// Each goroutine fetches immediately at startup, then every 30 minutes.
// It exits when ctx is cancelled.
func (f *Fetcher) Start(ctx context.Context, stations []GSCoord) {
	for _, gs := range stations {
		gs := gs
		go func() {
			f.refresh(ctx, gs.Lat, gs.Lon)
			t := time.NewTicker(refreshInterval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					f.refresh(ctx, gs.Lat, gs.Lon)
				}
			}
		}()
	}
}

// IsImpaired returns true when the cloud cover fraction for the given ground
// station meets or exceeds threshold. Uses the live cached value when fresh;
// falls back to Index() when the API is unavailable or the cache is stale.
func (f *Fetcher) IsImpaired(gsLat, gsLon float64, t time.Time, threshold float64) bool {
	return f.indexFor(gsLat, gsLon, t) >= threshold
}

// CachedIndex returns the cached cloud cover fraction for a GS, or -1 if no
// live data is available. Intended for health/metrics endpoints.
func (f *Fetcher) CachedIndex(gsLat, gsLon float64) float64 {
	f.mu.RLock()
	entry, ok := f.cache[gsKey{gsLat, gsLon}]
	f.mu.RUnlock()
	if ok && time.Since(entry.fetchedAt) < staleAfter {
		return entry.index
	}
	return -1
}

func (f *Fetcher) indexFor(gsLat, gsLon float64, t time.Time) float64 {
	f.mu.RLock()
	entry, ok := f.cache[gsKey{gsLat, gsLon}]
	f.mu.RUnlock()
	if ok && time.Since(entry.fetchedAt) < staleAfter {
		return entry.index
	}
	return Index(gsLat, gsLon, t)
}

func (f *Fetcher) refresh(ctx context.Context, lat, lon float64) {
	idx, err := f.fetchFromAPI(ctx, lat, lon)
	if err != nil {
		slog.Warn("cloudcover: Open-Meteo fetch failed — using deterministic fallback",
			slog.Float64("lat", lat),
			slog.Float64("lon", lon),
			slog.Any("error", err),
		)
		return
	}
	f.mu.Lock()
	f.cache[gsKey{lat, lon}] = cacheEntry{index: idx, fetchedAt: time.Now()}
	f.mu.Unlock()
	slog.Info("cloudcover: updated from Open-Meteo",
		slog.Float64("lat", lat),
		slog.Float64("lon", lon),
		slog.Float64("cloud_cover_pct", idx*100),
	)
}

type openMeteoResp struct {
	Hourly struct {
		Time       []string  `json:"time"`
		CloudCover []float64 `json:"cloud_cover"`
	} `json:"hourly"`
}

func (f *Fetcher) fetchFromAPI(ctx context.Context, lat, lon float64) (float64, error) {
	url := fmt.Sprintf(
		"%s?latitude=%g&longitude=%g&hourly=cloud_cover&forecast_days=1&timezone=UTC",
		openMeteoURL, lat, lon,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("open-meteo returned HTTP %d", resp.StatusCode)
	}
	var result openMeteoResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}
	// Hourly entries are in "2006-01-02T15:04" format; find the current hour.
	currentHour := time.Now().UTC().Format("2006-01-02T15:00")
	for i, ts := range result.Hourly.Time {
		if ts == currentHour && i < len(result.Hourly.CloudCover) {
			return result.Hourly.CloudCover[i] / 100.0, nil
		}
	}
	return 0, fmt.Errorf("no cloud cover entry for hour %s in Open-Meteo response", currentHour)
}

// Index returns a deterministic cloud cover probability [0, 1] for the given
// ground-station location at time t. Used as the fallback when Open-Meteo is
// unreachable or the Fetcher cache is stale.
func Index(gsLat, gsLon float64, t time.Time) float64 {
	dayFrac := float64(t.Hour()*3600+t.Minute()*60+t.Second()) / 86400.0
	lonRad := gsLon * math.Pi / 180
	latRad := gsLat * math.Pi / 90

	slow := 0.5 + 0.25*math.Sin(2*math.Pi*dayFrac+lonRad)
	fast := 0.15 * math.Sin(2*math.Pi*dayFrac*7.3+latRad*3)
	drift := 0.10 * math.Sin(float64(t.Unix()) * 2 * math.Pi / 3600)

	v := slow + fast + drift
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// IsImpaired is a convenience wrapper around Index for callers that do not
// need the caching Fetcher (e.g. tests, CLI tools).
func IsImpaired(gsLat, gsLon float64, t time.Time, threshold float64) bool {
	return Index(gsLat, gsLon, t) >= threshold
}
