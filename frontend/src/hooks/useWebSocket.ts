import { useEffect, useRef, useState, useCallback } from 'react'
import type { StateUpdate } from '../types'

const RECONNECT_DELAY_MS = 2000

export function useWebSocket() {
  const [latest, setLatest] = useState<StateUpdate | null>(null)
  const [connected, setConnected] = useState(false)
  const ws = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const connect = useCallback(() => {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${window.location.host}/ws`
    const socket = new WebSocket(url)

    socket.onopen = () => setConnected(true)

    socket.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as StateUpdate
        setLatest(msg)
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
