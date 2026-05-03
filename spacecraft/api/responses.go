package api

import (
	"net/http"
	"time"

	"github.com/absmach/orbitron/pkg/orbital"
	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/absmach/orbitron/spacecraft"
)

type telemetryRes struct {
	Sequence  uint16 `json:"sequence"`
	Timestamp uint32 `json:"timestamp"`
	Flags     uint8  `json:"flags"`
	Presence  uint16 `json:"presence"`
	LatE7     int32  `json:"lat_e7,omitempty"`
	LonE7     int32  `json:"lon_e7,omitempty"`
	AltM      int32  `json:"alt_m,omitempty"`
	AttRoll   int16  `json:"att_roll,omitempty"`
	AttPitch  int16  `json:"att_pitch,omitempty"`
	AttYaw    int16  `json:"att_yaw,omitempty"`
	AngVelX   int16  `json:"ang_vel_x,omitempty"`
	AngVelY   int16  `json:"ang_vel_y,omitempty"`
	AngVelZ   int16  `json:"ang_vel_z,omitempty"`
	BatV      uint16 `json:"bat_v,omitempty"`
	SolarV    uint16 `json:"solar_v,omitempty"`
	TempC     int16  `json:"temp_c,omitempty"`
	RSSI      int16  `json:"rssi,omitempty"`

	InferenceClass *uint8 `json:"inference_class,omitempty"`
	InferenceConf  *uint8 `json:"inference_conf,omitempty"`
	InferenceLabel string `json:"inference_label,omitempty"`
}

func (r telemetryRes) Code() int { return http.StatusOK }

func telemetryFromDomain(tm orbitron.Telemetry) telemetryRes {
	r := telemetryRes{
		Sequence:  tm.Sequence,
		Timestamp: tm.Timestamp,
		Flags:     tm.Flags,
		Presence:  tm.Presence,
		LatE7:     tm.LatE7,
		LonE7:     tm.LonE7,
		AltM:      tm.AltM,
		AttRoll:   tm.AttRoll,
		AttPitch:  tm.AttPitch,
		AttYaw:    tm.AttYaw,
		AngVelX:   tm.AngVelX,
		AngVelY:   tm.AngVelY,
		AngVelZ:   tm.AngVelZ,
		BatV:      tm.BatV,
		SolarV:    tm.SolarV,
		TempC:     tm.TempC,
		RSSI:      tm.RSSI,
	}
	if tm.Presence&orbitron.PresenceInference != 0 {
		c := tm.InferenceClass
		cf := tm.InferenceConf
		r.InferenceClass = &c
		r.InferenceConf = &cf
		r.InferenceLabel = orbitron.InferenceClassName(c)
	}
	return r
}

type stateRes struct {
	Time       time.Time `json:"time"`
	ECIX       float64   `json:"eci_x"`
	ECIY       float64   `json:"eci_y"`
	ECIZ       float64   `json:"eci_z"`
	LatDeg     float64   `json:"lat_deg"`
	LonDeg     float64   `json:"lon_deg"`
	AltM       float64   `json:"alt_m"`
	InSunlight bool      `json:"in_sunlight"`
}

func (r stateRes) Code() int { return http.StatusOK }

func stateFromDomain(s spacecraft.State) stateRes {
	return stateRes{
		Time:       s.Time,
		ECIX:       s.ECI.X,
		ECIY:       s.ECI.Y,
		ECIZ:       s.ECI.Z,
		LatDeg:     s.Geodetic.LatitudeDeg,
		LonDeg:     s.Geodetic.LongitudeDeg,
		AltM:       s.Geodetic.AltitudeM,
		InSunlight: s.InSunlight,
	}
}

type commandRes struct {
	CommandID uint8  `json:"command_id"`
	Accepted  bool   `json:"accepted"`
	Message   string `json:"message"`
}

func commandFromDomain(r spacecraft.CommandResult) commandRes {
	return commandRes{
		CommandID: uint8(r.CommandID),
		Accepted:  r.Accepted,
		Message:   r.Message,
	}
}

type contactWindowRes struct {
	AOS             time.Time `json:"aos"`
	LOS             time.Time `json:"los"`
	DurationSec     float64   `json:"duration_sec"`
	MaxElevationDeg float64   `json:"max_elevation_deg"`
}

type contactWindowsRes struct {
	Count   int                `json:"count"`
	Windows []contactWindowRes `json:"windows"`
}

func contactWindowsFromDomain(ws []orbital.ContactWindow) contactWindowsRes {
	out := make([]contactWindowRes, len(ws))
	for i, w := range ws {
		out[i] = contactWindowRes{
			AOS:             w.AOS,
			LOS:             w.LOS,
			DurationSec:     w.Duration().Seconds(),
			MaxElevationDeg: w.MaxElevationDeg,
		}
	}
	return contactWindowsRes{Count: len(out), Windows: out}
}

type errorRes struct {
	Error string `json:"error"`
}
