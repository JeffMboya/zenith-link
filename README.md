# Project Zenith-Link

High-fidelity simulation of an asymmetric satellite-to-ground data link with sovereign binary protocol, stateful delta-compression, and an aerospace-grade mission control dashboard.

## Quick Start

```bash
cd zenith-link
docker compose up --build
```

Open `http://localhost:8501` in your browser.

The ground station API is available at `http://localhost:8000`.

## Services

| Service | Port | Description |
|---|---|---|
| `ground-station` | 8000 | FastAPI receiver — unpacks binary frames, issues ACK/NACK |
| `satellite-node` | — | Transmits telemetry at 1 Hz using the Zenith-Link protocol |
| `dashboard` | 8501 | Streamlit mission control UI |

## Local Development (no Docker)

```bash
pip install -r requirements.txt

# Terminal 1 — ground station
python ground_station_api.py

# Terminal 2 — satellite node
GROUND_URL=http://localhost:8000 python satellite_node.py

# Terminal 3 — dashboard
GROUND_URL=http://localhost:8000 streamlit run dashboard.py
```

## Protocol Summary

```
┌──────────────────────────────────────────────────────────────┐
│  ZENITH-LINK FRAME (big-endian)                              │
├────────┬──────┬──────┬─────────────────┬───────┬──────────── │
│ MAGIC  │ VER  │ SEQ  │  TIMESTAMP (µs) │ FLAGS │  PRESENCE  │
│ 0x5A4C │ u8   │ u32  │      u64        │  u8   │    u8      │
│ 2 B    │ 1 B  │ 4 B  │      8 B        │  1 B  │    1 B     │
├────────┴──────┴──────┴─────────────────┴───────┴────────────┤
│  PAYLOAD — only fields whose PRESENCE bit is set             │
│  Attitude (6B) | Angular vel (6B) | Position (12B) | ...    │
└──────────────────────────────────────────────────────────────┘

Full sync:  51 bytes
Typical delta:  17–34 bytes
JSON equivalent:  ~300 bytes
```

## Architecture Inspiration

See `ENGINEERING_LOG.md` for the full design rationale, including the Nakuja Project and Octavia Carbon references.
