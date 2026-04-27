package api

// TLE (Two-Line Element) integration with CelesTrak.
//
// GET /tle/import?group=<group>&limit=<n>
//
//	group: stations | starlink | planet | active
//	limit: max satellites to import (default 100; ignored for "stations")
//
// GET /tle/status  — returns current data source and count
//
// GET /tle/clear   — reverts to the hardcoded 16-satellite simulation
//
// After a successful import, GET /constellation returns TLE-derived positions
// instead of the hardcoded constellation. The switch is transparent — the
// frontend polling /constellation at 1Hz sees the change immediately.

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/absmach/zenith-link/pkg/orbital"
)

const celestrakBase = "https://celestrak.org/NORAD/elements/gp.php"

// Supported groups and their CelesTrak group names.
var celestrakGroups = map[string]string{
	"stations": "stations",
	"starlink": "starlink",
	"planet":   "planet",
	"active":   "active",
}

// tleEntry holds a parsed satellite from a TLE set.
type tleEntry struct {
	Name     string
	NoradID  string
	Elements orbital.Elements
}

// tleDataStore is the active TLE dataset.  nil slice = use hardcoded simulation.
var tleDataStore struct {
	sync.RWMutex
	entries  []tleEntry
	group    string
	loadedAt time.Time
}

// tleEntries returns a snapshot of the current TLE entries (nil if none loaded).
func tleEntries() ([]tleEntry, string) {
	tleDataStore.RLock()
	defer tleDataStore.RUnlock()
	return tleDataStore.entries, tleDataStore.group
}

// importTLEs fetches a TLE group from CelesTrak, parses it, and stores the result.
func importTLEs(group string, limit int) (int, error) {
	celestrakGroup, ok := celestrakGroups[group]
	if !ok {
		return 0, fmt.Errorf("unknown group %q — valid: stations, starlink, planet, active", group)
	}

	url := fmt.Sprintf("%s?GROUP=%s&FORMAT=tle", celestrakBase, celestrakGroup)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("CelesTrak fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("CelesTrak returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // cap at 10 MB
	if err != nil {
		return 0, fmt.Errorf("reading response: %w", err)
	}

	entries, err := parseTLEText(string(body), limit)
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, fmt.Errorf("no valid TLE records parsed from CelesTrak response")
	}

	tleDataStore.Lock()
	tleDataStore.entries = entries
	tleDataStore.group = group
	tleDataStore.loadedAt = time.Now().UTC()
	tleDataStore.Unlock()

	return len(entries), nil
}

// parseTLEText splits raw TLE text (3-line format: name + line1 + line2) into entries.
func parseTLEText(text string, limit int) ([]tleEntry, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(text), "\n")

	var entries []tleEntry
	for i := 0; i+2 < len(lines); i += 3 {
		name := strings.TrimSpace(lines[i])
		l1 := strings.TrimSpace(lines[i+1])
		l2 := strings.TrimSpace(lines[i+2])

		if len(l1) < 69 || len(l2) < 69 {
			continue
		}
		if l1[0] != '1' || l2[0] != '2' {
			continue
		}

		elem, norad, err := tleLinesToElements(l1, l2)
		if err != nil {
			continue
		}

		entries = append(entries, tleEntry{
			Name:     name,
			NoradID:  norad,
			Elements: elem,
		})

		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries, nil
}

// tleLinesToElements parses TLE line1 + line2 into orbital.Elements.
// TLE character positions follow the NORAD standard (0-indexed in Go):
//
//	Line 1: [18:20] epoch year, [20:32] epoch day+fraction
//	Line 2: [8:16] inclination, [17:25] RAAN, [26:33] eccentricity,
//	        [34:42] arg perigee, [43:51] mean anomaly, [52:63] mean motion
func tleLinesToElements(l1, l2 string) (orbital.Elements, string, error) {
	norad := strings.TrimSpace(l1[2:7])

	// ── Line 2 ────────────────────────────────────────────────────────────
	incDeg, err := strconv.ParseFloat(strings.TrimSpace(l2[8:16]), 64)
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("inclination: %w", err)
	}
	raanDeg, err := strconv.ParseFloat(strings.TrimSpace(l2[17:25]), 64)
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("RAAN: %w", err)
	}
	// Eccentricity has an implied leading "0." in the TLE format.
	ecc, err := strconv.ParseFloat("0."+strings.TrimSpace(l2[26:33]), 64)
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("eccentricity: %w", err)
	}
	argPeriDeg, err := strconv.ParseFloat(strings.TrimSpace(l2[34:42]), 64)
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("arg perigee: %w", err)
	}
	maDeg, err := strconv.ParseFloat(strings.TrimSpace(l2[43:51]), 64)
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("mean anomaly: %w", err)
	}
	// Mean motion [rev/day] → convert to rad/s → derive SMA via Kepler's 3rd law.
	mmRevDay, err := strconv.ParseFloat(strings.TrimSpace(l2[52:63]), 64)
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("mean motion: %w", err)
	}

	const mu = 3.986004418e14 // Earth gravitational parameter [m³/s²]
	n := mmRevDay * 2 * math.Pi / 86400
	sma := math.Pow(mu/(n*n), 1.0/3.0)

	// ── Line 1 epoch ──────────────────────────────────────────────────────
	epoch := parseTLEEpoch(l1)

	const toRad = math.Pi / 180
	return orbital.Elements{
		SemiMajorAxis: sma,
		Eccentricity:  ecc,
		Inclination:   incDeg * toRad,
		RAAN:          raanDeg * toRad,
		ArgPerigee:    argPeriDeg * toRad,
		MeanAnomaly:   maDeg * toRad,
		Epoch:         epoch,
	}, norad, nil
}

// parseTLEEpoch converts the two-digit year and day-fraction from TLE line 1
// into a time.Time.  Year < 57 → 2000s, year ≥ 57 → 1900s.
func parseTLEEpoch(l1 string) time.Time {
	if len(l1) < 32 {
		return time.Now().UTC()
	}
	yr2, err1 := strconv.Atoi(strings.TrimSpace(l1[18:20]))
	dayFrac, err2 := strconv.ParseFloat(strings.TrimSpace(l1[20:32]), 64)
	if err1 != nil || err2 != nil {
		return time.Now().UTC()
	}
	yr := yr2
	if yr < 57 {
		yr += 2000
	} else {
		yr += 1900
	}
	dayInt := int(dayFrac)
	frac := dayFrac - float64(dayInt)
	base := time.Date(yr, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dayInt-1)
	return base.Add(time.Duration(frac * float64(24*time.Hour)))
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

// tleImportHandler fetches and stores TLEs from CelesTrak.
func tleImportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		group := r.URL.Query().Get("group")
		if group == "" {
			group = "stations"
		}

		limit := 100
		if s := r.URL.Query().Get("limit"); s != "" {
			if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 5000 {
				limit = v
			}
		}
		// No limit on small datasets
		if group == "stations" {
			limit = 0
		}

		n, err := importTLEs(group, limit)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"group":     group,
			"imported":  n,
			"source":    "celestrak",
			"loaded_at": tleDataStore.loadedAt.Format(time.RFC3339),
		})
	}
}

// tleStatusHandler returns the current data source.
func tleStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tleDataStore.RLock()
		defer tleDataStore.RUnlock()

		if tleDataStore.entries == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"source": "simulation",
				"count":  len(constellation16),
				"group":  "",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"source":    "celestrak",
			"group":     tleDataStore.group,
			"count":     len(tleDataStore.entries),
			"loaded_at": tleDataStore.loadedAt.Format(time.RFC3339),
		})
	}
}

// tleClearHandler reverts to the hardcoded simulation.
func tleClearHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tleDataStore.Lock()
		tleDataStore.entries = nil
		tleDataStore.group = ""
		tleDataStore.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"source": "simulation",
			"count":  len(constellation16),
		})
	}
}
