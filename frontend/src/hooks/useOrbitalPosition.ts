import { useEffect, useRef, useState } from 'react'

export interface OrbitalPos {
  lat: number
  lon: number
  alt: number
  inSunlight: boolean
}

export function useOrbitalPosition(): OrbitalPos | null {
  const [pos, setPos] = useState<OrbitalPos | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let active = true

    async function tick() {
      try {
        const res = await fetch('/state')
        if (!res.ok) return
        const d = await res.json() as { lat_deg: number; lon_deg: number; alt_m: number; in_sunlight: boolean }
        if (active) setPos({ lat: d.lat_deg, lon: d.lon_deg, alt: d.alt_m, inSunlight: d.in_sunlight })
      } catch {  }
      if (active) timer.current = setTimeout(tick, 1000)
    }

    tick()
    return () => {
      active = false
      if (timer.current) clearTimeout(timer.current)
    }
  }, [])

  return pos
}
