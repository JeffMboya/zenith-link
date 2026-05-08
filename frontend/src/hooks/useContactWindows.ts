import { useEffect, useRef, useState } from 'react'
import { GROUND_STATIONS } from '../data/groundStations'

const POLL_MS = 30_000

const RELAYS = [
  { path: '/relay1', name: 'NOAA-20' },
  { path: '/relay2', name: 'Sentinel-2A' },
  { path: '/relay3', name: 'Landsat-9' },
  { path: '/relay4', name: 'Aqua' },
  { path: '/relay5', name: 'Terra' },
  { path: '/relay6', name: 'NOAA-19' },
]

export interface ContactWindow {
  aos:              string
  los:              string
  duration_sec:     number
  max_elevation_deg: number
}

export interface GSWindows {
  gsName:    string
  relay:     string
  windows:   ContactWindow[]
  inContact: boolean
}

export interface DeliveryState {
  aosMs:    number | null
  durationMs: number
  relay:    string
  gs:       string
  flowing:  boolean
}

export interface ContactWindowsState {
  byGS:       Record<string, GSWindows[]>  // gsName → array of per-relay windows
  delivery:   DeliveryState | null         // earliest upcoming delivery (for KPIBar)
  lastUpdated: number
}

const EMPTY: ContactWindowsState = { byGS: {}, delivery: null, lastUpdated: 0 }

export function useContactWindows(): ContactWindowsState {
  const [state, setState] = useState<ContactWindowsState>(EMPTY)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    async function poll() {
      const combos = RELAYS.flatMap(relay =>
        GROUND_STATIONS.map(gs => ({
          relay: relay.name,
          gs:    gs.name,
          url:   `${relay.path}/windows?gs_lat=${gs.lat}&gs_lon=${gs.lon}`,
        }))
      )

      const results = await Promise.allSettled(
        combos.map(c => fetch(c.url).then(r => r.ok ? r.json() : Promise.reject()))
      )

      // Build byGS map and find earliest delivery
      const byGS: Record<string, GSWindows[]> = {}
      GROUND_STATIONS.forEach(gs => { byGS[gs.name] = [] })

      let earliest: DeliveryState | null = null

      results.forEach((r, i) => {
        if (r.status !== 'fulfilled') return
        const data = r.value as { windows: ContactWindow[]; in_contact: boolean }
        const { relay, gs } = combos[i]

        byGS[gs] = byGS[gs] ?? []
        byGS[gs].push({
          gsName:    gs,
          relay,
          windows:   data.windows ?? [],
          inContact: data.in_contact ?? false,
        })

        // Compute delivery state
        if (data.in_contact) {
          if (!earliest?.flowing) {
            earliest = { aosMs: null, durationMs: 0, relay, gs, flowing: true }
          }
          return
        }
        if (!data.windows?.length) return

        const aosMs  = new Date(data.windows[0].aos).getTime()
        const losMs  = new Date(data.windows[0].los).getTime()
        if (!earliest || earliest.flowing) return
        if (earliest.aosMs === null || aosMs < earliest.aosMs) {
          if (!earliest.flowing) {
            earliest = { aosMs, durationMs: losMs - aosMs, relay, gs, flowing: false }
          }
        }
      })

      const newState: ContactWindowsState = { byGS, delivery: earliest, lastUpdated: Date.now() }
      setState(newState)

      // Publish delivery state for MissionFooter
      if (earliest) {
        window.dispatchEvent(new CustomEvent('delivery-state', { detail: earliest }))
      }
    }

    poll()
    timer.current = setInterval(poll, POLL_MS)
    return () => {
      if (timer.current) clearInterval(timer.current)
    }
  }, [])

  return state
}
