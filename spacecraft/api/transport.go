// Package api provides the HTTP transport for the spacecraft service.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/absmach/zenith-link/pkg/ccsds/tmframe"
	"github.com/absmach/zenith-link/spacecraft"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds and returns the chi router for the spacecraft service.
func NewRouter(svc spacecraft.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler)
	r.Get("/telemetry", telemetryHandler(svc))
	r.Get("/state", stateHandler(svc))
	r.Get("/frame", tmFrameHandler(svc))
	r.Get("/frame/zenith", zenithFrameHandler(svc))
	r.Post("/command", commandHandler(svc))
	r.Get("/windows", windowsHandler(svc))

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

// zenithFrameHandler returns the raw Zenith-Link v2 binary frame (no CCSDS
// wrapping). Used by relay satellites to fetch and forward telemetry.
func zenithFrameHandler(svc spacecraft.Service) http.HandlerFunc {
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

// commandHandler accepts a raw CCSDS TC Transfer Frame (application/octet-stream)
// and executes the embedded command.
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

// windowsHandler returns contact windows for a configurable ground station.
//
// Query parameters:
//
//	gs_lat    geodetic latitude  [degrees, default -1.2864 Nairobi]
//	gs_lon    geodetic longitude [degrees, default 36.8172 Nairobi]
//	hours     look-ahead window  [hours,   default 4,     max 48]
//	min_elev  minimum elevation  [degrees, default 5.0]
func windowsHandler(svc spacecraft.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := windowsReq{
			GSLat:      -1.2864, // Nairobi default
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

// queryTime reads an optional "t" Unix-seconds query parameter; defaults to now.
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
