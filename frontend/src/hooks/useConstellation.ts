import { useEffect, useRef, useState } from 'react'

export interface ConstellationSat {
  id: string
  lat: number
  lon: number
  alt_m: number
  plane: string       // 'A'|'B'|'C'|'D' for simulated; group name for TLE
  source: 'sim' | 'tle'
}

export interface ConstellationState {
  satellites: ConstellationSat[]
  source: 'sim' | 'tle'
  group: string       // '' for sim, e.g. 'stations' | 'starlink' for TLE
}

const EMPTY: ConstellationState = { satellites: [], source: 'sim', group: '' }

// Polls /constellation at 1Hz.
export function useConstellation(): ConstellationState {
  const [state, setState] = useState<ConstellationState>(EMPTY)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let active = true

    async function tick() {
      try {
        const res = await fetch('/constellation')
        if (!res.ok) return
        const d = await res.json() as { satellites: ConstellationSat[]; source: 'sim' | 'tle'; group: string }
        if (active && d.satellites?.length) {
          setState({ satellites: d.satellites, source: d.source, group: d.group })
        }
      } catch { /* keep last */ }
      if (active) timer.current = setTimeout(tick, 1000)
    }

    tick()
    return () => {
      active = false
      if (timer.current) clearTimeout(timer.current)
    }
  }, [])

  return state
}
