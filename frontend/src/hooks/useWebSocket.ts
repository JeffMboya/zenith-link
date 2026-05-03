import { useEffect, useRef, useState, useCallback } from 'react'
import type { StateUpdate } from '../types'

const RECONNECT_DELAY_MS = 2000
const MAX_TRACK_POINTS = 200

interface RawTelemetry {
  sequence: number
  timestamp: number
  lat_e7: number
  lon_e7: number
  alt_m: number
  att_roll?: number
  att_pitch?: number
  att_yaw?: number
  ang_vel_x?: number
  ang_vel_y?: number
  ang_vel_z?: number
  bat_v: number
  solar_v: number
  temp_c: number
  rssi: number
  flags?: number
  inference_class?: number
  inference_conf?: number
  inference_label?: string
}

function toStateUpdate(
  raw: RawTelemetry,
  track: { lat: number; lon: number; alt: number }[],
  orbitronBytes: number,
  jsonBytes: number,
  packets: number,
  nacks: number,
): StateUpdate {
  return {
    type: 'state_update',
    ts: raw.timestamp,
    satellite: {
      pitch:   (raw.att_pitch  ?? 0) / 100,   
      yaw:     (raw.att_yaw    ?? 0) / 100,
      roll:    (raw.att_roll   ?? 0) / 100,
      omega_x: (raw.ang_vel_x  ?? 0) / 100,   
      omega_y: (raw.ang_vel_y  ?? 0) / 100,
      omega_z: (raw.ang_vel_z  ?? 0) / 100,
      latitude: raw.lat_e7 / 1e7,
      longitude: raw.lon_e7 / 1e7,
      altitude: raw.alt_m,
      battery_voltage: raw.bat_v / 1000,
      solar_voltage: raw.solar_v / 1000,
      chassis_temp: raw.temp_c / 100,
      cpu_temp: raw.temp_c / 100 + 8,          
      rssi: raw.rssi / 10,
      flags: raw.flags,
      inference_class: raw.inference_class,
      inference_conf: raw.inference_conf,
      inference_label: raw.inference_label,
    },
    metrics: {
      packets_received: packets,
      packets_dropped_simulated: 0,
      nacks_issued: nacks,
      acks_issued: packets,
      full_syncs_received: 0,
      bytes_received_orbitron: orbitronBytes,
      bytes_equivalent_json: jsonBytes,
    },
    ground_track: track,
    ground_stations: [
      { name: 'Nairobi',       lat: -1.2864, lon:  36.8172, inView: false },
      { name: 'Svalbard',      lat: 78.2297, lon:  15.3975, inView: false },
      { name: 'Punta Arenas',  lat: -53.163, lon: -70.9171, inView: false },
    ],
  }
}

const ORBITRON_FRAME_BYTES = 78  

export function useWebSocket() {
  const [latest, setLatest] = useState<StateUpdate | null>(null)
  const [connected, setConnected] = useState(false)
  const [linkLost, setLinkLost] = useState(false)
  const ws = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const trackRef = useRef<{ lat: number; lon: number; alt: number }[]>([])
  const everConnected = useRef(false)
  const orbitronBytesRef = useRef(0)
  const jsonBytesRef = useRef(0)
  const packetsRef = useRef(0)
  const nacksRef = useRef(0)

  const connect = useCallback(() => {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${window.location.host}/ws`
    const socket = new WebSocket(url)

    socket.onopen = () => {
      setConnected(true)
      setLinkLost(false)
      everConnected.current = true
    }

    socket.onmessage = (e) => {
      try {
        const raw = JSON.parse(e.data) as RawTelemetry
        if (typeof raw.sequence !== 'number') return
        orbitronBytesRef.current += ORBITRON_FRAME_BYTES
        jsonBytesRef.current += e.data.length  
        packetsRef.current += 1
        const pt = { lat: raw.lat_e7 / 1e7, lon: raw.lon_e7 / 1e7, alt: raw.alt_m }
        trackRef.current = [...trackRef.current.slice(-(MAX_TRACK_POINTS - 1)), pt]
        setLatest(toStateUpdate(raw, trackRef.current, orbitronBytesRef.current, jsonBytesRef.current, packetsRef.current, nacksRef.current))
      } catch {
        
      }
    }

    socket.onclose = () => {
      setConnected(false)
      if (everConnected.current) setLinkLost(true)
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

  return { latest, connected, linkLost }
}
