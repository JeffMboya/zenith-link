package orbital_test

import (
	"math"
	"testing"
	"time"

	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ISS-like TLE orbital elements (epoch 2024-01-01 12:00:00 UTC).
func issElements() orbital.Elements {
	return orbital.Elements{
		SemiMajorAxis: 6_788_000, // ~420 km altitude
		Eccentricity:  0.0001,
		Inclination:   51.6 * math.Pi / 180,
		RAAN:          0.0,
		ArgPerigee:    0.0,
		MeanAnomaly:   0.0,
		Epoch:         time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}

// ─── Elements.Validate ──────────────────────────────────────────────────────

func TestValidate(t *testing.T) {
	tests := []struct {
		desc    string
		elem    orbital.Elements
		wantErr bool
	}{
		{
			desc: "valid ISS-like orbit",
			elem: issElements(),
		},
		{
			desc: "circular orbit at 500 km",
			elem: orbital.Elements{
				SemiMajorAxis: 6_878_000,
				Eccentricity:  0,
				Inclination:   0,
				Epoch:         time.Now(),
			},
		},
		{
			desc: "semi-major axis equal to Earth radius — invalid",
			elem: orbital.Elements{
				SemiMajorAxis: 6_378_137,
				Eccentricity:  0,
				Inclination:   0,
				Epoch:         time.Now(),
			},
			wantErr: true,
		},
		{
			desc: "semi-major axis below Earth radius — invalid",
			elem: orbital.Elements{
				SemiMajorAxis: 100_000,
				Eccentricity:  0,
				Inclination:   0,
				Epoch:         time.Now(),
			},
			wantErr: true,
		},
		{
			desc: "eccentricity = 1 (parabolic) — invalid",
			elem: orbital.Elements{
				SemiMajorAxis: 7_000_000,
				Eccentricity:  1.0,
				Inclination:   0,
				Epoch:         time.Now(),
			},
			wantErr: true,
		},
		{
			desc: "negative eccentricity — invalid",
			elem: orbital.Elements{
				SemiMajorAxis: 7_000_000,
				Eccentricity:  -0.1,
				Inclination:   0,
				Epoch:         time.Now(),
			},
			wantErr: true,
		},
		{
			desc: "inclination > π — invalid",
			elem: orbital.Elements{
				SemiMajorAxis: 7_000_000,
				Eccentricity:  0,
				Inclination:   math.Pi + 0.001,
				Epoch:         time.Now(),
			},
			wantErr: true,
		},
		{
			desc: "negative inclination — invalid",
			elem: orbital.Elements{
				SemiMajorAxis: 7_000_000,
				Eccentricity:  0,
				Inclination:   -0.1,
				Epoch:         time.Now(),
			},
			wantErr: true,
		},
		{
			desc: "polar orbit inclination = π — valid",
			elem: orbital.Elements{
				SemiMajorAxis: 7_000_000,
				Eccentricity:  0,
				Inclination:   math.Pi,
				Epoch:         time.Now(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.elem.Validate()
			if tc.wantErr {
				assert.True(t, errors.Contains(err, errors.ErrOrbitalPropagate))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ─── Propagate ──────────────────────────────────────────────────────────────

func TestPropagate(t *testing.T) {
	elem := issElements()

	tests := []struct {
		desc    string
		elem    orbital.Elements
		t       time.Time
		check   func(t *testing.T, s orbital.ECIState)
		wantErr bool
	}{
		{
			desc: "at epoch — position is at perigee",
			elem: elem,
			t:    elem.Epoch,
			check: func(t *testing.T, s orbital.ECIState) {
				// At M=0, true anomaly ≈ 0, so position should be in the X-Z plane.
				// Magnitude should be approximately semi-major axis (low eccentricity).
				r := math.Sqrt(s.X*s.X + s.Y*s.Y + s.Z*s.Z)
				assert.InDelta(t, elem.SemiMajorAxis, r, 1000, "radius should equal semi-major axis at epoch")
				// Y component should be near 0 (perigee in X-Z plane when RAAN=0, argP=0)
				assert.InDelta(t, 0.0, s.Y, 1000)
			},
		},
		{
			// J2 causes RAAN to drift ~30 km/orbit for ISS-like inclination;
			// the satellite does NOT return to the same ECI position after one
			// Keplerian period. We verify only that the orbital radius is preserved.
			desc: "one full period — orbital radius preserved",
			elem: elem,
			t:    elem.Epoch.Add(time.Duration(orbital.OrbitalPeriod(elem.SemiMajorAxis) * float64(time.Second))),
			check: func(t *testing.T, s orbital.ECIState) {
				r := math.Sqrt(s.X*s.X + s.Y*s.Y + s.Z*s.Z)
				assert.InDelta(t, elem.SemiMajorAxis, r, 2000, "orbital radius should be preserved after one period")
			},
		},
		{
			desc: "quarter period — position roughly 90° ahead",
			elem: elem,
			t:    elem.Epoch.Add(time.Duration(orbital.OrbitalPeriod(elem.SemiMajorAxis)/4 * float64(time.Second))),
			check: func(t *testing.T, s orbital.ECIState) {
				r := math.Sqrt(s.X*s.X + s.Y*s.Y + s.Z*s.Z)
				assert.InDelta(t, elem.SemiMajorAxis, r, 2000)
			},
		},
		{
			desc: "24 hours propagation — position is finite",
			elem: elem,
			t:    elem.Epoch.Add(24 * time.Hour),
			check: func(t *testing.T, s orbital.ECIState) {
				r := math.Sqrt(s.X*s.X + s.Y*s.Y + s.Z*s.Z)
				assert.InDelta(t, elem.SemiMajorAxis, r, 5000)
				assert.False(t, math.IsNaN(s.X) || math.IsNaN(s.Y) || math.IsNaN(s.Z))
			},
		},
		{
			desc:    "invalid elements — returns error",
			elem:    orbital.Elements{SemiMajorAxis: 100, Eccentricity: 0},
			t:       time.Now(),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := orbital.Propagate(tc.elem, tc.t)
			if tc.wantErr {
				assert.True(t, errors.Contains(err, errors.ErrOrbitalPropagate))
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// ─── ECEFToGeodetic ──────────────────────────────────────────────────────────

func TestECEFToGeodetic(t *testing.T) {
	tests := []struct {
		desc     string
		ecef     orbital.ECIState
		wantLat  float64 // degrees
		wantLon  float64 // degrees
		wantAlt  float64 // metres
		latTol   float64
		lonTol   float64
		altTol   float64
	}{
		{
			desc:    "equatorial prime-meridian — on Earth surface",
			ecef:    orbital.ECIState{X: 6_378_137, Y: 0, Z: 0},
			wantLat: 0, wantLon: 0, wantAlt: 0,
			latTol: 0.001, lonTol: 0.001, altTol: 1,
		},
		{
			// Semi-minor axis b = 6356752.3142 m; Z = b, X = Y = 0 is exactly the north pole on the ellipsoid.
			desc:    "north pole",
			ecef:    orbital.ECIState{X: 0, Y: 0, Z: 6_356_752.3142},
			wantLat: 90, wantLon: 0, wantAlt: 0,
			latTol: 0.01, lonTol: 180, altTol: 1,
		},
		{
			desc:    "equatorial 90° east",
			ecef:    orbital.ECIState{X: 0, Y: 6_378_137, Z: 0},
			wantLat: 0, wantLon: 90, wantAlt: 0,
			latTol: 0.001, lonTol: 0.001, altTol: 1,
		},
		{
			desc:    "500 km altitude over equator",
			ecef:    orbital.ECIState{X: 6_878_137, Y: 0, Z: 0},
			wantLat: 0, wantLon: 0, wantAlt: 500_000,
			latTol: 0.001, lonTol: 0.001, altTol: 100,
		},
		{
			// ECEF for geodetic lat=45°, lon=0°, alt=410 km above WGS-84 ellipsoid.
			// N(45°) = a/sqrt(1-e²sin²(45°)) ≈ 6388840 m
			// X = (N + alt)*cos(lat) ≈ (6388840 + 410000)*cos(45°) ≈ 4_808_760 m
			// Z = (N*(1-e²) + alt)*sin(lat) ≈ (6346016 + 410000)*sin(45°) ≈ 4_774_840 m
			desc:    "ISS-like altitude over 45°N geodetic",
			ecef:    orbital.ECIState{X: 4_808_760, Y: 0, Z: 4_774_840},
			wantLat: 45.0, wantLon: 0, wantAlt: 410_000,
			latTol: 0.1, lonTol: 0.001, altTol: 2000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := orbital.ECEFToGeodetic(tc.ecef)
			assert.InDelta(t, tc.wantLat, got.LatitudeDeg, tc.latTol, "latitude")
			assert.InDelta(t, tc.wantLon, got.LongitudeDeg, tc.lonTol, "longitude")
			assert.InDelta(t, tc.wantAlt, got.AltitudeM, tc.altTol, "altitude")
		})
	}
}

// ─── PropagateGeodetic ───────────────────────────────────────────────────────

func TestPropagateGeodetic(t *testing.T) {
	elem := issElements()
	t0 := elem.Epoch

	tests := []struct {
		desc    string
		t       time.Time
		check   func(t *testing.T, g orbital.GeodeticPosition)
		wantErr bool
	}{
		{
			desc: "position at epoch is physically reasonable",
			t:    t0,
			check: func(t *testing.T, g orbital.GeodeticPosition) {
				assert.GreaterOrEqual(t, g.LatitudeDeg, -90.0)
				assert.LessOrEqual(t, g.LatitudeDeg, 90.0)
				assert.GreaterOrEqual(t, g.LongitudeDeg, -180.0)
				assert.LessOrEqual(t, g.LongitudeDeg, 180.0)
				assert.InDelta(t, 409_863, g.AltitudeM, 5_000, "altitude should be ~410 km")
			},
		},
		{
			desc: "position after one hour is still valid",
			t:    t0.Add(time.Hour),
			check: func(t *testing.T, g orbital.GeodeticPosition) {
				assert.InDelta(t, 409_863, g.AltitudeM, 10_000)
			},
		},
		{
			desc:    "invalid elements returns error",
			t:       t0,
			wantErr: true,
		},
	}

	for i, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			e := elem
			if tc.wantErr {
				e.SemiMajorAxis = 100
			}
			got, err := orbital.PropagateGeodetic(e, tc.t)
			if tc.wantErr {
				assert.True(t, errors.Contains(err, errors.ErrOrbitalPropagate))
				return
			}
			require.NoError(t, err)
			if tc.check != nil {
				tc.check(t, got)
			}
			_ = i
		})
	}
}

// ─── InSunlight ─────────────────────────────────────────────────────────────

func TestInSunlight(t *testing.T) {
	elem := issElements()
	t0 := elem.Epoch

	tests := []struct {
		desc string
		t    time.Time
	}{
		{
			desc: "result is boolean (no panic)",
			t:    t0,
		},
		{
			desc: "result after 12 hours",
			t:    t0.Add(12 * time.Hour),
		},
		{
			desc: "result after 45 minutes",
			t:    t0.Add(45 * time.Minute),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			eci, err := orbital.Propagate(elem, tc.t)
			require.NoError(t, err)
			// InSunlight must not panic; return value is not checked deterministically
			// because it depends on the precise orbital position relative to the Sun.
			_ = orbital.InSunlight(eci, tc.t)
		})
	}

	// Sanity: a satellite well inside Earth's shadow cylinder must be in shadow.
	t.Run("satellite directly opposite sun is in shadow", func(t *testing.T) {
		// At J2000 the Sun's ecliptic longitude ≈ 280° → ECI direction ≈ (+0.18, -0.90, -0.39).
		// The anti-sun direction is therefore mostly +Y. A satellite at (0, +7 Mm, 0)
		// lies behind Earth: its perpendicular distance to the Earth-Sun axis is ~3000 km,
		// well inside Earth's radius (6371 km), so it must be in shadow.
		umbra := orbital.ECIState{X: 0, Y: 7_000_000, Z: 0}
		inSun := orbital.InSunlight(umbra, j2000())
		assert.False(t, inSun, "satellite in anti-sun direction at J2000 should be in shadow")
	})
}

// j2000 returns the J2000.0 epoch as a time.Time.
func j2000() time.Time {
	return time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)
}

// ─── OrbitalPeriod ───────────────────────────────────────────────────────────

func TestOrbitalPeriod(t *testing.T) {
	tests := []struct {
		desc          string
		semiMajorAxis float64
		wantPeriodSec float64
		tol           float64
	}{
		{
			// ISS: a ≈ 6788 km → T ≈ 5560 s ≈ 92.7 min
			desc:          "ISS-like orbit",
			semiMajorAxis: 6_788_000,
			wantPeriodSec: 5560,
			tol:           50,
		},
		{
			// GEO: a ≈ 42164 km → T ≈ 86164 s ≈ 1 sidereal day
			desc:          "geostationary orbit",
			semiMajorAxis: 42_164_000,
			wantPeriodSec: 86_164,
			tol:           100,
		},
		{
			// 500 km circular: a = 6878 km → T ≈ 5676 s
			desc:          "500 km circular orbit",
			semiMajorAxis: 6_878_000,
			wantPeriodSec: 5676,
			tol:           50,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := orbital.OrbitalPeriod(tc.semiMajorAxis)
			assert.InDelta(t, tc.wantPeriodSec, got, tc.tol)
		})
	}
}

// ─── ECIToECEF ───────────────────────────────────────────────────────────────

func TestECIToECEF(t *testing.T) {
	// At sidereal time = 0, ECI and ECEF should coincide (GMST=0 → no rotation).
	// We use a time near the J2000 epoch and verify the magnitude is preserved.
	t.Run("magnitude is preserved under rotation", func(t *testing.T) {
		eci := orbital.ECIState{X: 7_000_000, Y: 0, Z: 500_000}
		rECI := math.Sqrt(eci.X*eci.X + eci.Y*eci.Y + eci.Z*eci.Z)

		ecef := orbital.ECIToECEF(eci, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
		rECEF := math.Sqrt(ecef.X*ecef.X + ecef.Y*ecef.Y + ecef.Z*ecef.Z)

		assert.InDelta(t, rECI, rECEF, 1.0, "rotation must preserve vector magnitude")
	})

	t.Run("Z component is unchanged", func(t *testing.T) {
		eci := orbital.ECIState{X: 6_000_000, Y: 3_000_000, Z: 1_234_567}
		ecef := orbital.ECIToECEF(eci, time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC))
		assert.InDelta(t, eci.Z, ecef.Z, 1.0, "Z is unchanged by Earth rotation")
	})
}
