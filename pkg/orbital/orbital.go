// Package orbital implements Keplerian orbit propagation with J2 perturbation,
// ECEF/geodetic coordinate conversion, and basic eclipse detection.
//
// Reference frames:
//   - ECI (Earth-Centred Inertial): J2000 approximation, Z toward north pole,
//     X toward vernal equinox.
//   - ECEF (Earth-Centred, Earth-Fixed): rotates with the Earth.
//   - Geodetic: WGS-84 ellipsoid (latitude, longitude, altitude above ellipsoid).
//
// Propagation model:
//   - Two-body Keplerian orbit (Kepler's equation, Newton's law of gravitation).
//   - First-order J2 secular drift in right ascension of ascending node (RAAN)
//     and argument of perigee.
//   - Sun position computed from a low-precision analytical model.
//   - Cylindrical shadow model for eclipse detection.
//
// All angles are in radians unless noted otherwise in parameter/return names.
package orbital

import (
	"math"
	"time"

	"github.com/absmach/zenith-link/pkg/errors"
)

// WGS-84 constants.
const (
	earthRadiusM    = 6_378_137.0          // semi-major axis [m]
	earthFlattening = 1.0 / 298.257223563  // WGS-84 flattening
	earthMu         = 3.986004418e14       // gravitational parameter [m³/s²]
	earthJ2         = 1.082626e-3          // second zonal harmonic
	earthOmega      = 7.2921150e-5         // rotation rate [rad/s]
	auMetres        = 1.496e11            // 1 AU in metres
)

// Epoch is the J2000.0 epoch used as the reference for GMST computation.
var j2000 = time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)

// Elements holds the classical Keplerian orbital elements.
type Elements struct {
	// SemiMajorAxis is the semi-major axis [m].
	SemiMajorAxis float64
	// Eccentricity is the orbital eccentricity (0 = circular, <1 = elliptical).
	Eccentricity float64
	// Inclination is the orbital inclination [rad].
	Inclination float64
	// RAAN is the right ascension of ascending node [rad].
	RAAN float64
	// ArgPerigee is the argument of perigee [rad].
	ArgPerigee float64
	// MeanAnomaly is the mean anomaly at epoch [rad].
	MeanAnomaly float64
	// Epoch is the time at which the elements are defined.
	Epoch time.Time
}

// ECIState holds a position+velocity state in the Earth-Centred Inertial frame.
type ECIState struct {
	// Position in metres.
	X, Y, Z float64
	// Velocity in metres per second.
	VX, VY, VZ float64
}

// GeodeticPosition holds a geodetic (WGS-84) position.
type GeodeticPosition struct {
	// LatitudeDeg is the geodetic latitude [degrees, -90..+90].
	LatitudeDeg float64
	// LongitudeDeg is the geodetic longitude [degrees, -180..+180].
	LongitudeDeg float64
	// AltitudeM is the altitude above the WGS-84 ellipsoid [m].
	AltitudeM float64
}

// Validate returns an error if any element is physically unreasonable.
func (e Elements) Validate() error {
	if e.SemiMajorAxis <= earthRadiusM {
		return errors.Wrap(errors.ErrOrbitalPropagate,
			errors.New("semi-major axis must be greater than Earth's radius"))
	}
	if e.Eccentricity < 0 || e.Eccentricity >= 1 {
		return errors.Wrap(errors.ErrOrbitalPropagate,
			errors.New("eccentricity must be in [0, 1) for an elliptical orbit"))
	}
	if e.Inclination < 0 || e.Inclination > math.Pi {
		return errors.Wrap(errors.ErrOrbitalPropagate,
			errors.New("inclination must be in [0, π]"))
	}
	return nil
}

// Propagate computes the ECI state (position + velocity) at time t using the
// two-body solution with first-order J2 secular corrections.
