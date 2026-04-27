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
func Propagate(elem Elements, t time.Time) (ECIState, error) {
	if err := elem.Validate(); err != nil {
		return ECIState{}, err
	}

	dt := t.Sub(elem.Epoch).Seconds()
	a := elem.SemiMajorAxis
	e := elem.Eccentricity
	inc := elem.Inclination

	// Mean motion [rad/s].
	n := math.Sqrt(earthMu / (a * a * a))

	// J2 secular rates.
	p := a * (1 - e*e)
	rp := earthRadiusM / p
	j2Factor := -1.5 * earthJ2 * rp * rp * n

	dRAAN := j2Factor * math.Cos(inc)
	dArgP := j2Factor * (2.5*math.Sin(inc)*math.Sin(inc) - 2)

	// Updated angles.
	raan := normalise(elem.RAAN + dRAAN*dt)
	argP := normalise(elem.ArgPerigee + dArgP*dt)
	M := normalise(elem.MeanAnomaly + n*dt)

	// Solve Kepler's equation M = E - e·sin(E) via Newton-Raphson.
	E, err := solveKepler(M, e)
	if err != nil {
		return ECIState{}, err
	}

	// True anomaly.
	nu := 2 * math.Atan2(
		math.Sqrt(1+e)*math.Sin(E/2),
		math.Sqrt(1-e)*math.Cos(E/2),
	)

	// Radius [m] and radial speed [m/s].
	r := a * (1 - e*math.Cos(E))
	h := math.Sqrt(earthMu * p)

	// Position and velocity in the perifocal (PQW) frame.
	cosNu, sinNu := math.Cos(nu), math.Sin(nu)
	xPQW := r * cosNu
	yPQW := r * sinNu
	vxPQW := -earthMu / h * sinNu
	vyPQW := earthMu / h * (e + cosNu)

	// Rotation from perifocal to ECI.
	state := perifocalToECI(xPQW, yPQW, vxPQW, vyPQW, inc, raan, argP)
	return state, nil
}

// ECIToECEF converts an ECI position to ECEF at time t using Greenwich Mean
// Sidereal Time (GMST).
func ECIToECEF(eci ECIState, t time.Time) ECIState {
	theta := gmst(t)
	cosT, sinT := math.Cos(theta), math.Sin(theta)

	return ECIState{
		X:  cosT*eci.X + sinT*eci.Y,
		Y:  -sinT*eci.X + cosT*eci.Y,
		Z:  eci.Z,
		VX: cosT*eci.VX + sinT*eci.VY - earthOmega*sinT*eci.X + earthOmega*cosT*eci.Y,
		VY: -sinT*eci.VX + cosT*eci.VY - earthOmega*cosT*eci.X - earthOmega*sinT*eci.Y,
		VZ: eci.VZ,
	}
}

// ECEFToGeodetic converts an ECEF position to WGS-84 geodetic coordinates
// using Bowring's iterative method (converges to sub-millimetre accuracy in
// 2–3 iterations for near-Earth orbits).
// The polar singularity (p ≈ 0) is handled separately.
func ECEFToGeodetic(ecef ECIState) GeodeticPosition {
	x, y, z := ecef.X, ecef.Y, ecef.Z

	a := earthRadiusM
	f := earthFlattening
	b := a * (1 - f)     // semi-minor axis
	e2 := 2*f - f*f      // first eccentricity squared
	ep2 := e2 / (1 - e2) // second eccentricity squared

	p := math.Sqrt(x*x + y*y)
	lon := math.Atan2(y, x)

	// Polar singularity: avoid dividing by cos(lat) ≈ 0.
	if p < 1.0 {
		lat := math.Pi / 2
		if z < 0 {
			lat = -math.Pi / 2
		}
		alt := math.Abs(z) - b
		return GeodeticPosition{
			LatitudeDeg:  lat * 180 / math.Pi,
			LongitudeDeg: lon * 180 / math.Pi,
			AltitudeM:    alt,
		}
	}

	// Initial estimate (Bowring's method).
	theta := math.Atan2(z*a, p*b)
	sinT, cosT := math.Sin(theta), math.Cos(theta)

	lat := math.Atan2(
		z+ep2*b*sinT*sinT*sinT,
		p-e2*a*cosT*cosT*cosT,
	)

	// Iterate for sub-millimetre accuracy.
	for i := 0; i < 3; i++ {
		sinL := math.Sin(lat)
		N := a / math.Sqrt(1-e2*sinL*sinL)
		lat = math.Atan2(z+e2*N*sinL, p)
	}

	sinL := math.Sin(lat)
	cosL := math.Cos(lat)
	N := a / math.Sqrt(1-e2*sinL*sinL)
	alt := p/cosL - N

	return GeodeticPosition{
		LatitudeDeg:  lat * 180 / math.Pi,
		LongitudeDeg: lon * 180 / math.Pi,
		AltitudeM:    alt,
	}
}

// PropagateGeodetic propagates elements to time t and returns the geodetic
// sub-satellite point. Combines Propagate → ECIToECEF → ECEFToGeodetic.
func PropagateGeodetic(elem Elements, t time.Time) (GeodeticPosition, error) {
	eci, err := Propagate(elem, t)
	if err != nil {
		return GeodeticPosition{}, err
	}
	ecef := ECIToECEF(eci, t)
	return ECEFToGeodetic(ecef), nil
}

// InSunlight returns true when the satellite (given its ECI position at t) is
// not in Earth's shadow, using a cylindrical shadow model.
func InSunlight(eci ECIState, t time.Time) bool {
	sunECI := sunPosition(t)

	// Project the satellite position onto the sun-direction vector.
	sLen := math.Sqrt(sunECI.X*sunECI.X + sunECI.Y*sunECI.Y + sunECI.Z*sunECI.Z)
	sx, sy, sz := sunECI.X/sLen, sunECI.Y/sLen, sunECI.Z/sLen
	dot := eci.X*sx + eci.Y*sy + eci.Z*sz

	// If dot > 0 the satellite is on the sun-side of Earth: sunlit.
	if dot > 0 {
		return true
	}

	// Compute perpendicular distance from the satellite to the Earth-Sun line.
	perpX := eci.X - dot*sx
	perpY := eci.Y - dot*sy
	perpZ := eci.Z - dot*sz
	perpDist := math.Sqrt(perpX*perpX + perpY*perpY + perpZ*perpZ)

	return perpDist > earthRadiusM
}

// OrbitalPeriod returns the period of the orbit [s] for the given semi-major axis [m].
func OrbitalPeriod(semiMajorAxis float64) float64 {
	return 2 * math.Pi * math.Sqrt(semiMajorAxis*semiMajorAxis*semiMajorAxis/earthMu)
}

// ─── internal helpers ────────────────────────────────────────────────────────

// solveKepler solves Kepler's equation M = E - e·sin(E) via Newton-Raphson.
// Converges quadratically; typical accuracy < 1e-12 rad.
func solveKepler(M, e float64) (float64, error) {
	E := M
	for i := 0; i < 50; i++ {
		dE := (M - E + e*math.Sin(E)) / (1 - e*math.Cos(E))
		E += dE
		if math.Abs(dE) < 1e-12 {
			return E, nil
		}
	}
	return 0, errors.Wrap(errors.ErrOrbitalPropagate,
		errors.New("Kepler's equation did not converge"))
}

// perifocalToECI rotates position/velocity from the perifocal (PQW) frame to
// the ECI frame given the three Euler angles (inc, RAAN, argP).
func perifocalToECI(xP, yP, vxP, vyP, inc, raan, argP float64) ECIState {
	cosR, sinR := math.Cos(raan), math.Sin(raan)
	cosI, sinI := math.Cos(inc), math.Sin(inc)
	cosW, sinW := math.Cos(argP), math.Sin(argP)

	// Rotation matrix columns (transposed, so rows are the ECI unit vectors).
	r11 := cosR*cosW - sinR*sinW*cosI
	r12 := -cosR*sinW - sinR*cosW*cosI
	r21 := sinR*cosW + cosR*sinW*cosI
	r22 := -sinR*sinW + cosR*cosW*cosI
	r31 := sinW * sinI
	r32 := cosW * sinI

	return ECIState{
		X:  r11*xP + r12*yP,
		Y:  r21*xP + r22*yP,
		Z:  r31*xP + r32*yP,
		VX: r11*vxP + r12*vyP,
		VY: r21*vxP + r22*vyP,
		VZ: r31*vxP + r32*vyP,
	}
}

// gmst returns the Greenwich Mean Sidereal Time [rad] at time t using the
// IAU 1982 formula (accurate to ~0.1 s over several decades).
func gmst(t time.Time) float64 {
	// Julian centuries since J2000.0.
	jd := julianDate(t)
	T := (jd - 2451545.0) / 36525.0

	// GMST in seconds of time at 0h UT1.
	gmstSec := 24110.54841 +
		8640184.812866*T +
		0.093104*T*T -
		6.2e-6*T*T*T

	// Add fractional day contribution.
	ut1 := float64(t.Hour()*3600+t.Minute()*60+t.Second()) +
		float64(t.Nanosecond())*1e-9
	gmstSec += ut1 * 1.00273790935

	// Convert to radians and normalise to [0, 2π).
	return normalise(gmstSec * 2 * math.Pi / 86400)
}

// julianDate computes the Julian Date for t.
func julianDate(t time.Time) float64 {
	// Days since J2000.0 = JD 2451545.0.
	return 2451545.0 + t.Sub(j2000).Hours()/24.0
}

// sunPosition returns a low-precision ECI position vector for the Sun at time t.
// Accurate to ~1°; sufficient for eclipse detection.
func sunPosition(t time.Time) ECIState {
	jd := julianDate(t)
	n := jd - 2451545.0 // days since J2000.0

	// Mean longitude and mean anomaly [deg].
	L := normalise(math.Mod(280.460+0.9856474*n, 360) * math.Pi / 180)
	g := normalise(math.Mod(357.528+0.9856003*n, 360) * math.Pi / 180)

	// Ecliptic longitude [rad].
	lambda := L + 1.915*math.Pi/180*math.Sin(g) + 0.020*math.Pi/180*math.Sin(2*g)

	// Obliquity of ecliptic [rad].
	eps := (23.439 - 0.0000004*n) * math.Pi / 180

	// Sun distance [m].
	dist := (1.000140612 - 0.016708617*math.Cos(g) - 0.000139589*math.Cos(2*g)) * auMetres

	return ECIState{
		X: dist * math.Cos(lambda),
		Y: dist * math.Cos(eps) * math.Sin(lambda),
		Z: dist * math.Sin(eps) * math.Sin(lambda),
	}
}

// normalise wraps an angle to [0, 2π).
func normalise(angle float64) float64 {
	angle = math.Mod(angle, 2*math.Pi)
	if angle < 0 {
		angle += 2 * math.Pi
	}
	return angle
}
