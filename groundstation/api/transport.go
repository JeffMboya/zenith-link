// Package api provides the HTTP and WebSocket transport for the groundstation service.
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/absmach/zenith-link/groundstation"
	"github.com/absmach/zenith-link/pkg/ccsds/tcframe"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// RouterConfig holds transport-level configuration for the groundstation router.
type RouterConfig struct {
	// SpacecraftAddr is the base URL of the spacecraft service (e.g. "http://spacecraft:8080").
	// Required for command forwarding. If empty, POST /command returns 503.
	SpacecraftAddr string
	// SCID is the spacecraft's 10-bit CCSDS ID — embedded in forwarded TC frames.
	SCID uint16
	// VCID is the spacecraft's 6-bit virtual channel ID.
	VCID uint8
	// GSLat is the ground station geodetic latitude [degrees] used in log output.
	GSLat float64
	// GSLon is the ground station geodetic longitude [degrees] used in log output.
	GSLon float64
}

// commandForwarder encodes JSON commands into TC Transfer Frames and POSTs
// them to the spacecraft's /command endpoint.
type commandForwarder struct {
	cfg    RouterConfig
	seq    atomic.Uint32 // modulo-256 TC sequence counter
	client *http.Client
}

func (cf *commandForwarder) forward(ctx context.Context, commandID uint8, payload []byte) (commandFwdRes, error) {
	if cf.cfg.SpacecraftAddr == "" {
		return commandFwdRes{}, fmt.Errorf("spacecraft address not configured")
	}

	// Build TC data field: [commandID, ...payload].
	data := make([]byte, 1+len(payload))
	data[0] = commandID
	copy(data[1:], payload)

	seq := uint8(cf.seq.Add(1) % 256)

	frame := tcframe.TransferFrame{
		Primary: tcframe.PrimaryHeader{
			SCID:                cf.cfg.SCID,
			VCID:                cf.cfg.VCID,
			FrameSequenceNumber: seq,
		},
		DataField: data,
	}
	rawTC, err := tcframe.Encode(frame)
	if err != nil {
		return commandFwdRes{}, fmt.Errorf("TC encode: %w", err)
	}

	url := cf.cfg.SpacecraftAddr + "/command"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawTC))
	if err != nil {
		return commandFwdRes{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := cf.client.Do(req)
	if err != nil {
		return commandFwdRes{}, fmt.Errorf("spacecraft unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return commandFwdRes{}, fmt.Errorf("spacecraft returned %d: %s", resp.StatusCode, body)
	}

	var res commandFwdRes
	if err := json.Unmarshal(body, &res); err != nil {
		return commandFwdRes{}, fmt.Errorf("decode spacecraft response: %w", err)
	}
	return res, nil
}

// NewRouter builds and returns the chi router for the groundstation service.
func NewRouter(svc groundstation.Service, cfg RouterConfig) http.Handler {
	fwd := &commandForwarder{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler)
	r.Get("/latest", latestHandler(svc))
	r.Post("/receive", receiveHandler(svc))
	r.Get("/ws", wsHandler(svc))
	r.Post("/command", commandHandler(fwd))

	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func latestHandler(svc groundstation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := svc.Latest(r.Context())
		if err != nil {
			if errors.Is(err, groundstation.ErrNoData) {
				writeJSON(w, http.StatusNoContent, nil)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, latestRes{
			Telemetry:  telemetryFromDomain(st.Telemetry),
			ReceivedAt: st.ReceivedAt,
			Frame:      st.FrameNumber,
		})
	}
}

func receiveHandler(svc groundstation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		relayedBy := r.Header.Get("X-Relayed-By")

		tm, err := svc.Receive(r.Context(), body)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}

		res := telemetryFromDomain(tm)
		res.RelayedBy = relayedBy
		writeJSON(w, http.StatusOK, res)
	}
}

// commandHandler accepts a JSON command request, builds a TC Transfer Frame,
// and forwards it to the spacecraft service.
//
// Request body:
//
//	{
//	  "command_id": 1,
//	  "payload_b64": "<base64-encoded bytes, optional>"
//	}
func commandHandler(fwd *commandForwarder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req commandReq
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		var payload []byte
		if req.PayloadB64 != "" {
			var err error
			payload, err = base64.StdEncoding.DecodeString(req.PayloadB64)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("invalid payload_b64: %w", err))
				return
			}
		}

		res, err := fwd.forward(r.Context(), req.CommandID, payload)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// wsHandler upgrades an HTTP connection to WebSocket and streams telemetry
// updates to the client until the connection is closed.
func wsHandler(svc groundstation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		ctx := r.Context()
		ch, err := svc.Subscribe(ctx)
		if err != nil {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
			return
		}

		for tm := range ch {
			msg, err := json.Marshal(telemetryFromDomain(tm))
			if err != nil {
				break
			}
			if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				break
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				break
			}
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, code int, err error) {
	msg := "internal error"
	if err != nil {
		msg = err.Error()
	}
	writeJSON(w, code, map[string]string{"error": msg})
}

// ─── request / response types ─────────────────────────────────────────────────

type commandReq struct {
	CommandID  uint8  `json:"command_id"`
	PayloadB64 string `json:"payload_b64,omitempty"`
}

type commandFwdRes struct {
	CommandID uint8  `json:"command_id"`
	Accepted  bool   `json:"accepted"`
	Message   string `json:"message"`
}

type latestRes struct {
	Telemetry  telemetryRes `json:"telemetry"`
	ReceivedAt time.Time    `json:"received_at"`
	Frame      uint64       `json:"frame_number"`
}

type telemetryRes struct {
	Sequence       uint16  `json:"sequence"`
	Timestamp      uint32  `json:"timestamp"`
	Flags          uint8   `json:"flags"`
	Presence       uint16  `json:"presence"`
	LatE7          int32   `json:"lat_e7,omitempty"`
	LonE7          int32   `json:"lon_e7,omitempty"`
	AltM           int32   `json:"alt_m,omitempty"`
	BatV           uint16  `json:"bat_v,omitempty"`
	SolarV         uint16  `json:"solar_v,omitempty"`
	TempC          int16   `json:"temp_c,omitempty"`
	RSSI           int16   `json:"rssi,omitempty"`
	InferenceClass *uint8  `json:"inference_class,omitempty"`
	InferenceConf  *uint8  `json:"inference_conf,omitempty"`
	InferenceLabel string  `json:"inference_label,omitempty"`
	RelayedBy      string  `json:"relayed_by,omitempty"`
}

func telemetryFromDomain(tm zenith.Telemetry) telemetryRes {
	r := telemetryRes{
		Sequence:  tm.Sequence,
		Timestamp: tm.Timestamp,
		Flags:     tm.Flags,
		Presence:  tm.Presence,
		LatE7:     tm.LatE7,
		LonE7:     tm.LonE7,
		AltM:      tm.AltM,
		BatV:      tm.BatV,
		SolarV:    tm.SolarV,
		TempC:     tm.TempC,
		RSSI:      tm.RSSI,
	}
	if tm.Presence&zenith.PresenceInference != 0 {
		c := tm.InferenceClass
		cf := tm.InferenceConf
		r.InferenceClass = &c
		r.InferenceConf = &cf
		r.InferenceLabel = zenith.InferenceClassName(c)
	}
	return r
}
