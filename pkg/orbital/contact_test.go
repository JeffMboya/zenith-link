package orbital_test

import (
	"math"
	"testing"
	"time"

	"github.com/absmach/satlyt-demo/pkg/errors"
	"github.com/absmach/satlyt-demo/pkg/orbital"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	nairobiLat = -1.2864
	nairobiLon = 36.8172
)

func issContactElements() orbital.Elements {
	return orbital.Elements{
		SemiMajorAxis: 6_788_000,
		Eccentricity:  0.0001,
		Inclination:   51.6 * math.Pi / 180,
		RAAN:          0.0,
		ArgPerigee:    0.0,
		MeanAnomaly:   0.0,
		Epoch:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestElevationAzimuth(t *testing.T) {
	tests := []struct {
		desc        string
		satECEF     orbital.ECIState
		gsLat       float64
		gsLon       float64
		wantElevDeg float64
		tolerance   float64
	}{
		{

			desc:        "satellite directly overhead — elevation ≈ 90°",
			satECEF:     orbital.ECIState{X: 7_000_000, Y: 0, Z: 0},
			gsLat:       0,
			gsLon:       0,
			wantElevDeg: 90,
			tolerance:   1,
		},
		{

			desc:        "satellite 90° away in longitude — below horizon",
			satECEF:     orbital.ECIState{X: 0, Y: 7_000_000, Z: 0},
			gsLat:       0,
			gsLon:       0,
			wantElevDeg: -40,
			tolerance:   5,
		},
		{

			desc:        "satellite north of GS — positive elevation",
			satECEF:     orbital.ECIState{X: 6_600_000, Y: 0, Z: 1_500_000},
			gsLat:       0,
			gsLon:       0,
			wantElevDeg: 10,
			tolerance:   20,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			elevRad, _ := orbital.ElevationAzimuth(tc.satECEF, tc.gsLat, tc.gsLon)
			elevDeg := elevRad * 180 / math.Pi
			assert.InDelta(t, tc.wantElevDeg, elevDeg, tc.tolerance)
		})
	}
}

func TestElevationAzimuth_Azimuth(t *testing.T) {

	satECEF := orbital.ECIState{X: 6_400_000, Y: 0, Z: 2_000_000}
	_, azRad := orbital.ElevationAzimuth(satECEF, 0, 0)
	azDeg := azRad * 180 / math.Pi

	assert.InDelta(t, 0, azDeg, 30)
}

func TestContactWindows(t *testing.T) {
	elem := issContactElements()
	start := elem.Epoch
	end := start.Add(24 * time.Hour)

	t.Run("ISS passes Nairobi multiple times per day", func(t *testing.T) {
		windows, err := orbital.ContactWindows(elem, nairobiLat, nairobiLon, start, end, 5.0)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, len(windows), 2, "should have at least 2 contact windows in 24h")
	})

	t.Run("each window has positive max elevation", func(t *testing.T) {
		windows, err := orbital.ContactWindows(elem, nairobiLat, nairobiLon, start, end, 5.0)
		require.NoError(t, err)
		for _, w := range windows {
			assert.GreaterOrEqual(t, w.MaxElevationDeg, 5.0, "max elev must exceed minimum")
		}
	})

	t.Run("each window has positive duration", func(t *testing.T) {
		windows, err := orbital.ContactWindows(elem, nairobiLat, nairobiLon, start, end, 5.0)
		require.NoError(t, err)
		for _, w := range windows {
			assert.Positive(t, w.Duration())
		}
	})

	t.Run("window duration is realistic for LEO — 1 to 15 minutes", func(t *testing.T) {
		windows, err := orbital.ContactWindows(elem, nairobiLat, nairobiLon, start, end, 5.0)
		require.NoError(t, err)
		for _, w := range windows {
			d := w.Duration()
			assert.GreaterOrEqual(t, d, time.Minute, "window too short")
			assert.LessOrEqual(t, d, 15*time.Minute, "window too long for LEO")
		}
	})

	t.Run("higher min elevation yields fewer windows", func(t *testing.T) {
		w5, err := orbital.ContactWindows(elem, nairobiLat, nairobiLon, start, end, 5.0)
		require.NoError(t, err)
		w30, err := orbital.ContactWindows(elem, nairobiLat, nairobiLon, start, end, 30.0)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(w5), len(w30), "higher min elev should not add windows")
	})
}

func TestContactWindows_Errors(t *testing.T) {
	elem := issContactElements()
	now := elem.Epoch

	tests := []struct {
		desc    string
		elem    orbital.Elements
		gsLat   float64
		gsLon   float64
		start   time.Time
		end     time.Time
		minElev float64
		wantErr error
	}{
		{
			desc:    "end before start",
			elem:    elem,
			gsLat:   nairobiLat,
			gsLon:   nairobiLon,
			start:   now.Add(time.Hour),
			end:     now,
			minElev: 5,
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "minElevDeg negative",
			elem:    elem,
			gsLat:   nairobiLat,
			gsLon:   nairobiLon,
			start:   now,
			end:     now.Add(time.Hour),
			minElev: -1,
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "minElevDeg >= 90",
			elem:    elem,
			gsLat:   nairobiLat,
			gsLon:   nairobiLon,
			start:   now,
			end:     now.Add(time.Hour),
			minElev: 90,
			wantErr: errors.ErrInvalidField,
		},
		{
			desc: "invalid orbital elements",
			elem: orbital.Elements{
				SemiMajorAxis: 100,
				Eccentricity:  0,
				Inclination:   0,
				Epoch:         now,
			},
			gsLat:   nairobiLat,
			gsLon:   nairobiLon,
			start:   now,
			end:     now.Add(time.Hour),
			minElev: 5,
			wantErr: errors.ErrOrbitalPropagate,
		},
		{
			desc:    "gsLat out of range (> 90)",
			elem:    elem,
			gsLat:   91,
			gsLon:   nairobiLon,
			start:   now,
			end:     now.Add(time.Hour),
			minElev: 5,
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "gsLat out of range (< -90)",
			elem:    elem,
			gsLat:   -91,
			gsLon:   nairobiLon,
			start:   now,
			end:     now.Add(time.Hour),
			minElev: 5,
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "gsLon out of range (> 180)",
			elem:    elem,
			gsLat:   nairobiLat,
			gsLon:   181,
			start:   now,
			end:     now.Add(time.Hour),
			minElev: 5,
			wantErr: errors.ErrInvalidField,
		},
		{
			desc:    "gsLon out of range (< -180)",
			elem:    elem,
			gsLat:   nairobiLat,
			gsLon:   -181,
			start:   now,
			end:     now.Add(time.Hour),
			minElev: 5,
			wantErr: errors.ErrInvalidField,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := orbital.ContactWindows(tc.elem, tc.gsLat, tc.gsLon, tc.start, tc.end, tc.minElev)
			assert.True(t, errors.Contains(err, tc.wantErr))
		})
	}
}

func TestIsInContact(t *testing.T) {
	elem := issContactElements()
	start := elem.Epoch
	end := start.Add(24 * time.Hour)

	windows, err := orbital.ContactWindows(elem, nairobiLat, nairobiLon, start, end, 5.0)
	require.NoError(t, err)
	require.NotEmpty(t, windows, "need at least one window to test IsInContact")

	w := windows[0]
	mid := w.AOS.Add(w.Duration() / 2)
	inContact, err := orbital.IsInContact(elem, nairobiLat, nairobiLon, mid, 5.0)
	require.NoError(t, err)
	assert.True(t, inContact, "mid-window should be in contact")

	before := w.AOS.Add(-30 * time.Minute)
	if before.After(start) {
		notIn, err := orbital.IsInContact(elem, nairobiLat, nairobiLon, before, 5.0)
		require.NoError(t, err)
		assert.False(t, notIn, "well before AOS should not be in contact")
	}
}

func TestIsInContact_InvalidCoordinates(t *testing.T) {
	elem := issContactElements()
	now := elem.Epoch

	_, err := orbital.IsInContact(elem, 91, 0, now, 5.0)
	assert.True(t, errors.Contains(err, errors.ErrInvalidField), "gsLat > 90 should error")

	_, err = orbital.IsInContact(elem, 0, 181, now, 5.0)
	assert.True(t, errors.Contains(err, errors.ErrInvalidField), "gsLon > 180 should error")

	_, err = orbital.IsInContact(elem, 0, 0, now, -1.0)
	assert.True(t, errors.Contains(err, errors.ErrInvalidField), "negative minElevDeg should error")

	_, err = orbital.IsInContact(elem, 0, 0, now, 90.0)
	assert.True(t, errors.Contains(err, errors.ErrInvalidField), "minElevDeg >= 90 should error")

	bad := orbital.Elements{SemiMajorAxis: 100, Epoch: now}
	_, err = orbital.IsInContact(bad, 0, 0, now, 5.0)
	assert.True(t, errors.Contains(err, errors.ErrOrbitalPropagate), "invalid elements should error")
}
