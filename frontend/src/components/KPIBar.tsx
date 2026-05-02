import { useEffect, useState, useRef } from 'react'
import type { LinkMetrics, SatelliteState } from '../types'
import { TleSelector } from './TleSelector'

const WINDOWS_POLL_MS = 30_000
const ISL_POLL_MS = 5_000
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

  // Poll server every 30s — just stores the raw window timestamps
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
      } catch { /* keep last */ }
      timer = setTimeout(poll, WINDOWS_POLL_MS)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])

  // Tick every second — recomputes label from stored window data
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

interface ISLNode {
  label: string
  color: string
  detail: string
  aosSec: number | null   // seconds to next AOS; null = currently in contact
  inContact: boolean
}

function fmtAOS(sec: number): string {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? `${m}m ${s}s` : `${s}s`
}

function useISLNodes() {
  const [nodes, setNodes] = useState<[ISLNode, ISLNode]>([
    { label: 'RELAY-1', color: 'var(--text-dim)', detail: '...', aosSec: null, inContact: false },
    { label: 'RELAY-2', color: 'var(--text-dim)', detail: '...', aosSec: null, inContact: false },
  ])

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>

    async function poll() {
      const [healthResults, windowResults] = await Promise.all([
        Promise.allSettled([
          fetch('/relay/health').then(r => r.ok ? r.json() : Promise.reject()),
          fetch('/relay2/health').then(r => r.ok ? r.json() : Promise.reject()),
        ]),
        Promise.allSettled([
          fetch('/relay/windows').then(r => r.ok ? r.json() : Promise.reject()),
          fetch('/relay2/windows').then(r => r.ok ? r.json() : Promise.reject()),
        ]),
      ])

      setNodes(healthResults.map((res, i) => {
        const label = i === 0 ? 'RELAY-1' : 'RELAY-2'

        // Parse windows for AOS countdown
        let aosSec: number | null = null
        let inContact = false
        const wr = windowResults[i]
        if (wr.status === 'fulfilled') {
          const wd = wr.value as { windows: { aos: string }[]; in_contact: boolean }
          inContact = wd.in_contact ?? false
          if (!inContact && wd.windows?.length) {
            const diffSec = Math.max(0, Math.round((new Date(wd.windows[0].aos).getTime() - Date.now()) / 1000))
            aosSec = diffSec
          }
        }

        if (res.status === 'rejected') return { label, color: 'var(--red)', detail: 'OFFLINE', aosSec: null, inContact: false }
        const d = res.value as { buffer_has_data: boolean }
        const color = inContact ? 'var(--green)' : d.buffer_has_data ? 'var(--cyan)' : 'var(--amber)'
        const detail = inContact ? 'IN CONTACT' : d.buffer_has_data ? 'BUFFERED' : 'EMPTY'
        return { label, color, detail, aosSec, inContact }
      }) as [ISLNode, ISLNode])

      timer = setTimeout(poll, ISL_POLL_MS)
    }

    poll()
    return () => clearTimeout(timer)
  }, [])

  return nodes
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
      } catch { /* keep last */ }
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
  const zenithMB = m.bytes_received_zenith / 1e6
  const jsonMB = m.bytes_equivalent_json / 1e6
  const savedMB = Math.max(0, jsonMB - zenithMB)
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
  const islNodes = useISLNodes()
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

  const nearestRelay = !linkLost
    ? islNodes
        .filter(n => !n.inContact && n.aosSec !== null)
        .sort((a, b) => (a.aosSec ?? 0) - (b.aosSec ?? 0))[0]
    : null
  const offlineText = linkLost
    ? 'LINK LOST'
    : nearestRelay?.aosSec != null
      ? `RELAY CONTACT IN ${fmtAOS(nearestRelay.aosSec)} · ${nearestRelay.label}`
      : 'AWAITING LINK ACQUISITION...'
  const offlineColor = linkLost ? 'var(--red)' : nearestRelay ? 'var(--cyan)' : 'var(--text-dim)'

  return (
    <div style={{ position: 'fixed', top: 0, left: 0, right: 0, zIndex: 100 }}>
    {/* PRE-FAULT warning banner — appears above KPI bar when a channel is trending toward anomaly */}
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
      {/* Logo */}
      <div style={{
        padding: '8px 20px', borderRight: '1px solid var(--border)',
        display: 'flex', flexDirection: 'column', minWidth: 180,
      }}>
        <span style={{ color: 'var(--cyan)', fontSize: 11, letterSpacing: 3, fontWeight: 700 }}>
          ZENITH-LINK
        </span>
        <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 2, marginTop: 2 }}>
          MISSION CONTROL
        </span>
      </div>

      {/* TLE source selector */}
      <div style={{ padding: '0 12px', borderRight: '1px solid var(--border)' }}>
        <TleSelector source={tleSource} group={tleGroup} count={tleCount} />
      </div>

      {/* KPI tiles — 3 tiles: PACKETS (with NACK badge), EFFICIENCY, BYTES SAVED */}
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

      {/* Onboard health — anomaly class from inference engine */}
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
        {/* Z-score tooltip */}
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

      {/* ISL mesh nodes */}
      <div style={{
        padding: '4px 12px', borderLeft: '1px solid var(--border)',
        display: 'flex', flexDirection: 'column', gap: 2, justifyContent: 'center',
      }}>
        {islNodes.map(node => (
          <div key={node.label} style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
            <div style={{
              width: 5, height: 5, borderRadius: '50%',
              background: node.color,
              boxShadow: node.color !== 'var(--text-dim)' ? `0 0 4px ${node.color}` : 'none',
              flexShrink: 0,
            }} />
            <span style={{ color: node.color, fontSize: 8, letterSpacing: 1, fontWeight: 600 }}>{node.label}</span>
            <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>{node.detail}</span>
            {node.aosSec !== null && (
              <span style={{ color: 'var(--cyan)', fontSize: 7, letterSpacing: 1 }}>
                · AOS {fmtAOS(node.aosSec)}
              </span>
            )}
          </div>
        ))}
        <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>ISL MESH</span>
      </div>

      {/* NEXT PASS — elevated tile */}
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

      {/* Link status */}
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
