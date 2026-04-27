# Zenith Link

A satellite ground segment stack built from scratch in Go. It simulates an LEO spacecraft downlinking telemetry, a ground station receiving and authenticating it, an inter-satellite relay bridging contact gaps, and a 3D mission control dashboard — all wired together with a compact binary protocol and a full CCSDS implementation.

I built this to understand what a real satellite link actually looks like at the byte level, and why JSON is a terrible idea over a constrained RF downlink.

---

## What it does

- **Spacecraft service** — generates telemetry, encodes it into Zenith-Link v2 binary frames (HMAC-SHA256 authenticated), wraps them in CCSDS Space Packets and TM Transfer Frames, and accepts TC uplink commands
- **Ground station service** — receives frames, verifies HMAC, stores latest telemetry, and streams updates over WebSocket
- **Relay service** — simulates a second satellite in a complementary orbit; polls the spacecraft, buffers the latest frame, and forwards it to the ground station during computed contact windows (store-and-forward ISL)
- **Frontend** — CesiumJS 3D globe showing live satellite position, telemetry panels, and a TC command uplink panel

The orbital propagator uses Keplerian elements with a J2 oblateness correction. Contact windows are computed from elevation masks against a configurable ground station. The relay uses real orbital mechanics to decide when to forward — not a timer.

---

## Protocol

Zenith-Link v2 is a presence-bitmask binary protocol. A full telemetry frame is ~50 bytes; a typical delta is 17–34 bytes. The JSON equivalent is ~300 bytes.

```
┌─────────────────────────────────────────────────────────┐
│  Magic (2B) │ Ver (1B) │ Seq (2B) │ Timestamp (4B)     │
│  Flags (1B) │ Presence (2B) │ Variable payload          │
│  HMAC-SHA256 (8B truncated)                             │
└─────────────────────────────────────────────────────────┘
```

The presence field is a bitmask — only fields whose bit is set are included in the payload. No field names on the wire. The receiver resolves the schema in O(1) with a bitwise AND.

The protocol is also implemented in C (`c/`) as a reference. Both implementations share the same test vectors.

---

## Stack

- **Language:** Go 1.22
- **Router:** go-chi/chi
- **WebSocket:** gorilla/websocket
- **Frontend:** React + TypeScript + CesiumJS (Vite build)
- **Deployment:** Docker Compose
- **CCSDS packages:** written from scratch — Space Packet (CCSDS 133.0), TM Transfer Frame (CCSDS 132.0), TC Transfer Frame, CLCW, CRC-16/CCITT-FALSE

---

## Services and ports

| Service       | Port | Role                                |
|---------------|------|-------------------------------------|
| spacecraft    | 8080 | Telemetry source, TC command sink   |
| groundstation | 8081 | Frame receiver, WebSocket stream    |
| relay         | 8082 | ISL store-and-forward (health only) |

The frontend is static — serve `frontend/dist/` from any web server or open `index.html` directly.

---

## Running locally

### With Docker

```bash
# HMAC_KEY is required — both ends must share it
export ZENITH_HMAC_KEY=your-secret-key-here

cd docker
docker compose up --build
```

Ground station API: `http://localhost:8081`  
Spacecraft API: `http://localhost:8080`

### Without Docker

You need Go 1.22+.

```bash
# Terminal 1 — spacecraft
ZENITH_HMAC_KEY=dev-key go run ./cmd/spacecraft

# Terminal 2 — ground station
ZENITH_HMAC_KEY=dev-key SC_ADDR=http://localhost:8080 go run ./cmd/groundstation

# Terminal 3 — relay (optional)
SC1_ADDR=http://localhost:8080 GS_ADDR=http://localhost:8081 go run ./cmd/relay
```

### Tests

```bash
go test ./... -race -count=1
```

---

## Environment variables

**Spacecraft (`cmd/spacecraft`)**

| Variable             | Default               | Description                             |
|----------------------|-----------------------|-----------------------------------------|
| `ZENITH_HMAC_KEY`    | required              | Shared HMAC key                         |
| `SPACECRAFT_ADDR`    | `:8080`               | Listen address                          |
| `SPACECRAFT_SCID`    | `90`                  | 10-bit CCSDS Spacecraft ID              |
| `SPACECRAFT_VCID`    | `0`                   | 6-bit Virtual Channel ID                |
| `SPACECRAFT_APID`    | `256`                 | Telemetry Space Packet APID             |

**Ground station (`cmd/groundstation`)**

| Variable             | Default               | Description                             |
|----------------------|-----------------------|-----------------------------------------|
| `ZENITH_HMAC_KEY`    | required              | Shared HMAC key — must match spacecraft |
| `GROUNDSTATION_ADDR` | `:8081`               | Listen address                          |
| `SC_ADDR`            | required              | Spacecraft base URL for TC forwarding   |
| `GS_LAT` / `GS_LON`  | `-1.2864` / `36.8172` | Ground station coordinates (Nairobi)    |
| `GS_MAX_SUBSCRIBERS` | `64`                  | Max concurrent WebSocket connections    |

**Relay (`cmd/relay`)**

| Variable             | Default               | Description                             |
|----------------------|-----------------------|-----------------------------------------|
| `SC1_ADDR`           | required              | Spacecraft base URL                     |
| `GS_ADDR`            | required              | Ground station base URL                 |
| `RELAY_SCID`         | `91`                  | This relay's CCSDS Spacecraft ID        |
| `MIN_ELEV_DEG`       | `5.0`                 | Elevation mask for contact windows      |
| `POLL_INTERVAL_SEC`  | `30`                  | How often to poll the spacecraft        |

---

## API — quick reference

**Spacecraft**

```
GET  /health              → {"status":"ok"}
GET  /telemetry           → decoded telemetry JSON
GET  /state               → orbital state (ECI + geodetic)
GET  /frame               → raw CCSDS TM Transfer Frame (binary)
GET  /frame/zenith        → raw Zenith-Link v2 frame (binary)
GET  /windows             → contact windows for a ground station
POST /command             → send TC Transfer Frame (binary body)
```

**Ground station**

```
GET  /health              → {"status":"ok"}
GET  /latest              → latest received telemetry
POST /receive             → ingest a raw Zenith-Link frame
GET  /ws                  → WebSocket stream of telemetry updates
POST /command             → forward TC command to spacecraft
```

---

## Known limitations

- Orbital propagation is Keplerian + J2 only. No drag, no solar radiation pressure. Good enough for short-term contact window estimates, not for station-keeping.
- The relay forwards at most once per minute during a contact window, even if the window is longer. This was a deliberate simplification.
- HMAC is truncated to 8 bytes. Fine for a simulation; not a production cryptographic boundary.
- The AI inference is a stub — it cycles through seven hardcoded class names to simulate earth observation results. There is no actual model.
- The frontend assumes the ground station WebSocket is at `ws://localhost:8081/ws`. There is no configuration UI.

---

## Project layout

```
cmd/
  spacecraft/     — entrypoint
  groundstation/  — entrypoint
  relay/          — entrypoint
spacecraft/       — service implementation + API
groundstation/    — service implementation + API
pkg/
  ccsds/          — Space Packet, TM frame, TC frame, CLCW, CRC
  zenith/         — Zenith-Link v2 encode/decode
  orbital/        — propagator, ECI/ECEF, contact windows
  errors/         — sentinel errors + wrapping
c/                — C reference implementation
frontend/         — React + CesiumJS dashboard
docker/           — Dockerfiles + compose
```

---

## Author

Built by [Jeff Mboya](https://github.com/JeffMboya)
