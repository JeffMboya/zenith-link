#!/bin/sh
# Production entrypoint for Fly.io single-machine deployment.
# Starts all four Zenith-Link services on localhost, then keeps the container
# alive until a SIGTERM arrives.
set -e

: "${ZENITH_HMAC_KEY:?ZENITH_HMAC_KEY must be set}"

# SC-1 primary spacecraft (port 8090 internally; GS proxies all /spacecraft/* paths)
SPACECRAFT_ADDR=":8090" \
SPACECRAFT_SCID="${SPACECRAFT_SCID:-90}" \
SPACECRAFT_VCID="${SPACECRAFT_VCID:-0}" \
SPACECRAFT_APID="${SPACECRAFT_APID:-256}" \
ZENITH_HMAC_KEY="$ZENITH_HMAC_KEY" \
  spacecraft &

# ISL Relay-1 (SC-2) — polls spacecraft on localhost:8090
RELAY_ADDR=":8082" \
SC1_ADDR="http://localhost:8090" \
GS_ADDR="http://localhost:8080" \
RELAY_SCID="${RELAY_SCID:-91}" \
GS_LAT="${GS_LAT:--1.2864}" \
GS_LON="${GS_LON:-36.8172}" \
MIN_ELEV_DEG="${MIN_ELEV_DEG:-5.0}" \
POLL_INTERVAL_SEC="${POLL_INTERVAL_SEC:-30}" \
  relay &

# ISL Relay-2 (SC-3) — peers with relay-1
RELAY2_ADDR=":8083" \
SC1_ADDR="http://localhost:8090" \
RELAY1_ADDR="http://localhost:8082" \
GS_ADDR="http://localhost:8080" \
RELAY2_SCID="${RELAY2_SCID:-92}" \
GS_LAT="${GS_LAT:--1.2864}" \
GS_LON="${GS_LON:-36.8172}" \
MIN_ELEV_DEG="${MIN_ELEV_DEG:-5.0}" \
POLL_INTERVAL_SEC="${POLL_INTERVAL_SEC:-30}" \
  relay2 &

# Wait briefly for upstream services to bind before groundstation starts proxying.
sleep 2

# Groundstation — the public entry point; serves frontend + proxies all upstreams.
GROUNDSTATION_ADDR=":8080" \
SC_ADDR="http://localhost:8090" \
SC_SCID="${SPACECRAFT_SCID:-90}" \
SC_VCID="${SPACECRAFT_VCID:-0}" \
GS_LAT="${GS_LAT:--1.2864}" \
GS_LON="${GS_LON:-36.8172}" \
RELAY1_ADDR="http://localhost:8082" \
RELAY2_ADDR="http://localhost:8083" \
STATIC_DIR="/app/dist" \
ZENITH_HMAC_KEY="$ZENITH_HMAC_KEY" \
  groundstation &

# Wait for any child to exit (error condition); propagate SIGTERM to all.
wait
