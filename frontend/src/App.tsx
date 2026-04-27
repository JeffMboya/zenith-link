import { useState, useEffect } from 'react'
import { useWebSocket } from './hooks/useWebSocket'
import { useOrbitalPosition } from './hooks/useOrbitalPosition'
import { useConstellation } from './hooks/useConstellation'
import { Globe3D } from './components/Globe3D'
import { KPIBar } from './components/KPIBar'
import { TelemetryPanel } from './components/TelemetryPanel'
import { CommandPanel } from './components/CommandPanel'

export default function App() {
  const { latest, connected, linkLost } = useWebSocket()
  const orbitalPos = useOrbitalPosition()
  const constellation = useConstellation()

  // AT-1 is the default selected satellite (primary spacecraft with full telemetry)
  const [selectedSatId, setSelectedSatId] = useState('AT-1')

  // Panel dot-buttons dispatch this event; Globe3D onClick calls setSelectedSatId directly
  useEffect(() => {
    const handler = (e: Event) => setSelectedSatId((e as CustomEvent<string>).detail)
    window.addEventListener('select-sat', handler)
    return () => window.removeEventListener('select-sat', handler)
  }, [])
  const selectedSat = constellation.find(s => s.id === selectedSatId) ?? null

  return (
    <>
      <Globe3D
        data={latest}
        orbitalPos={orbitalPos}
        constellation={constellation}
        selectedSatId={selectedSatId}
        onSelectSat={setSelectedSatId}
      />
      <KPIBar metrics={latest?.metrics ?? null} connected={connected} linkLost={linkLost} />
      <TelemetryPanel
        state={latest?.satellite ?? null}
        selectedSatId={selectedSatId}
        selectedSat={selectedSat}
      />
      <CommandPanel />
    </>
  )
}
