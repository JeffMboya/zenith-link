// Contact window computation for satellite–ground-station visibility.
//
// A contact window opens when a satellite rises above a minimum elevation
// angle as seen from a ground station, and closes when it sets below that
// angle again. Elevation is computed via the standard ENU (East-North-Up)
// topocentric transform from ECEF coordinates.
package orbital

import (
	"math"
	"time"

	"github.com/absmach/zenith-link/pkg/errors"
)

const contactSampleStep = 10 * time.Second

type ContactWindow struct {
	AOS time.Time

	LOS time.Time

	MaxElevationDeg float64
}

func (cw ContactWindow) Duration() time.Duration {
	return cw.LOS.Sub(cw.AOS)
}

func ElevationAzimuth(satECEF ECIState, gsLat, gsLon float64) (elevRad, azRad float64) {
	lat := gsLat * math.Pi / 180
	lon := gsLon * math.Pi / 180

	gsECEF := geodeticToECEF(lat, lon, 0)

	dx := satECEF.X - gsECEF.X
	dy := satECEF.Y - gsECEF.Y
	dz := satECEF.Z - gsECEF.Z

	sinLat, cosLat := math.Sin(lat), math.Cos(lat)
	sinLon, cosLon := math.Sin(lon), math.Cos(lon)

	east := -sinLon*dx + cosLon*dy
	north := -sinLat*cosLon*dx - sinLat*sinLon*dy + cosLat*dz
	up := cosLat*cosLon*dx + cosLat*sinLon*dy + sinLat*dz

	elevRad = math.Atan2(up, math.Sqrt(east*east+north*north))
	azRad = math.Atan2(east, north)
	return
}

func ContactWindows(elem Elements, gsLat, gsLon float64, start, end time.Time, minElevDeg float64) ([]ContactWindow, error) {
	if err := elem.Validate(); err != nil {
		return nil, err
	}
	if end.Before(start) || end.Equal(start) {
		return nil, errors.Wrap(errors.ErrInvalidField, errors.New("end must be after start"))
	}
	if minElevDeg < 0 || minElevDeg >= 90 {
		return nil, errors.Wrap(errors.ErrInvalidField, errors.New("minElevDeg must be in [0, 90)"))
	}
	if err := validateGSCoords(gsLat, gsLon); err != nil {
		return nil, err
	}

	minElevRad := minElevDeg * math.Pi / 180

	var windows []ContactWindow
	var inContact bool
	var current ContactWindow
	var maxElev float64

	for t := start; !t.After(end); t = t.Add(contactSampleStep) {
		eci, err := Propagate(elem, t)
		if err != nil {
			return nil, err
		}
		ecef := ECIToECEF(eci, t)
		elev, _ := ElevationAzimuth(ecef, gsLat, gsLon)

		switch {
		case !inContact && elev >= minElevRad:
			inContact = true
			current = ContactWindow{AOS: t}
			maxElev = elev

		case inContact && elev < minElevRad:
			inContact = false
			current.LOS = t
			current.MaxElevationDeg = maxElev * 180 / math.Pi
			windows = append(windows, current)
			maxElev = 0

		case inContact && elev > maxElev:
			maxElev = elev
		}
	}

	if inContact {
		current.LOS = end
		current.MaxElevationDeg = maxElev * 180 / math.Pi
		windows = append(windows, current)
	}

	return windows, nil
}

func IsInContact(elem Elements, gsLat, gsLon float64, t time.Time, minElevDeg float64) (bool, error) {
	if err := elem.Validate(); err != nil {
		return false, err
	}
	if err := validateGSCoords(gsLat, gsLon); err != nil {
		return false, err
	}
	if minElevDeg < 0 || minElevDeg >= 90 {
		return false, errors.Wrap(errors.ErrInvalidField, errors.New("minElevDeg must be in [0, 90)"))
	}
	eci, err := Propagate(elem, t)
	if err != nil {
		return false, err
	}
	ecef := ECIToECEF(eci, t)
	elev, _ := ElevationAzimuth(ecef, gsLat, gsLon)
	return elev >= minElevDeg*math.Pi/180, nil
}

func validateGSCoords(gsLat, gsLon float64) error {
	if gsLat < -90 || gsLat > 90 {
		return errors.Wrap(errors.ErrInvalidField, errors.New("gsLat must be in [-90, 90]"))
	}
	if gsLon < -180 || gsLon > 180 {
		return errors.Wrap(errors.ErrInvalidField, errors.New("gsLon must be in [-180, 180]"))
	}
	return nil
}

func geodeticToECEF(lat, lon, altM float64) ECIState {
	f := earthFlattening
	e2 := 2*f - f*f
	sinLat := math.Sin(lat)
	cosLat := math.Cos(lat)
	N := earthRadiusM / math.Sqrt(1-e2*sinLat*sinLat)

	return ECIState{
		X: (N + altM) * cosLat * math.Cos(lon),
		Y: (N + altM) * cosLat * math.Sin(lon),
		Z: (N*(1-e2) + altM) * sinLat,
	}
}
