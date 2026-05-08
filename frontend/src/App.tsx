import { useState, useEffect, useRef } from 'react'
import { useSatWebSocket } from './hooks/useSatWebSocket'
import { useOrbitalPosition } from './hooks/useOrbitalPosition'
import { useConstellation } from './hooks/useConstellation'
import { Globe3D } from './components/Globe3D'
import type { RelayHealth } from './components/Globe3D'
import { KPIBar } from './components/KPIBar'
import { TelemetryPanel } from './components/TelemetryPanel'
import { CommandPanel } from './components/CommandPanel'
import { OperatorPanel } from './components/OperatorPanel'
import { MissionFooter } from './components/MissionFooter'
import { useContactWindows } from './hooks/useContactWindows'
import { useCommandEngine, CommandEngineContext } from './hooks/useCommandEngine'
import { useSatCommandState } from './hooks/useSatCommandState'
import { PRIMARY_SATELLITES, primarySatByName } from './data/primarySatellites'
import { GROUND_STATIONS } from './data/groundStations'

const RELAY_POLL_MS = 5_000

// Ordered list of primary satellite IDs for keyboard cycling (J/K, 1/2/3)
const PRIMARY_IDS = PRIMARY_SATELLITES.map(s => s.tleName)

function useRelayHealth(path: string): RelayHealth | null {
  const [health, setHealth] = useState<RelayHealth | null>(null)
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch(path)
        if (!res.ok) {
          setHealth(h => h ? { ...h, online: false } : { online: false, bufferHasData: false, lastFetchAt: null })
        } else {
          const d = await res.json() as { buffer_has_data: boolean; last_fetch_at: string; upstream_node?: string }
          setHealth({ online: true, bufferHasData: d.buffer_has_data, lastFetchAt: d.last_fetch_at, upstreamNode: d.upstream_node })
        }
      } catch {
        setHealth(h => h ? { ...h, online: false } : { online: false, bufferHasData: false, lastFetchAt: null })
      }
      timer = setTimeout(poll, RELAY_POLL_MS)
    }
    poll()
    return () => clearTimeout(timer)
  }, [path])
  return health
}

export default function App() {
  // Three independent WebSocket connections — one per primary spacecraft.
  // satTarget filters by sat_id field once groundstation sends it.
  const sc1 = useSatWebSocket('/ws',              'sc1')
  const sc2 = useSatWebSocket('/spacecraft2/ws',  'sc2')
  const sc3 = useSatWebSocket('/spacecraft3/ws',  'sc3')

  const orbitalPos       = useOrbitalPosition()
  useContactWindows()  // drives delivery-state events for MissionFooter
  const commandEngine    = useCommandEngine()
  const cmdState         = useSatCommandState()
  const constellation = useConstellation()
  const relay1Health = useRelayHealth('/relay1/health')
  // relay2Health reserved for ISL beam coloring
  useRelayHealth('/relay2/health')

  // Selected satellite ID (TLE display name, e.g. "ISS (ZARYA)")
  const [selectedSatId, setSelectedSatId] = useState(PRIMARY_SATELLITES[0].tleName)

  // Resolve which WebSocket state is active based on selection
  const activeSat = primarySatByName(selectedSatId)
  const active = activeSat?.scTarget === 'sc3' ? sc3
               : activeSat?.scTarget === 'sc2' ? sc2
               : sc1

  // Per-satellite connected state for sidebar LIVE/OFFLINE badges
  const satConnected: Record<string, boolean> = {
    [PRIMARY_SATELLITES[0].tleName]: sc1.connected,
    [PRIMARY_SATELLITES[1].tleName]: sc2.connected,
    [PRIMARY_SATELLITES[2].tleName]: sc3.connected,
  }

  // hasTelemetry — true when the selected sat is a primary with a live WS
  const hasTelemetry = !!primarySatByName(selectedSatId)

  // Mission start time for MET (Mission Elapsed Time)
  const missionStartRef = useRef(Date.now())

  // Keyboard navigation: J/K cycle primaries, 1/2/3 direct select
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Don't fire when palette input or any other input is focused
      const tag = (document.activeElement as HTMLElement)?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA') return

      if (e.key === 'j' || e.key === 'J') {
        e.preventDefault()
        setSelectedSatId(id => {
          const idx = PRIMARY_IDS.indexOf(id)
          const next = PRIMARY_IDS[(idx + 1) % PRIMARY_IDS.length]
          window.dispatchEvent(new CustomEvent('select-sat', { detail: next }))
          return next
        })
      } else if (e.key === 'k' || e.key === 'K') {
        e.preventDefault()
        setSelectedSatId(id => {
          const idx = PRIMARY_IDS.indexOf(id)
          const prev = PRIMARY_IDS[(idx - 1 + PRIMARY_IDS.length) % PRIMARY_IDS.length]
          window.dispatchEvent(new CustomEvent('select-sat', { detail: prev }))
          return prev
        })
      } else if (e.key === '1') {
        const id = PRIMARY_IDS[0]
        setSelectedSatId(id)
        window.dispatchEvent(new CustomEvent('select-sat', { detail: id }))
      } else if (e.key === '2') {
        const id = PRIMARY_IDS[1]
        setSelectedSatId(id)
        window.dispatchEvent(new CustomEvent('select-sat', { detail: id }))
      } else if (e.key === '3') {
        const id = PRIMARY_IDS[2]
        setSelectedSatId(id)
        window.dispatchEvent(new CustomEvent('select-sat', { detail: id }))
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  // Listen for globe/sidebar select-sat events
  useEffect(() => {
    const handler = (e: Event) => setSelectedSatId((e as CustomEvent<string>).detail)
    window.addEventListener('select-sat', handler)
    return () => window.removeEventListener('select-sat', handler)
  }, [])

  const selectedSat = constellation.satellites.find(s => s.id === selectedSatId) ?? null

  // Publish RSSI for each spacecraft to the sat-rssi event (consumed by OperatorPanel LinkBars)
  useEffect(() => {
    if (sc1.latest?.satellite?.rssi !== undefined) {
      window.dispatchEvent(new CustomEvent('sat-rssi', { detail: { id: PRIMARY_SATELLITES[0].tleName, rssi: sc1.latest.satellite.rssi } }))
    }
  }, [sc1.latest?.satellite?.rssi])
  useEffect(() => {
    if (sc2.latest?.satellite?.rssi !== undefined) {
      window.dispatchEvent(new CustomEvent('sat-rssi', { detail: { id: PRIMARY_SATELLITES[1].tleName, rssi: sc2.latest.satellite.rssi } }))
    }
  }, [sc2.latest?.satellite?.rssi])
  useEffect(() => {
    if (sc3.latest?.satellite?.rssi !== undefined) {
      window.dispatchEvent(new CustomEvent('sat-rssi', { detail: { id: PRIMARY_SATELLITES[2].tleName, rssi: sc3.latest.satellite.rssi } }))
    }
  }, [sc3.latest?.satellite?.rssi])

  // primarySatId (kept for Globe3D backwards compat) = first primary's TLE name
  const primarySatId = PRIMARY_SATELLITES[0].tleName

  return (
    <CommandEngineContext.Provider value={commandEngine}>
    <>
      <Globe3D
        data={active.latest}
        orbitalPos={orbitalPos}
        constellation={constellation.satellites}
        groundStations={GROUND_STATIONS}
        selectedSatId={selectedSatId}
        onSelectSat={setSelectedSatId}
        relay1Health={relay1Health}
        tleSource={constellation.source}
        primarySatId={primarySatId}
        primarySatIds={PRIMARY_IDS}
        cmdState={cmdState}
      />
      <KPIBar
        metrics={active.latest?.metrics ?? null}
        satellite={active.latest?.satellite ?? null}
        connected={active.connected}
        linkLost={active.linkLost}
        tleSource={constellation.source}
        tleGroup={constellation.group}
        tleCount={constellation.satellites.length}
        selectedSatId={selectedSatId}
        cmdState={cmdState}
      />
      <TelemetryPanel
        state={active.latest?.satellite ?? null}
        selectedSatId={selectedSatId}
        selectedSat={selectedSat}
        hasTelemetry={hasTelemetry}
        tleSource={constellation.source}
        primarySatId={primarySatId}
      />
      <CommandPanel selectedSatId={selectedSatId} />
      <OperatorPanel
        satConnected={satConnected}
        primarySatIds={PRIMARY_IDS}
        tleSource={constellation.source}
        tleGroup={constellation.group}
        cmdState={cmdState}
      />
      <MissionFooter
        connected={active.connected}
        selectedSatId={selectedSatId}
        missionStartMs={missionStartRef.current}
      />
    </>
    </CommandEngineContext.Provider>
  )
}
