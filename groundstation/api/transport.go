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
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/absmach/orbitron/groundstation"
	"github.com/absmach/orbitron/pkg/ccsds/tcframe"
	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

type RouterConfig struct {
	SpacecraftAddr  string
	Spacecraft2Addr string
	Spacecraft3Addr string

	SCID uint16
	VCID uint8

	GSLat float64
	GSLon float64

	StaticDir string

	Relay1Addr string
	Relay2Addr string
	Relay3Addr string
	Relay4Addr string
	Relay5Addr string
	Relay6Addr string

	GS4Addr string
	GS4Lat  float64
	GS4Lon  float64
	GS5Addr string
	GS5Lat  float64
	GS5Lon  float64
	GS6Addr string
	GS6Lat  float64
	GS6Lon  float64
}

type commandForwarder struct {
	cfg    RouterConfig
	seq    atomic.Uint32
	client *http.Client
}

// forward routes a CCSDS TC frame to the specified spacecraft target.
// target: "sc1" (default), "sc2" (Tiangong), "sc3" (Hubble).
func (cf *commandForwarder) forward(ctx context.Context, commandID uint8, payload []byte, target string) (commandFwdRes, error) {
	addr := cf.cfg.SpacecraftAddr
	switch target {
	case "sc2":
		if cf.cfg.Spacecraft2Addr != "" {
			addr = cf.cfg.Spacecraft2Addr
		}
	case "sc3":
		if cf.cfg.Spacecraft3Addr != "" {
			addr = cf.cfg.Spacecraft3Addr
		}
	}
	if addr == "" {
		return commandFwdRes{}, fmt.Errorf("satellite address not configured for target %q", target)
	}

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

	url := addr + "/command"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawTC))
	if err != nil {
		return commandFwdRes{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := cf.client.Do(req)
	if err != nil {
		return commandFwdRes{}, fmt.Errorf("satellite unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return commandFwdRes{}, fmt.Errorf("satellite returned %d: %s", resp.StatusCode, body)
	}

	var res commandFwdRes
	if err := json.Unmarshal(body, &res); err != nil {
		return commandFwdRes{}, fmt.Errorf("decode satellite response: %w", err)
	}
	return res, nil
}

func NewRouter(svc groundstation.Service, cfg RouterConfig) http.Handler {
	fwd := &commandForwarder{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}

	var framesReceived atomic.Int64
	var wsConns atomic.Int64

	topoClient := &http.Client{Timeout: 3 * time.Second}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/health", healthHandler)
	r.Get("/latest", latestHandler(svc))
	r.Post("/receive", func(w http.ResponseWriter, req *http.Request) {
		framesReceived.Add(1)
		receiveHandler(svc)(w, req)
	})
	r.Get("/ws", func(w http.ResponseWriter, req *http.Request) {
		wsConns.Add(1)
		defer wsConns.Add(-1)
		wsHandler(svc)(w, req)
	})
	r.Post("/command", commandHandler(fwd))

	r.Get("/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id":          "Ground Station Nairobi",
			"role":             "groundstation",
			"protocols":        []string{"orbitron-v2", "ccsds-tm"},
			"lat":              cfg.GSLat,
			"lon":              cfg.GSLon,
			"frames_received":  framesReceived.Load(),
			"ws_connections":   wsConns.Load(),
		})
	})

	r.Get("/topology", func(w http.ResponseWriter, req *http.Request) {
		type nodeEntry struct {
			name string
			addr string
		}
		var nodes []nodeEntry
		for _, sc := range []struct{ name, addr string }{
			{"Spacecraft-1 (ISS)",      cfg.SpacecraftAddr},
			{"Spacecraft-2 (Tiangong)", cfg.Spacecraft2Addr},
			{"Spacecraft-3 (Hubble)",   cfg.Spacecraft3Addr},
		} {
			if sc.addr != "" {
				nodes = append(nodes, nodeEntry{sc.name, sc.addr})
			}
		}
		for i, addr := range []string{cfg.Relay1Addr, cfg.Relay2Addr, cfg.Relay3Addr, cfg.Relay4Addr, cfg.Relay5Addr, cfg.Relay6Addr} {
			if addr != "" {
				nodes = append(nodes, nodeEntry{fmt.Sprintf("Relay-%d", i+1), addr})
			}
		}
		for _, gs := range []struct{ name, addr string }{
			{"Ground Station Fairbanks", cfg.GS4Addr},
			{"Ground Station Bangalore", cfg.GS5Addr},
			{"Ground Station Perth", cfg.GS6Addr},
		} {
			if gs.addr != "" {
				nodes = append(nodes, nodeEntry{gs.name, gs.addr})
			}
		}

		results := make([]map[string]any, len(nodes))
		var wg sync.WaitGroup
		for i, n := range nodes {
			wg.Add(1)
			go func(i int, n nodeEntry) {
				defer wg.Done()
				results[i] = map[string]any{"name": n.name}
				capReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, n.addr+"/capabilities", nil)
				if err != nil {
					results[i]["error"] = err.Error()
					return
				}
				resp, err := topoClient.Do(capReq)
				if err != nil {
					results[i]["error"] = err.Error()
					return
				}
				defer resp.Body.Close()
				var cap any
				if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&cap); err != nil {
					results[i]["error"] = err.Error()
					return
				}
				results[i]["capabilities"] = cap
			}(i, n)
		}
		wg.Wait()

		writeJSON(w, http.StatusOK, map[string]any{
			"groundstation": map[string]any{
				"node_id":         "Ground Station Nairobi",
				"role":            "groundstation",
				"protocols":       []string{"orbitron-v2", "ccsds-tm"},
				"lat":             cfg.GSLat,
				"lon":             cfg.GSLon,
				"frames_received": framesReceived.Load(),
				"ws_connections":  wsConns.Load(),
			},
			"nodes": results,
			"ts":    time.Now().UTC(),
		})
	})

	r.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# HELP orbitron_frames_received_total Total Orbitron frames received via POST /receive\n")
		fmt.Fprintf(w, "# TYPE orbitron_frames_received_total counter\n")
		fmt.Fprintf(w, "orbitron_frames_received_total %d\n", framesReceived.Load())
		fmt.Fprintf(w, "# HELP orbitron_ws_connections Current number of active WebSocket subscribers\n")
		fmt.Fprintf(w, "# TYPE orbitron_ws_connections gauge\n")
		fmt.Fprintf(w, "orbitron_ws_connections %d\n", wsConns.Load())
	})

	if cfg.StaticDir != "" {

		if cfg.SpacecraftAddr != "" {
			scProxy := mustReverseProxy(cfg.SpacecraftAddr)
			for _, prefix := range []string{"/windows", "/state", "/track", "/constellation", "/tle", "/events", "/payload", "/inference"} {
				r.Handle(prefix, scProxy)
				r.Handle(prefix+"/*", scProxy)
			}
		}

		if cfg.Spacecraft2Addr != "" {
			sc2Proxy := mustReverseProxyStrip(cfg.Spacecraft2Addr, "/spacecraft2")
			r.Handle("/spacecraft2/*", sc2Proxy)
		}
		if cfg.Spacecraft3Addr != "" {
			sc3Proxy := mustReverseProxyStrip(cfg.Spacecraft3Addr, "/spacecraft3")
			r.Handle("/spacecraft3/*", sc3Proxy)
		}

		// WebSocket paths filtered by scid — all served by this groundstation
		// nginx proxies /spacecraft2/ws → groundstation:8081/ws?scid=sc2
		// nginx proxies /spacecraft3/ws → groundstation:8081/ws?scid=sc3

		for i, addr := range []string{cfg.Relay1Addr, cfg.Relay2Addr, cfg.Relay3Addr, cfg.Relay4Addr, cfg.Relay5Addr, cfg.Relay6Addr} {
			if addr == "" {
				continue
			}
			prefix := fmt.Sprintf("/relay%d", i+1)
			proxy := mustReverseProxyStrip(addr, prefix)
			r.Handle(prefix+"/*", proxy)
		}

		fs := http.FileServer(http.Dir(cfg.StaticDir))
		r.Handle("/*", spaHandler(fs, cfg.StaticDir))
	}

	return r
}

func mustReverseProxy(baseURL string) http.Handler {
	u, err := url.Parse(baseURL)
	if err != nil {
		panic("invalid proxy target: " + baseURL)
	}
	return httputil.NewSingleHostReverseProxy(u)
}

func mustReverseProxyStrip(baseURL, stripPrefix string) http.Handler {
	u, err := url.Parse(baseURL)
	if err != nil {
		panic("invalid proxy target: " + baseURL)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	return http.StripPrefix(stripPrefix, proxy)
}

func spaHandler(fs http.Handler, dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := dir + r.URL.Path
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, r, dir+"/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	}
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
		// X-Source-SC: "sc1" | "sc2" | "sc3" — set by relay when forwarding frames
		sourceSC := r.Header.Get("X-Source-SC")

		tm, err := svc.Receive(r.Context(), body, sourceSC)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}

		if tm.Flags&orbitron.FlagPriority != 0 {
			label := orbitron.InferenceClassName(tm.InferenceClass)
			slog.Warn("[PRIORITY DOWNLINK] anomaly flagged by satellite",
				slog.String("class", label),
				slog.Int("conf_pct", int(tm.InferenceConf)*100/255),
				slog.Uint64("sequence", uint64(tm.Sequence)),
				slog.String("relayed_by", relayedBy),
			)
		}

		res := telemetryFromDomain(tm)
		res.RelayedBy = relayedBy
		writeJSON(w, http.StatusOK, res)
	}
}

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

		// ?target=sc1|sc2|sc3 routes TC frame to the correct spacecraft
		target := r.URL.Query().Get("target")
		if target == "" {
			target = "sc1"
		}

		res, err := fwd.forward(r.Context(), req.CommandID, payload, target)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

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

		// scid filter: if ?scid= query param is set, only forward matching frames
		scidFilter := r.URL.Query().Get("scid")

		for tagged := range ch {
			if scidFilter != "" && tagged.SourceSC != "" && tagged.SourceSC != scidFilter {
				continue
			}
			raw := telemetryFromDomain(tagged.Telemetry)
			raw.SatID = tagged.SourceSC
			msg, err := json.Marshal(raw)
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
	Sequence       uint16 `json:"sequence"`
	Timestamp      uint32 `json:"timestamp"`
	Flags          uint8  `json:"flags"`
	Presence       uint16 `json:"presence"`
	LatE7          int32  `json:"lat_e7,omitempty"`
	LonE7          int32  `json:"lon_e7,omitempty"`
	AltM           int32  `json:"alt_m,omitempty"`
	AttRoll        int16  `json:"att_roll,omitempty"`
	AttPitch       int16  `json:"att_pitch,omitempty"`
	AttYaw         int16  `json:"att_yaw,omitempty"`
	AngVelX        int16  `json:"ang_vel_x,omitempty"`
	AngVelY        int16  `json:"ang_vel_y,omitempty"`
	AngVelZ        int16  `json:"ang_vel_z,omitempty"`
	BatV           uint16 `json:"bat_v,omitempty"`
	SolarV         uint16 `json:"solar_v,omitempty"`
	TempC          int16  `json:"temp_c,omitempty"`
	RSSI           int16  `json:"rssi,omitempty"`
	InferenceClass *uint8 `json:"inference_class,omitempty"`
	InferenceConf  *uint8 `json:"inference_conf,omitempty"`
	InferenceLabel string `json:"inference_label,omitempty"`
	RelayedBy      string `json:"relayed_by,omitempty"`
	SatID          string `json:"sat_id,omitempty"` // "sc1" | "sc2" | "sc3" for frontend multiplex
}

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
