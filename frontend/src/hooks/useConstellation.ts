import { useEffect, useRef, useState } from 'react'

export interface ConstellationSat {
  id: string
  lat: number
  lon: number
  alt_m: number
  plane: 'A' | 'B' | 'C' | 'D'
}

// Polls /constellation at 1Hz. Returns all 16 satellite positions.
export function useConstellation(): ConstellationSat[] {
  const [sats, setSats] = useState<ConstellationSat[]>([])
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let active = true

    async function tick() {
      try {
        const res = await fetch('/constellation')
        if (!res.ok) return
        const d = await res.json() as { satellites: ConstellationSat[] }
        if (active && d.satellites?.length) setSats(d.satellites)
      } catch { /* keep last */ }
      if (active) timer.current = setTimeout(tick, 1000)
    }

    tick()
    return () => {
      active = false
      if (timer.current) clearTimeout(timer.current)
    }
  }, [])

  return sats
}
