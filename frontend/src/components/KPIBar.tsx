import { useEffect, useState, useRef } from 'react'
import type { LinkMetrics, SatelliteState } from '../types'
import { TleSelector } from './TleSelector'

const WINDOWS_POLL_MS = 30_000
const INFERENCE_POLL_MS = 2_000

interface Props {
  metrics: LinkMetrics | null
  satellite: SatelliteState | null
  connected: boolean
  linkLost: boolean
  tleSource: 'sim' | 'tle'
  tleGroup: string
  tleCount: number
}

interface ContactWindow {
  aos: string
  duration_sec: number
  max_elevation_deg: number
}

interface NextPassWindow { aosMs: number; durationMs: number; elevDeg: number }

function useNextPass() {
  const [window, setWindow] = useState<NextPassWindow | null>(null)
  const [label, setLabel] = useState<string>('—')
  const [color, setColor] = useState('var(--text-dim)')
  const [urgent, setUrgent] = useState(false)

  
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/windows')
        if (!res.ok) return
        const data = await res.json() as { windows: ContactWindow[] }
        if (data.windows?.length) {
          const w = data.windows[0]
          setWindow({ aosMs: new Date(w.aos).getTime(), durationMs: w.duration_sec * 1000, elevDeg: w.max_elevation_deg })
        } else {
          setWindow(null)
        }
      } catch {  }
      timer = setTimeout(poll, WINDOWS_POLL_MS)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])

  
  useEffect(() => {
    function tick() {
      if (!window) { setLabel('—'); setColor('var(--text-dim)'); setUrgent(false); return }
      const now = Date.now()
      const diffSec = Math.round((window.aosMs - now) / 1000)
      if (diffSec < 0) {
        const losSec = Math.round((window.aosMs + window.durationMs - now) / 1000)
        if (losSec > 0) { setLabel(`IN PASS ${losSec}s`); setColor('var(--green)'); setUrgent(true) }
        else { setLabel('PASS DONE'); setColor('var(--text-dim)'); setUrgent(false) }
      } else {
        const h = Math.floor(diffSec / 3600)
        const m = Math.floor((diffSec % 3600) / 60)
        const s = diffSec % 60
        const elev = window.elevDeg.toFixed(0)
        const t = h > 0
          ? `${h}h ${m}m ${String(s).padStart(2, '0')}s`
          : m > 0
            ? `${m}m ${String(s).padStart(2, '0')}s`
            : `${s}s`
        setLabel(`${t} · ${elev}°`)
        setColor(diffSec < 300 ? 'var(--amber)' : 'var(--cyan)')
        setUrgent(diffSec < 300)
      }
    }
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [window])

  return { label, color, urgent }
}

interface ChannelDetail { z: number; mean: number; std: number }

interface InferenceDetail {
  class_label: string
  confidence: number
  channels: {
    bat_v: ChannelDetail
    chassis_c: ChannelDetail
    rssi: ChannelDetail
    att_vel: ChannelDetail
  }
  pre_fault_class: string
  pre_fault_chan: string
  storm_level?: string
  kp_index?: number
}

function useInferenceDetail() {
  const [detail, setDetail] = useState<InferenceDetail | null>(null)
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/inference/state')
        if (res.ok) setDetail(await res.json() as InferenceDetail)
      } catch {  }
      timer = setTimeout(poll, INFERENCE_POLL_MS)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])
  return detail
}

const ANOMALY_COLOR: Record<string, string> = {
  NOMINAL: 'var(--green)',
  ECLIPSE_ENTRY: 'var(--cyan)',
  ECLIPSE_COMPUTE: 'var(--purple)',
  POWER_ANOMALY: 'var(--red)',
  THERMAL_EVENT: 'var(--amber)',
  ATTITUDE_INSTABILITY: 'var(--amber)',
  RF_DEGRADATION: 'var(--amber)',
}

interface KPITile { label: string; value: string; color: string; nacks?: number }

function kpis(m: LinkMetrics): KPITile[] {
  const orbitronMB = m.bytes_received_orbitron / 1e6
  const jsonMB = m.bytes_equivalent_json / 1e6
  const savedMB = Math.max(0, jsonMB - orbitronMB)
  const eff = jsonMB > 0 ? ((savedMB / jsonMB) * 100).toFixed(1) : '0.0'
  const effNum = parseFloat(eff)
  return [
    { label: 'PACKETS ACK', value: m.packets_received.toLocaleString(), color: 'var(--cyan)', nacks: m.nacks_issued },
    { label: 'EFFICIENCY',  value: `${eff}%`, color: effNum >= 80 ? 'var(--green)' : effNum >= 60 ? 'var(--amber)' : 'var(--red)' },
    { label: 'BYTES SAVED', value: `${(savedMB * 1000).toFixed(1)} KB`, color: 'var(--teal)' },
  ]
}

export function KPIBar({ metrics, satellite, connected, linkLost, tleSource, tleGroup, tleCount }: Props) {
  const { label: passLabel, color: passColor, urgent: passUrgent } = useNextPass()
  const inferenceDetail = useInferenceDetail()
  const aiTileRef = useRef<HTMLDivElement>(null)
  const [aiHover, setAiHover] = useState(false)

  const inferenceLabel = satellite?.inference_label ?? 'NOMINAL'
  const inferenceColor = ANOMALY_COLOR[inferenceLabel] ?? 'var(--text-dim)'
  const preFaultClass = inferenceDetail?.pre_fault_class ?? ''
  const preFaultChan = inferenceDetail?.pre_fault_chan ?? ''
  const preFaultColor = ANOMALY_COLOR[preFaultClass] ?? 'var(--amber)'

  const [eclipseStart, setEclipseStart] = useState<number | null>(null)
  const [eclipseElapsed, setEclipseElapsed] = useState(0)

  useEffect(() => {
    if (inferenceLabel === 'ECLIPSE_COMPUTE') {
      if (eclipseStart === null) setEclipseStart(Date.now())
    } else {
      setEclipseStart(null)
      setEclipseElapsed(0)
    }
  }, [inferenceLabel, eclipseStart])

  useEffect(() => {
    if (eclipseStart === null) return
    const id = setInterval(() => {
      setEclipseElapsed(Math.floor((Date.now() - eclipseStart) / 1000))
    }, 1000)
    return () => clearInterval(id)
  }, [eclipseStart])

  const offlineText = linkLost ? 'LINK LOST' : 'AWAITING LINK ACQUISITION...'
  const offlineColor = linkLost ? 'var(--red)' : 'var(--text-dim)'

  return (
    <div style={{ position: 'fixed', top: 0, left: 0, right: 0, zIndex: 110 }}>
    
    {preFaultClass && (
      <div style={{
        background: `color-mix(in srgb, ${preFaultColor} 15%, rgba(4,13,28,0.95))`,
        borderBottom: `1px solid ${preFaultColor}`,
        padding: '2px 20px',
        display: 'flex', alignItems: 'center', gap: 10,
        animation: 'kpi-pulse 2s ease-in-out infinite',
      }}>
        <span style={{ color: preFaultColor, fontSize: 9, fontWeight: 700, letterSpacing: 2 }}>
          ▲ PRE-FAULT
        </span>
        <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>
          {preFaultClass} trend detected on {preFaultChan} — z-score rising toward threshold
        </span>
      </div>
    )}
    <div style={{
      display: 'flex', alignItems: 'center', gap: 1,
      height: 48,
      background: 'rgba(4,13,28,0.92)', borderBottom: '1px solid var(--border)',
      backdropFilter: 'blur(6px)',
    }}>
      
      <div style={{
        padding: '8px 20px', borderRight: '1px solid var(--border)',
        display: 'flex', flexDirection: 'column', minWidth: 180,
      }}>
        <span style={{ color: 'var(--cyan)', fontSize: 11, letterSpacing: 3, fontWeight: 700 }}>
          ORBITRON
        </span>
        <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 2, marginTop: 2 }}>
          MISSION CONTROL
        </span>
      </div>

      
      <div style={{ padding: '0 12px', borderRight: '1px solid var(--border)' }}>
        <TleSelector source={tleSource} group={tleGroup} count={tleCount} />
      </div>

      
      <div style={{ flex: 1, display: 'flex' }}>
        {metrics
          ? kpis(metrics).map(({ label, value, color, nacks }) => (
              <div key={label} style={{
                padding: '6px 16px', borderRight: '1px solid var(--bg-dark)',
                display: 'flex', flexDirection: 'column', alignItems: 'center',
                position: 'relative', minWidth: 90,
              }}>
                <span style={{ color, fontSize: 16, fontWeight: 700, lineHeight: 1.2 }}>{value}</span>
                <div style={{ display: 'flex', alignItems: 'center', gap: 5, marginTop: 2 }}>
                  <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 2 }}>{label}</span>
                  {nacks != null && nacks > 0 && (
                    <span style={{
                      color: 'var(--amber)', fontSize: 7, letterSpacing: 1, fontWeight: 700,
                      border: '1px solid var(--amber)', borderRadius: 2, padding: '0 3px',
                    }}>
                      {nacks} NACK
                    </span>
                  )}
                </div>
              </div>
            ))
          : <div style={{ padding: '12px 20px', color: offlineColor, fontSize: 10, letterSpacing: 2, fontWeight: linkLost ? 700 : 400 }}>
              {offlineText}
            </div>
        }
      </div>

      
      <div
        ref={aiTileRef}
        onMouseEnter={() => setAiHover(true)}
        onMouseLeave={() => setAiHover(false)}
        style={{
          position: 'relative',
          padding: '6px 14px', borderLeft: '1px solid var(--border)',
          display: 'flex', flexDirection: 'column', alignItems: 'center', minWidth: 130,
          background: inferenceLabel !== 'NOMINAL' ? `color-mix(in srgb, ${inferenceColor} 8%, transparent)` : 'transparent',
          borderTop: inferenceLabel !== 'NOMINAL' ? `2px solid ${inferenceColor}` : '2px solid transparent',
          cursor: 'default',
        }}>
        <span style={{ color: inferenceColor, fontSize: 11, fontWeight: 700, lineHeight: 1.2, letterSpacing: 1 }}>
          {inferenceLabel}
        </span>
        {inferenceLabel === 'ECLIPSE_COMPUTE' && eclipseStart !== null && (
          <span style={{ color: 'var(--purple)', fontSize: 8, letterSpacing: 1, marginTop: 1 }}>
            +{String(Math.floor(eclipseElapsed / 60)).padStart(2, '0')}:{String(eclipseElapsed % 60).padStart(2, '0')} compute
          </span>
        )}
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 2 }}>
          <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 2 }}>ONBOARD AI</span>
          {inferenceDetail && (
            <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>
              · Kp {(inferenceDetail.kp_index ?? 0).toFixed(1)}
            </span>
          )}
        </div>
        
        {aiHover && inferenceDetail?.channels && (
          <div style={{
            position: 'absolute', top: '100%', right: 0, zIndex: 200,
            background: 'rgba(4,13,28,0.97)', border: '1px solid var(--border)',
            padding: '8px 12px', minWidth: 220,
            backdropFilter: 'blur(8px)',
          }}>
            <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2, marginBottom: 6 }}>Z-SCORES vs 30-FRAME BASELINE</div>
            {([
              ['bat_v',     'BAT V',    inferenceDetail.channels.bat_v],
              ['chassis_c', 'CHASSIS T', inferenceDetail.channels.chassis_c],
              ['rssi',      'RSSI',     inferenceDetail.channels.rssi],
              ['att_vel',   'ATT VEL',  inferenceDetail.channels.att_vel],
            ] as [string, string, ChannelDetail][]).map(([key, label, ch]) => {
              const zAbs = Math.abs(ch.z)
              const zColor = zAbs > 2 ? 'var(--red)' : zAbs > 1 ? 'var(--amber)' : 'var(--green)'
              return (
                <div key={key} style={{ display: 'flex', justifyContent: 'space-between', gap: 12, marginBottom: 3 }}>
                  <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>{label}</span>
                  <span style={{ color: zColor, fontSize: 9, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}>
                    z={ch.z >= 0 ? '+' : ''}{ch.z.toFixed(2)}
                  </span>
                </div>
              )
            })}
            {preFaultChan && (
              <div style={{ marginTop: 6, borderTop: '1px solid var(--border)', paddingTop: 5, color: preFaultColor, fontSize: 8, letterSpacing: 1 }}>
                ▲ {preFaultChan} trending → {preFaultClass}
              </div>
            )}
          </div>
        )}
      </div>

      

      
      <div style={{
        padding: '6px 16px', borderLeft: '1px solid var(--border)',
        display: 'flex', flexDirection: 'column', alignItems: 'center', minWidth: 118,
        background: passUrgent ? 'rgba(240,168,0,0.07)' : 'transparent',
        borderTop: passUrgent ? '2px solid var(--amber)' : '2px solid transparent',
      }}>
        <span style={{ color: passColor, fontSize: 14, fontWeight: 700, lineHeight: 1.2, letterSpacing: 1 }}>
          {passLabel}
        </span>
        <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 2, marginTop: 2 }}>NEXT PASS</span>
      </div>

      
      <div style={{
        padding: '6px 16px', borderLeft: '1px solid var(--border)',
        display: 'flex', alignItems: 'center', gap: 6,
      }}>
        <div style={{
          width: 6, height: 6, borderRadius: '50%',
          background: connected ? 'var(--green)' : 'var(--red)',
          boxShadow: connected ? '0 0 6px var(--green)' : '0 0 6px var(--red)',
        }} />
        <span style={{ color: connected ? 'var(--green)' : 'var(--red)', fontSize: 9, letterSpacing: 2 }}>
          {connected ? 'LIVE' : 'OFFLINE'}
        </span>
      </div>
    </div>
    </div>
  )
}
