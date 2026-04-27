import { useEffect, useRef, useState, useCallback } from 'react'
import type { StateUpdate } from '../types'

const RECONNECT_DELAY_MS = 2000
const MAX_TRACK_POINTS = 200

// Shape the ground station WS actually sends
interface RawTelemetry {
  sequence: number
  timestamp: number
  lat_e7: number
  lon_e7: number
  alt_m: number
  bat_v: number
  solar_v: number
  temp_c: number
  rssi: number
}

function toStateUpdate(raw: RawTelemetry, track: { lat: number; lon: number; alt: number }[]): StateUpdate {
  return {
    type: 'state_update',
    ts: raw.timestamp,
    satellite: {
      pitch: 0,
      yaw: 0,
      roll: 0,
      omega_x: 0,
      omega_y: 0,
      omega_z: 0,
      latitude: raw.lat_e7 / 1e7,
      longitude: raw.lon_e7 / 1e7,
      altitude: raw.alt_m,
      battery_voltage: raw.bat_v / 1000,
      solar_voltage: raw.solar_v / 1000,
      chassis_temp: raw.temp_c / 100,
      cpu_temp: 0,
      rssi: raw.rssi / 10,
    },
    metrics: {
      packets_received: raw.sequence,
      packets_dropped_simulated: 0,
      nacks_issued: 0,
      acks_issued: raw.sequence,
      full_syncs_received: 0,
      bytes_received_zenith: raw.sequence * 70,
      bytes_equivalent_json: raw.sequence * 300,
    },
    ground_track: track,
    ground_stations: [
      { name: 'Nairobi', lat: -1.2864, lon: 36.8172, inView: false },
    ],
  }
}

export function useWebSocket() {
  const [latest, setLatest] = useState<StateUpdate | null>(null)
  const [connected, setConnected] = useState(false)
  const ws = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const trackRef = useRef<{ lat: number; lon: number; alt: number }[]>([])

  const connect = useCallback(() => {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${window.location.host}/ws`
    const socket = new WebSocket(url)

    socket.onopen = () => setConnected(true)

    socket.onmessage = (e) => {
      try {
        const raw = JSON.parse(e.data) as RawTelemetry
        if (typeof raw.sequence !== 'number') return
        const pt = { lat: raw.lat_e7 / 1e7, lon: raw.lon_e7 / 1e7, alt: raw.alt_m }
        trackRef.current = [...trackRef.current.slice(-(MAX_TRACK_POINTS - 1)), pt]
        setLatest(toStateUpdate(raw, trackRef.current))
      } catch {
        // malformed frame — ignore
      }
    }

    socket.onclose = () => {
      setConnected(false)
      reconnectTimer.current = setTimeout(connect, RECONNECT_DELAY_MS)
    }

    socket.onerror = () => socket.close()

    ws.current = socket
  }, [])

  useEffect(() => {
    connect()
    return () => {
      reconnectTimer.current && clearTimeout(reconnectTimer.current)
      ws.current?.close()
    }
  }, [connect])

  return { latest, connected }
}
