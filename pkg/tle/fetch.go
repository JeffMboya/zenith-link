// Package tle fetches and parses Two-Line Element sets from CelesTrak.
package tle

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/absmach/orbitron/pkg/orbital"
)

const celestrakBase = "https://celestrak.org/NORAD/elements/gp.php"

var httpClient = &http.Client{Timeout: 15 * time.Second}

// FetchByNoradID fetches the current TLE for a single satellite from CelesTrak
// and returns its parsed orbital elements and common name.
func FetchByNoradID(noradID string) (orbital.Elements, string, error) {
	url := fmt.Sprintf("%s?CATNR=%s&FORMAT=tle", celestrakBase, noradID)
	resp, err := httpClient.Get(url)
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("celestrak fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return orbital.Elements{}, "", fmt.Errorf("celestrak returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return orbital.Elements{}, "", fmt.Errorf("reading response: %w", err)
	}
	return parseSingleTLE(strings.ReplaceAll(string(body), "\r\n", "\n"))
}

func parseSingleTLE(text string) (orbital.Elements, string, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 3 {
		return orbital.Elements{}, "", fmt.Errorf("expected 3 TLE lines, got %d", len(lines))
	}
	name := strings.TrimSpace(lines[0])
	l1 := strings.TrimSpace(lines[1])
	l2 := strings.TrimSpace(lines[2])
	if len(l1) < 69 || len(l2) < 69 {
		return orbital.Elements{}, "", fmt.Errorf("TLE lines too short")
	}
	elem, err := linesToElements(l1, l2)
	return elem, name, err
}

func linesToElements(l1, l2 string) (orbital.Elements, error) {
	parse := func(s string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(s), 64) }

	incDeg, err := parse(l2[8:16])
	if err != nil {
		return orbital.Elements{}, fmt.Errorf("inclination: %w", err)
	}
	raanDeg, err := parse(l2[17:25])
	if err != nil {
		return orbital.Elements{}, fmt.Errorf("RAAN: %w", err)
	}
	ecc, err := strconv.ParseFloat("0."+strings.TrimSpace(l2[26:33]), 64)
	if err != nil {
		return orbital.Elements{}, fmt.Errorf("eccentricity: %w", err)
	}
	argPeriDeg, err := parse(l2[34:42])
	if err != nil {
		return orbital.Elements{}, fmt.Errorf("arg perigee: %w", err)
	}
	maDeg, err := parse(l2[43:51])
	if err != nil {
		return orbital.Elements{}, fmt.Errorf("mean anomaly: %w", err)
	}
	mmRevDay, err := parse(l2[52:63])
	if err != nil {
		return orbital.Elements{}, fmt.Errorf("mean motion: %w", err)
	}

	const mu = 3.986004418e14
	n := mmRevDay * 2 * math.Pi / 86400
	sma := math.Pow(mu/(n*n), 1.0/3.0)

	const toRad = math.Pi / 180
	return orbital.Elements{
		SemiMajorAxis: sma,
		Eccentricity:  ecc,
		Inclination:   incDeg * toRad,
		RAAN:          raanDeg * toRad,
		ArgPerigee:    argPeriDeg * toRad,
		MeanAnomaly:   maDeg * toRad,
		Epoch:         parseEpoch(l1),
	}, nil
}

func parseEpoch(l1 string) time.Time {
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
