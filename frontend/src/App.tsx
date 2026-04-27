import { useWebSocket } from './hooks/useWebSocket'
import { Globe3D } from './components/Globe3D'
import { KPIBar } from './components/KPIBar'
import { TelemetryPanel } from './components/TelemetryPanel'
import { CommandPanel } from './components/CommandPanel'

export default function App() {
  const { latest, connected } = useWebSocket()

  return (
    <>
      {/* 3D globe fills the entire viewport — rendered first so overlays sit on top */}
      <Globe3D data={latest} />

      {/* Overlays */}
      <KPIBar metrics={latest?.metrics ?? null} connected={connected} />
      <TelemetryPanel state={latest?.satellite ?? null} />
      <CommandPanel />
    </>
  )
}
