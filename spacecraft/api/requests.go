package api

import (
	"time"

	"github.com/absmach/orbitron/pkg/errors"
)

type telemetryReq struct {
	At time.Time
}

func (r telemetryReq) validate() error {
	if r.At.IsZero() {
		return errors.Wrap(errors.ErrInvalidField, errors.New("time must be set"))
	}
	return nil
}

type tmFrameReq struct {
	At        time.Time
	FrameSize int
}

func (r tmFrameReq) validate() error {
	if r.At.IsZero() {
		return errors.Wrap(errors.ErrInvalidField, errors.New("time must be set"))
	}
	if r.FrameSize < 9 {
		return errors.Wrap(errors.ErrFrameTooSmall, errors.New("frame_size must be at least 9"))
	}
	return nil
}

type windowsReq struct {
	GSLat      float64
	GSLon      float64
	HoursAhead float64
	MinElevDeg float64
}

func (r windowsReq) validate() error {
	if r.GSLat < -90 || r.GSLat > 90 {
		return errors.Wrap(errors.ErrInvalidField, errors.New("gs_lat must be in [-90, 90]"))
	}
	if r.GSLon < -180 || r.GSLon > 180 {
		return errors.Wrap(errors.ErrInvalidField, errors.New("gs_lon must be in [-180, 180]"))
	}
	if r.HoursAhead <= 0 || r.HoursAhead > 48 {
		return errors.Wrap(errors.ErrInvalidField, errors.New("hours must be in (0, 48]"))
	}
	if r.MinElevDeg < 0 || r.MinElevDeg >= 90 {
		return errors.Wrap(errors.ErrInvalidField, errors.New("min_elev must be in [0, 90)"))
	}
	return nil
}
