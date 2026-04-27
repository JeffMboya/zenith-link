import { useEffect, useRef, useState } from 'react'

export interface TrackPoint { lat: number; lon: number; alt_m: number }

// Fetches the predicted forward ground track from /track every 60 seconds.
export function useForwardTrack(): TrackPoint[] {
  const [track, setTrack] = useState<TrackPoint[]>([])
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    let active = true

    async function fetch_() {
      try {
        const res = await fetch('/track?minutes=90&step_s=30')
        if (!res.ok) return
        const d = await res.json() as { points: TrackPoint[] }
        if (active && d.points?.length) setTrack(d.points)
      } catch { /* keep last */ }
      if (active) timer.current = setTimeout(fetch_, 60_000)
    }

    fetch_()
    return () => {
      active = false
      if (timer.current) clearTimeout(timer.current)
    }
  }, [])

  return track
}
