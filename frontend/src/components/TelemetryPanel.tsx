import { useState, useEffect } from 'react'
import type { SatelliteState, DeployedPayload } from '../types'
import type { ConstellationSat } from '../hooks/useConstellation'
import { ArcGauge } from './ArcGauge'
import { primarySatByName } from '../data/primarySatellites'

function usePayload() {
  const [payload, setPayload] = useState<DeployedPayload | null>(null)
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/payload')
        if (res.status === 204) { setPayload(null) }
        else if (res.ok) { setPayload(await res.json()) }
      } catch {  }
      timer = setTimeout(poll, 5000)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])
  return payload
}

interface Props {
  state: SatelliteState | null
  selectedSatId: string
  selectedSat: ConstellationSat | null
  hasTelemetry: boolean
  tleSource: 'sim' | 'tle'
  primarySatId: string
}

function Row({ label, value, unit, color = 'var(--text-body)' }: {
  label: string; value: string; unit: string; color?: string
}) {
  return (
    <div style={{
      display: 'flex', justifyContent: 'space-between', alignItems: 'baseline',
      padding: '3px 0', borderBottom: '1px solid var(--bg-dark)',
    }}>
      <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>{label}</span>
      <span style={{ color, fontSize: 12, fontWeight: 700 }}>
        {value}<span style={{ color: 'var(--text-dim)', fontSize: 9, marginLeft: 3 }}>{unit}</span>
      </span>
    </div>
  )
}

function Section({ title, accent = 'var(--cyan-dim)', children }: {
  title: string; accent?: string; children: React.ReactNode
}) {
  return (
    <div style={{ marginBottom: 12 }}>
      <div style={{
        color: accent, fontSize: 9, letterSpacing: 3, marginBottom: 6,
        paddingBottom: 4, borderBottom: '1px solid var(--border)',
        borderLeft: `2px solid ${accent}`, paddingLeft: 6,
      }}>
        {title}
      </div>
      {children}
    </div>
  )
}

export function TelemetryPanel({ state, selectedSatId, selectedSat, hasTelemetry, tleSource, primarySatId }: Props) {
  const [open, setOpen] = useState(true)
  const payload = usePayload()

  useEffect(() => {
    const h = (e: Event) => {
      const section = (e as CustomEvent<string>).detail
      if (section === 'telemetry' || section === 'ai') setOpen(true)
    }
    window.addEventListener('nav-section', h)
    return () => window.removeEventListener('nav-section', h)
  }, [])
  const meta = primarySatByName(selectedSatId)
  const planeColor = 'var(--cyan)'

  const battColor = state && state.battery_voltage > 7.8
    ? 'var(--green)'
    : state && state.battery_voltage > 7.0 ? 'var(--amber)' : 'var(--red)'
  const rssiColor = state && state.rssi > -90
    ? 'var(--green)'
    : state && state.rssi > -110 ? 'var(--amber)' : 'var(--red)'

  return (
    <div style={{
      position: 'fixed', right: 0, top: 48, bottom: 0, width: open ? 220 : 32,
      background: 'rgba(4,13,28,0.90)', borderLeft: '1px solid var(--border)',
      backdropFilter: 'blur(6px)', zIndex: 90,
      transition: 'width 0.2s ease', overflow: 'hidden',
      display: 'flex', flexDirection: 'column',
    }}>
      <button
        onClick={() => setOpen(o => !o)}
        aria-label={open ? 'Collapse telemetry panel' : 'Expand telemetry panel'}
        style={{
          position: 'absolute', left: -20, top: 60, width: 20, height: 48,
          background: 'var(--bg-deep)', border: '1px solid var(--border)', borderRight: 'none',
          color: 'var(--text-mid)', cursor: 'pointer', fontSize: 10,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          borderRadius: '4px 0 0 4px',
        }}
      >
        {open ? '›' : '‹'}
      </button>

      {open && (
        <div style={{ padding: '12px 12px', flex: 1, overflowY: 'auto' }}>

          
          <div style={{ marginBottom: 12 }}>
            <div style={{
              color: planeColor, fontSize: 13, fontWeight: 700, letterSpacing: 1,
              marginBottom: 3,
            }}>
              {selectedSatId}
            </div>
            {meta && (
              <div style={{ color: 'var(--text-dim)', fontSize: 8, marginTop: 2 }}>
                LEO · {meta.altKm} km · {meta.incDeg}° · TLE
              </div>
            )}
          </div>

          
          {hasTelemetry && (
            <>
              {tleSource === 'sim' && (
                <div style={{ color: 'var(--cyan)', fontSize: 9, letterSpacing: 3, marginBottom: 12 }}>
                  TELEMETRY · GROUND MIRROR
                </div>
              )}
              {!state ? (
                <div style={{ color: 'var(--text-dim)', fontSize: 10, marginTop: 8, textAlign: 'center' }}>
                  AWAITING FIRST FRAME...
                </div>
              ) : (
                <>
                  
                  <Section
                    title={tleSource === 'tle' ? 'REAL POSITION' : 'POSITION'}
                    accent={tleSource === 'tle' ? 'var(--green)' : 'var(--cyan)'}
                  >
                    <Row label="LAT" value={(selectedSat?.lat ?? state.latitude).toFixed(4)}                             unit="°"  color={tleSource === 'tle' ? 'var(--green)' : 'var(--text-body)'} />
                    <Row label="LON" value={(selectedSat?.lon ?? state.longitude).toFixed(4)}                             unit="°"  color={tleSource === 'tle' ? 'var(--green)' : 'var(--text-body)'} />
                    <Row label="ALT" value={((selectedSat?.alt_m ?? state.altitude) / 1000).toFixed(1)} unit="km" color={tleSource === 'tle' ? 'var(--green)' : 'var(--cyan)'} />
                  </Section>

                  
                  {tleSource === 'tle' && (
                    <div style={{
                      margin: '4px 0 8px',
                      paddingLeft: 6,
                      borderLeft: '2px solid var(--amber)',
                      color: 'var(--amber)',
                      fontSize: 9, letterSpacing: 3,
                    }}>
                      SIMULATED TELEMETRY
                    </div>
                  )}

                  <Section title="ATTITUDE">
                    <div style={{ display: 'flex', justifyContent: 'space-between', flexWrap: 'wrap', gap: 4 }}>
                      <ArcGauge value={state.pitch}  min={-30}  max={30}   label="PITCH" unit="°"   color="var(--cyan)"   size={68} />
                      <ArcGauge value={state.yaw}    min={-180} max={180}  label="YAW"   unit="°"   color="var(--purple)" size={68} />
                      <ArcGauge value={state.roll}   min={-15}  max={15}   label="ROLL"  unit="°"   color="var(--teal)"   size={68} />
                    </div>
                  </Section>
                  <Section title="ANGULAR VELOCITY">
                    <Row label="ω_X" value={state.omega_x.toFixed(4)} unit="rad/s" />
                    <Row label="ω_Y" value={state.omega_y.toFixed(4)} unit="rad/s" />
                    <Row label="ω_Z" value={state.omega_z.toFixed(4)} unit="rad/s" />
                  </Section>
                  <Section title="POWER">
                    <div style={{ display: 'flex', justifyContent: 'space-around', marginBottom: 4 }}>
                      <ArcGauge value={state.battery_voltage} min={6.0} max={8.4} label="BATTERY" unit="V"   color={battColor} size={80} />
                      <ArcGauge value={state.solar_voltage}   min={0}   max={8}   label="SOLAR"   unit="V"   color={state.solar_voltage > 4 ? 'var(--green)' : 'var(--amber)'} size={80} />
                    </div>
                  </Section>
                  <Section title="RF / THERMAL">
                    <ArcGauge value={state.rssi} min={-130} max={-60} label="RSSI" unit="dBm" color={rssiColor} size={80} />
                    <div style={{ marginTop: 8 }}>
                      <Row label="CHASSIS" value={state.chassis_temp.toFixed(1)} unit="°C" color={state.chassis_temp > 50 ? 'var(--amber)' : 'var(--text-body)'} />
                      <Row label="CPU"     value={state.cpu_temp.toFixed(1)}     unit="°C" color={state.cpu_temp > 70 ? 'var(--red)' : 'var(--text-body)'} />
                    </div>
                  </Section>
                  {payload && (
                    <Section title="PAYLOAD" accent="var(--teal)">
                      <Row label="NAME"   value={payload.Name}                                           unit="" color="var(--teal)" />
                      <Row label="SIZE"   value={(payload.SizeBytes / 1024).toFixed(1)}                  unit="KB" />
                      <Row label="STATUS" value={payload.Status}                                         unit="" color={payload.Status === 'RUNNING' ? 'var(--green)' : 'var(--red)'} />
                      <Row label="DEPLOY" value={new Date(payload.DeployedAt).toISOString().slice(11,19)} unit="Z" />
                    </Section>
                  )}
                </>
              )}
            </>
          )}

          
          {!hasTelemetry && (
            <>
              <div style={{ color: planeColor, fontSize: 9, letterSpacing: 3, marginBottom: 12 }}>
                ORBITAL DATA
              </div>
              {selectedSat ? (
                <>
                  <Section title="CURRENT POSITION" accent={planeColor}>
                    <Row label="LAT" value={selectedSat.lat.toFixed(4)}              unit="°"  color={planeColor} />
                    <Row label="LON" value={selectedSat.lon.toFixed(4)}              unit="°"  color={planeColor} />
                    <Row label="ALT" value={(selectedSat.alt_m / 1000).toFixed(1)}   unit="km" color={planeColor} />
                  </Section>
                  {meta && (
                    <Section title="ORBIT" accent={planeColor}>
                      <Row label="INCLINATION" value={meta.incDeg.toFixed(1)}   unit="°" />
                      <Row label="ALTITUDE"    value={meta.altKm.toFixed(0)}    unit="km" />
                      <Row label="NORAD ID"    value={String(meta.noradId)}     unit="" color={planeColor} />
                    </Section>
                  )}
                  <div style={{
                    marginTop: 16, padding: '8px 10px',
                    background: 'var(--bg-deep)', borderRadius: 3,
                    border: '1px solid var(--border)',
                  }}>
                    <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2, marginBottom: 4 }}>
                      TELEMETRY
                    </div>
                    <div style={{ color: 'var(--text-mid)', fontSize: 9 }}>
                      Position-only — no direct link to this satellite.
                    </div>
                    <button
                      onClick={() => {
                        const evt = new CustomEvent('select-sat', { detail: primarySatId })
                        window.dispatchEvent(evt)
                      }}
                      style={{
                        marginTop: 8, width: '100%', padding: '4px 0',
                        background: 'var(--bg-dark)', border: '1px solid var(--cyan)',
                        color: 'var(--cyan)', fontSize: 9, letterSpacing: 1,
                        cursor: 'pointer', borderRadius: 2,
                      }}
                    >
                      SWITCH TO PRIMARY TELEMETRY
                    </button>
                  </div>
                </>
              ) : (
                <div style={{ color: 'var(--text-dim)', fontSize: 10, marginTop: 20, textAlign: 'center' }}>
                  AWAITING POSITION...
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}
