import { useWebSocket } from './hooks/useWebSocket'
import { useOrbitalPosition } from './hooks/useOrbitalPosition'
import { Globe3D } from './components/Globe3D'
import { KPIBar } from './components/KPIBar'
import { TelemetryPanel } from './components/TelemetryPanel'
import { CommandPanel } from './components/CommandPanel'

export default function App() {
  const { latest, connected } = useWebSocket()
  const orbitalPos = useOrbitalPosition()

  return (
    <>
      <Globe3D data={latest} orbitalPos={orbitalPos} />
      <KPIBar metrics={latest?.metrics ?? null} connected={connected} />
      <TelemetryPanel state={latest?.satellite ?? null} />
      <CommandPanel />
    </>
  )
}
