// Package api provides the HTTP transport for the spacecraft service.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/absmach/orbitron/pkg/ccsds/tmframe"
	"github.com/absmach/orbitron/spacecraft"
	"github.com/absmach/orbitron/spacecraft/inference"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(svc spacecraft.Service) http.Handler {
	var framesServed atomic.Int64

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler)
	r.Get("/telemetry", telemetryHandler(svc))
	r.Get("/state", stateHandler(svc))
	r.Get("/frame", tmFrameHandler(svc))
	r.Get("/frame/orbitron", func(w http.ResponseWriter, req *http.Request) {
		framesServed.Add(1)
		orbitronFrameHandler(svc)(w, req)
	})
	r.Post("/command", commandHandler(svc))
	r.Get("/windows", windowsHandler(svc))
	r.Get("/track", trackHandler(svc))
	r.Get("/constellation", constellationHandler())
	r.Post("/tle/import", tleImportHandler())
	r.Get("/tle/status", tleStatusHandler())
	r.Post("/tle/clear", tleClearHandler())
	r.Get("/events", eventsHandler(svc))
	r.Get("/payload", payloadHandler(svc))
	r.Get("/inference/state", inferenceStateHandler(svc))

	r.Get("/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		nodeName := os.Getenv("SPACECRAFT_NAME")
		if nodeName == "" {
			nodeName = os.Getenv("SPACECRAFT_SCID")
		}
		if nodeName == "" {
			nodeName = "Spacecraft"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id":  nodeName,
			"role":     "spacecraft",
			"protocols": []string{"orbitron-v2", "ccsds-tm", "ccsds-sp", "ccsds-tc"},
			"features": []string{
				"telemetry_generation",
				"onboard_inference",
				"command_uplink",
				"eclipse_detection",
				"autonomous_mode",
				"payload_deploy",
				"link_budget_compute",
				"space_weather_monitoring",
			},
			"inference_classes": []string{
				"NOMINAL", "POWER_ANOMALY", "THERMAL_EVENT",
				"ATTITUDE_INSTABILITY", "RF_DEGRADATION",
				"ECLIPSE_ENTRY", "ECLIPSE_COMPUTE", "UNKNOWN",
			},
			"bandwidth_bps": 20480,
		})
	})

	r.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# HELP orbitron_frames_served_total Orbitron frames encoded and served to downstream nodes\n")
		fmt.Fprintf(w, "# TYPE orbitron_frames_served_total counter\n")
		fmt.Fprintf(w, "orbitron_frames_served_total %d\n", framesServed.Load())
	})

	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func telemetryHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := queryTime(r)
		tm, err := svc.Telemetry(r.Context(), t)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, telemetryFromDomain(tm))
	}
}

func stateHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := queryTime(r)
		st, err := svc.State(r.Context(), t)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, stateFromDomain(st))
	}
}

func tmFrameHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := queryTime(r)
		frameSize := 512
		if s := r.URL.Query().Get("size"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if n < 9 || n > tmframe.MaxFrameSize {
				writeError(w, http.StatusBadRequest, fmt.Errorf("size must be between 9 and %d", tmframe.MaxFrameSize))
				return
			}
			frameSize = n
		}
		frame, err := svc.TMFrame(r.Context(), t, frameSize)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(frame)
	}
}

func orbitronFrameHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := queryTime(r)
		b, err := svc.TelemetryFrame(r.Context(), t)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}
}

func commandHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		res, err := svc.ExecuteCommand(r.Context(), body)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, commandFromDomain(res))
	}
}

func windowsHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := windowsReq{
			GSLat:      -1.2864,
			GSLon:      36.8172,
			HoursAhead: 4,
			MinElevDeg: 5.0,
		}
		if s := r.URL.Query().Get("gs_lat"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				req.GSLat = v
			}
		}
		if s := r.URL.Query().Get("gs_lon"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				req.GSLon = v
			}
		}
		if s := r.URL.Query().Get("hours"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				req.HoursAhead = v
			}
		}
		if s := r.URL.Query().Get("min_elev"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				req.MinElevDeg = v
			}
		}
		if err := req.validate(); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		now := time.Now().UTC()
		end := now.Add(time.Duration(req.HoursAhead * float64(time.Hour)))

		ws, err := svc.Windows(r.Context(), req.GSLat, req.GSLon, now, end, req.MinElevDeg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, contactWindowsFromDomain(ws))
	}
}

func trackHandler(svc spacecraft.Service) http.HandlerFunc {
	type trackPoint struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
		Alt float64 `json:"alt_m"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		minutes := 90.0
		stepSec := 30.0
		if s := r.URL.Query().Get("minutes"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 && v <= 200 {
				minutes = v
			}
		}
		if s := r.URL.Query().Get("step_s"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil && v >= 10 {
				stepSec = v
			}
		}
		now := time.Now().UTC()
		end := now.Add(time.Duration(minutes * float64(time.Minute)))
		step := time.Duration(stepSec * float64(time.Second))

		var points []trackPoint
		for t := now; !t.After(end); t = t.Add(step) {
			st, err := svc.State(r.Context(), t)
			if err != nil {
				continue
			}
			points = append(points, trackPoint{
				Lat: st.Geodetic.LatitudeDeg,
				Lon: st.Geodetic.LongitudeDeg,
				Alt: st.Geodetic.AltitudeM,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"points": points, "step_s": stepSec})
	}
}

func eventsHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, svc.Events())
	}
}

func payloadHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := svc.PayloadState()
		if p == nil {
			writeJSON(w, http.StatusNoContent, nil)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

func inferenceStateHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res := svc.LastResult()
		writeJSON(w, http.StatusOK, map[string]any{
			"class":       uint8(res.Class),
			"class_label": inference.ClassName(res.Class),
			"confidence":  res.Confidence,
			"channels": map[string]any{
				"bat_v":     map[string]any{"z": res.Channels.BatV.Z, "mean": res.Channels.BatV.Mean, "std": res.Channels.BatV.Std},
				"chassis_c": map[string]any{"z": res.Channels.ChassisC.Z, "mean": res.Channels.ChassisC.Mean, "std": res.Channels.ChassisC.Std},
				"rssi":      map[string]any{"z": res.Channels.RSSI.Z, "mean": res.Channels.RSSI.Mean, "std": res.Channels.RSSI.Std},
				"att_vel":   map[string]any{"z": res.Channels.AttVel.Z, "mean": res.Channels.AttVel.Mean, "std": res.Channels.AttVel.Std},
			},
			"pre_fault_class": res.PreFaultClass,
			"pre_fault_chan":  res.PreFaultChan,
			"storm_level":     svc.StormLevel(),
			"kp_index":        svc.KpIndex(),
		})
	}
}

func queryTime(r *http.Request) time.Time {
	if s := r.URL.Query().Get("t"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Now().UTC()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	msg := "internal error"
	if err != nil {
		msg = err.Error()
	}
	writeJSON(w, code, errorRes{Error: msg})
}
