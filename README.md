# Zenith-Link

A software-only distributed space computing stack built from scratch in Go — LEO spacecraft, ISL relay mesh, onboard edge AI, and mission control dashboard, containerized for deployment anywhere.

Demonstrates in-orbit compute orchestration: a primary spacecraft runs a continuous health anomaly detector (statistical, no external dependencies), flags priority downlinks when anomalies are detected, and executes compute jobs on demand. Two relay nodes in staggered orbits form a peer-to-peer ISL mesh, bridging contact gaps with store-and-forward routing. All framed in a compact binary protocol designed for constrained RF links — 78 bytes vs 281 bytes JSON.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  SC-1 (400 km, 51.6°)   ←── TC uplink ───  Ground Station   │
│  · Zenith-Link v2 frames      (Nairobi)     · Nairobi GS     │
│  · CCSDS TM/TC stack     ──► Relay-1/2 ──► · WebSocket stream│
│  · Onboard health AI          (ISL mesh)    · Priority logging│
│  · In-orbit compute jobs                                      │
├─────────────────────────────────┬────────────────────────────┤
│  Relay-1 (SC-2)                 │  Relay-2 (SC-3)            │
│  Sun-sync polar, 700 km, 98°    │  Med-incl, 550 km, 53°     │
│  Store-and-forward to GS        │  Polls Relay-1 first       │
│  SCID=91, port 8082             │  SCID=92, port 8083        │
└─────────────────────────────────┴────────────────────────────┘
```

- **SC-1 spacecraft** — telemetry generation (attitude, power, thermal), Zenith-Link v2 encoding, HMAC-SHA256 auth, CCSDS Space Packet + TM Transfer Frame wrapping, TC uplink command processing, onboard edge AI health detector, in-orbit compute jobs
- **Onboard edge AI** — rolling 30-frame statistical anomaly detector; classes: NOMINAL, POWER_ANOMALY, THERMAL_EVENT, ATTITUDE_INSTABILITY, RF_DEGRADATION, ECLIPSE_ENTRY, ECLIPSE_COMPUTE; sets priority flag in frame header when anomaly detected; pure Go, no external ML dependencies
- **Ground station** — frame receive, HMAC verification, WebSocket broadcast, priority-downlink logging, command forwarder
- **ISL Relay mesh** — two nodes in complementary orbits; Relay-2 checks Relay-1 before polling SC-1 directly (peer routing); both use real Keplerian + J2 orbital mechanics for contact window decisions
- **Mission control** — CesiumJS 3D globe, live 1Hz tracking, onboard AI status in KPI bar, ISL node status indicators, arc-gauge telemetry panel, searchable command palette with COMPUTE category (Ctrl+K)

The orbital propagator uses Keplerian elements with a J2 oblateness correction. Contact windows are computed from elevation masks. All three relay/spacecraft nodes decide independently when to forward — no timers, real orbital geometry only.

---

## Protocol

Zenith-Link v2 is a presence-bitmask binary protocol. A full telemetry frame is ~78 bytes; a delta (position + power only) is ~60 bytes. The JSON equivalent is ~281 bytes — 72% larger.

```
┌─────────────────────────────────────────────────────────┐
│  Magic (2B) │ Ver (1B) │ Seq (2B) │ Timestamp (4B)     │
│  Flags (1B) │ Presence (2B) │ Variable payload          │
│  HMAC-SHA256 (8B truncated)                             │
└─────────────────────────────────────────────────────────┘
```

The presence field is a bitmask — only fields whose bit is set are included in the payload. No field names on the wire. The receiver resolves the schema in O(1) with a bitwise AND.

Run the benchmark:

```bash
go test ./pkg/zenith -bench=. -benchmem -run TestFrameSize
```

The protocol is also implemented in C (`c/`) as a reference. Both implementations share the same test vectors.

---

## Stack

- **Language:** Go 1.22
- **Router:** go-chi/chi
- **WebSocket:** gorilla/websocket
- **Frontend:** React + TypeScript + CesiumJS + Resium (Vite build)
- **Typography:** IBM Plex Mono
- **Deployment:** Docker Compose
- **CCSDS packages:** written from scratch — Space Packet (CCSDS 133.0), TM Transfer Frame (CCSDS 132.0), TC Transfer Frame, CLCW, CRC-16/CCITT-FALSE

---

## Services and ports

| Service       | Port | Role                                                       |
|---------------|----|--------------------------------------------------------------|
| spacecraft    | 8080 | Telemetry source, TC command sink, onboard edge AI         |
| groundstation | 8081 | Frame receiver, WebSocket stream, priority-downlink logger |
| relay         | 8082 | ISL Relay-1 (SC-2) — sun-sync polar, 700 km, 98°           |
| relay2        | 8083 | ISL Relay-2 (SC-3) — med-incl, 550 km, 53°, peer routing  |

The frontend dev server runs on port 5173 (Vite) and proxies `/ws`, `/windows`, `/state`, `/track`, `/command`, `/health`, `/relay/health`, and `/relay2/health` to the appropriate backend service.

---

## Running locally

### With Docker (backend only)

```bash
export ZENITH_HMAC_KEY=your-secret-key-here
docker compose -f docker/docker-compose.yaml up --build
```

Then start the frontend dev server separately:

```bash
cd frontend
npm install
npm run dev
```

Open `http://localhost:5173`. The Vite dev server proxies all API calls to the Docker services.

### Without Docker

You need Go 1.22+ and Node 18+.

```bash
# Terminal 1 — spacecraft
ZENITH_HMAC_KEY=dev-key go run ./cmd/spacecraft

# Terminal 2 — ground station
ZENITH_HMAC_KEY=dev-key SC_ADDR=http://localhost:8080 go run ./cmd/groundstation

# Terminal 3 — ISL Relay-1 (optional)
SC1_ADDR=http://localhost:8080 GS_ADDR=http://localhost:8081 go run ./cmd/relay

# Terminal 4 — ISL Relay-2 (optional, requires Relay-1)
SC1_ADDR=http://localhost:8080 RELAY1_ADDR=http://localhost:8082 GS_ADDR=http://localhost:8081 go run ./cmd/relay2

# Terminal 5 — frontend
cd frontend && npm run dev
```

### Telemetry pusher

The ground station only receives frames when something sends them. The relay forwards during contact windows (every ~90 minutes for the default orbit). For continuous testing, push frames manually:

```bash
while true; do
  curl -s http://localhost:8080/frame/zenith -o /tmp/f.bin
  curl -s -X POST http://localhost:8081/receive \
    -H "Content-Type: application/octet-stream" --data-binary @/tmp/f.bin > /dev/null
  sleep 5
done
```

### Tests

```bash
go test ./... -race -count=1
```

---

## Frontend

The mission control dashboard runs at `http://localhost:5173` when the Vite dev server is active.

**Dashboard layout:**

```
┌─────────────────────────────────────────────────────────────┐
│  KPI bar: packets ACK / NACKs / efficiency / NEXT PASS / LIVE│
├──────────────────────────────────────────┬──────────────────┤
│                                          │                  │
│  CesiumJS 3D globe                       │  Telemetry panel │
│  · Natural Earth II imagery              │  · ATTITUDE      │
│  · 1Hz satellite position update         │  · ANGULAR VEL   │
│  · 90-min forward orbital track          │  · POWER         │
│  · Past ground track history             │  · RF / THERMAL  │
│  · Nairobi / Svalbard / Punta Arenas GS  │  · POSITION      │
│                                          │                  │
├──────────────────────────────────────────┴──────────────────┤
│  UPLINK CMD [Ctrl+K] — searchable command palette           │
└─────────────────────────────────────────────────────────────┘
```

**Command palette** — press `Ctrl+K` to open. Type to filter commands by name, description, or category. Arrow keys to navigate, Enter to transmit. Commands are categorised as MODE / SYSTEM / TELEMETRY / DIAGNOSTIC.

**NEXT PASS tile** — shows time-to-AOS and max elevation for the next contact window. Turns amber when the pass is less than 5 minutes away.

**Cesium Ion** — Earth imagery uses the Natural Earth II texture bundled with CesiumJS (no token required). If you have a Cesium Ion token, set `VITE_CESIUM_TOKEN=your-token` before running the dev server to get high-resolution satellite imagery.

---

## Environment variables

**Spacecraft**

| Variable             | Default               | Description                             |
|----------------------|-----------------------|-----------------------------------------|
| `ZENITH_HMAC_KEY`    | required              | Shared HMAC key                         |
| `SPACECRAFT_ADDR`    | `:8080`               | Listen address                          |
| `SPACECRAFT_SCID`    | `90`                  | 10-bit CCSDS Spacecraft ID              |
| `SPACECRAFT_VCID`    | `0`                   | 6-bit Virtual Channel ID                |
| `SPACECRAFT_APID`    | `256`                 | Telemetry Space Packet APID             |

**Ground station**

| Variable             | Default               | Description                             |
|----------------------|-----------------------|-----------------------------------------|
| `ZENITH_HMAC_KEY`    | required              | Shared HMAC key — must match spacecraft |
| `GROUNDSTATION_ADDR` | `:8081`               | Listen address                          |
| `SC_ADDR`            | required              | Spacecraft base URL for TC forwarding   |
| `GS_LAT` / `GS_LON`  | `-1.2864` / `36.8172` | Ground station coordinates (Nairobi)    |
| `GS_MAX_SUBSCRIBERS` | `64`                  | Max concurrent WebSocket connections    |

**Relay**

| Variable             | Default               | Description                             |
|----------------------|-----------------------|-----------------------------------------|
| `SC1_ADDR`           | required              | Spacecraft base URL                     |
| `GS_ADDR`            | required              | Ground station base URL                 |
| `RELAY_SCID`         | `91`                  | This relay's CCSDS Spacecraft ID        |
| `MIN_ELEV_DEG`       | `5.0`                 | Elevation mask for contact windows      |
| `POLL_INTERVAL_SEC`  | `30`                  | How often to poll the spacecraft        |

---

## API

**Spacecraft** — port 8080

| Method | Path            | Body / notes                              | Response                          |
|--------|-----------------|-------------------------------------------|-----------------------------------|
| GET    | `/health`       |                                           | `{"status":"ok"}`                 |
| GET    | `/telemetry`    |                                           | Decoded telemetry fields as JSON  |
| GET    | `/state`        |                                           | Orbital state — ECI + geodetic    |
| GET    | `/track`        | `?minutes=90&step_s=30`                   | Predicted ground track points     |
| GET    | `/frame`        |                                           | Raw CCSDS TM Transfer Frame       |
| GET    | `/frame/zenith` |                                           | Raw Zenith-Link v2 frame          |
| GET    | `/windows`      |                                           | Computed contact windows for GS   |
| POST   | `/command`      | CCSDS TC Transfer Frame (`octet-stream`)  | `{"accepted":true,...}`           |

**Ground station** — port 8081

| Method | Path       | Body / notes                                                   | Response                         |
|--------|------------|----------------------------------------------------------------|----------------------------------|
| GET    | `/health`  |                                                                | `{"status":"ok"}`                |
| GET    | `/latest`  |                                                                | Latest received telemetry + meta |
| POST   | `/receive` | Raw Zenith-Link frame (`octet-stream`); relay sets `X-Relayed-By` header | Decoded telemetry      |
| GET    | `/ws`      | Upgrades to WebSocket                                          | Stream of telemetry updates      |
| POST   | `/command` | `{"command_id":1,"payload_b64":"..."}` — forwarded to spacecraft as TC frame | `{"accepted":true,...}` |

---

## Known limitations

- Orbital propagation is Keplerian + J2 only. No drag, no solar radiation pressure.
- The relay forwards at most once per contact window pass. Deliberate simplification.
- HMAC is truncated to 8 bytes. Fine for simulation; not a production cryptographic boundary.
- The onboard health detector uses statistical z-scores on simulated telemetry — no real sensor hardware. Anomaly classes are deterministic from the simulated sine-wave signals.
- Attitude data (roll/pitch/yaw) is simulated with slow sine oscillations, not from a real ADCS.
- The frontend is desktop-only. No mobile layout.
