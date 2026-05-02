import { useState, useEffect } from 'react'
import { useWebSocket } from './hooks/useWebSocket'
import { useOrbitalPosition } from './hooks/useOrbitalPosition'
import { useConstellation } from './hooks/useConstellation'
import { Globe3D } from './components/Globe3D'
import type { RelayHealth } from './components/Globe3D'
import { KPIBar } from './components/KPIBar'
import { TelemetryPanel } from './components/TelemetryPanel'
import { CommandPanel } from './components/CommandPanel'
import { OperatorPanel } from './components/OperatorPanel'

const RELAY_POLL_MS = 5_000

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
  const { latest, connected, linkLost } = useWebSocket()
  const orbitalPos = useOrbitalPosition()
  const constellation = useConstellation()
  const relay1Health = useRelayHealth('/relay/health')
  const relay2Health = useRelayHealth('/relay2/health')

  // AT-1 is the default selected satellite (primary spacecraft with full telemetry)
  const [selectedSatId, setSelectedSatId] = useState('AT-1')

  // Panel dot-buttons dispatch this event; Globe3D onClick calls setSelectedSatId directly
  useEffect(() => {
    const handler = (e: Event) => setSelectedSatId((e as CustomEvent<string>).detail)
    window.addEventListener('select-sat', handler)
    return () => window.removeEventListener('select-sat', handler)
  }, [])
  const selectedSat = constellation.satellites.find(s => s.id === selectedSatId) ?? null

  return (
    <>
      <Globe3D
        data={latest}
        orbitalPos={orbitalPos}
        constellation={constellation.satellites}
        selectedSatId={selectedSatId}
        onSelectSat={setSelectedSatId}
        relay1Health={relay1Health}
        relay2Health={relay2Health}
      />
      <KPIBar
        metrics={latest?.metrics ?? null}
        satellite={latest?.satellite ?? null}
        connected={connected}
        linkLost={linkLost}
        tleSource={constellation.source}
        tleGroup={constellation.group}
        tleCount={constellation.satellites.length}
      />
      <TelemetryPanel
        state={latest?.satellite ?? null}
        selectedSatId={selectedSatId}
        selectedSat={selectedSat}
      />
      <CommandPanel />
      <OperatorPanel primaryOnline={connected} />
    </>
  )
}
