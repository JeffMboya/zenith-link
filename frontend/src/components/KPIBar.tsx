import { useEffect, useState, useRef } from 'react'
import type { LinkMetrics, SatelliteState } from '../types'
import type { SatCommandStateMap } from '../hooks/useSatCommandState'
import { TleSelector } from './TleSelector'

const CLOUD_POLL_MS     = 15_000
const INFERENCE_POLL_MS = 2_000

// ─── props ───────────────────────────────────────────────────────────────────

interface Props {
  metrics:       LinkMetrics | null
  satellite:     SatelliteState | null
  connected:     boolean
  linkLost:      boolean
  tleSource:     'sim' | 'tle'
  tleGroup:      string
  tleCount:      number
  selectedSatId: string
  cmdState:      SatCommandStateMap
}

// ─── types ───────────────────────────────────────────────────────────────────

interface DeliveryState {
  aosMs:      number | null
  durationMs: number
  relay:      string
  gs:         string
  flowing:    boolean
}

interface GSCloudEntry {
  name:            string
  cloud_cover_pct: number | null
  impaired:        boolean
  source:          string
}

interface ChannelDetail { z: number; mean: number; std: number }
interface InferenceDetail {
  class_label:     string
  confidence:      number
  channels:        { bat_v: ChannelDetail; chassis_c: ChannelDetail; rssi: ChannelDetail; att_vel: ChannelDetail }
  pre_fault_class: string
  pre_fault_chan:  string
  storm_level?:    string
  kp_index?:       number
}

// ─── hooks ───────────────────────────────────────────────────────────────────

function useNextDelivery() {
  const [delivery, setDelivery] = useState<DeliveryState | null>(null)
  const [label, setLabel]       = useState('—')
  const [detail, setDetail]     = useState('')
  const [color, setColor]       = useState('var(--text-dim)')
  const [urgent, setUrgent]     = useState(false)

  useEffect(() => {
    const h = (e: Event) => setDelivery((e as CustomEvent<DeliveryState>).detail)
    window.addEventListener('delivery-state', h)
    return () => window.removeEventListener('delivery-state', h)
  }, [])

  useEffect(() => {
    function tick() {
      if (!delivery) {
        setLabel('—'); setDetail(''); setColor('var(--text-dim)'); setUrgent(false); return
      }
      if (delivery.flowing) {
        setLabel('DATA FLOWING'); setDetail(`${delivery.relay} → ${delivery.gs}`)
        setColor('var(--green)'); setUrgent(true); return
      }
      if (delivery.aosMs === null) return
      const diffSec = Math.round((delivery.aosMs - Date.now()) / 1000)
      if (diffSec < 0) {
        const remaining = Math.round((delivery.aosMs + delivery.durationMs - Date.now()) / 1000)
        if (remaining > 0) {
          setLabel(`IN CONTACT · ${remaining}s`); setDetail(`${delivery.relay} → ${delivery.gs}`)
          setColor('var(--green)'); setUrgent(true)
        } else {
          setLabel('—'); setDetail(''); setColor('var(--text-dim)'); setUrgent(false)
        }
        return
      }
      const h = Math.floor(diffSec / 3600)
      const m = Math.floor((diffSec % 3600) / 60)
      const s = diffSec % 60
      const t = h > 0
        ? `${h}h ${m}m ${String(s).padStart(2, '0')}s`
        : m > 0 ? `${m}m ${String(s).padStart(2, '0')}s` : `${s}s`
      setLabel(t); setDetail(`${delivery.relay} → ${delivery.gs}`)
      setColor(diffSec < 300 ? 'var(--amber)' : 'var(--cyan)'); setUrgent(diffSec < 300)
    }
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [delivery])

  return { label, detail, color, urgent, delivery }
}

function useThroughput(bytesTotal: number | null): number {
  const prev = useRef<{ bytes: number; ts: number } | null>(null)
  const [bps, setBps] = useState(0)
  useEffect(() => {
    if (bytesTotal === null) { prev.current = null; setBps(0); return }
    const now = Date.now()
    if (prev.current) {
      const dt = (now - prev.current.ts) / 1000
      const db = bytesTotal - prev.current.bytes
      if (dt > 0) setBps(db / dt)
    }
    prev.current = { bytes: bytesTotal, ts: now }
  }, [bytesTotal])
  return bps
}

function useCloudCover(): GSCloudEntry[] {
  const [entries, setEntries] = useState<GSCloudEntry[]>([])
  useEffect(() => {
    let t: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/relay1/health')
        if (res.ok) {
          const d = await res.json() as {
            ground_stations?: Record<string, { cloud_cover_pct?: number; impaired?: boolean; source?: string }>
          }
          if (d.ground_stations) {
            setEntries(Object.entries(d.ground_stations).map(([name, gs]) => ({
              name,
              cloud_cover_pct: gs.cloud_cover_pct ?? null,
              impaired:        gs.impaired ?? false,
              source:          gs.source ?? 'fallback',
            })))
          }
        }
      } catch { /* silent */ }
      t = setTimeout(poll, CLOUD_POLL_MS)
    }
    poll()
    return () => clearTimeout(t)
  }, [])
  return entries
}

function useInferenceDetail() {
  const [detail, setDetail] = useState<InferenceDetail | null>(null)
  useEffect(() => {
    let t: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/inference/state')
        if (res.ok) setDetail(await res.json() as InferenceDetail)
      } catch { /* silent */ }
      t = setTimeout(poll, INFERENCE_POLL_MS)
    }
    poll()
    return () => clearTimeout(t)
  }, [])
  return detail
}

// ─── utils ───────────────────────────────────────────────────────────────────

function fmtBytes(b: number): string {
  if (b >= 1e6) return `${(b / 1e6).toFixed(1)} MB`
  if (b >= 1e3) return `${(b / 1e3).toFixed(1)} KB`
  return `${b.toFixed(0)} B`
}

function fmtBps(bps: number): string {
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} MB/s`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(1)} KB/s`
  return `${bps.toFixed(0)} B/s`
}

function gsShortName(name: string): string {
  const u = name.toUpperCase()
  if (u.includes('SVALBARD'))  return 'SVL'
  if (u.includes('KIRUNA'))    return 'KRN'
  if (u.includes('FAIRBANKS')) return 'FAI'
  if (u.includes('HAWAII'))    return 'HWI'
  if (u.includes('SANTIAGO'))  return 'STG'
  if (u.includes('PERTH'))     return 'PTH'
  return name.slice(0, 4).toUpperCase()
}

function cloudColor(pct: number | null, impaired: boolean): string {
  if (impaired)                return 'var(--amber)'
  if (pct === null || pct < 0) return 'var(--text-dim)'
  if (pct < 30)                return 'var(--green)'
  if (pct < 70)                return 'var(--amber)'
  return 'var(--red)'
}

// ─── constants ────────────────────────────────────────────────────────────────

const ANOMALY_COLOR: Record<string, string> = {
  NOMINAL:              'var(--green)',
  ECLIPSE_ENTRY:        'var(--cyan)',
  ECLIPSE_COMPUTE:      'var(--purple)',
  POWER_ANOMALY:        'var(--red)',
  THERMAL_EVENT:        'var(--amber)',
  ATTITUDE_INSTABILITY: 'var(--amber)',
  RF_DEGRADATION:       'var(--amber)',
}

function kpiCards(m: LinkMetrics | null) {
  if (!m) return [
    { label: 'PACKETS',    value: '—', color: 'var(--text-dim)', nacks: 0 },
    { label: 'EFFICIENCY', value: '—', color: 'var(--text-dim)', nacks: undefined },
    { label: 'SAVED',      value: '—', color: 'var(--text-dim)', nacks: undefined },
  ]
  const orbitronMB = m.bytes_received_orbitron / 1e6
  const jsonMB     = m.bytes_equivalent_json   / 1e6
  const savedMB    = Math.max(0, jsonMB - orbitronMB)
  const eff        = jsonMB > 0 ? ((savedMB / jsonMB) * 100).toFixed(1) : '0.0'
  const effNum     = parseFloat(eff)
  return [
    { label: 'PACKETS',    value: m.packets_received.toLocaleString(),  color: 'var(--cyan)', nacks: m.nacks_issued },
    { label: 'EFFICIENCY', value: `${eff}%`,                            color: effNum >= 80 ? 'var(--green)' : effNum >= 60 ? 'var(--amber)' : 'var(--red)', nacks: undefined },
    { label: 'SAVED',      value: `${(savedMB * 1000).toFixed(1)} KB`,  color: 'var(--teal)', nacks: undefined },
  ]
}

const DIVIDER = <div style={{ width: 1, alignSelf: 'stretch', background: 'var(--border)', flexShrink: 0 }} />

const NAV_ITEMS = ['MISSION', 'FLEET', 'RELAYS', 'TELEMETRY', 'AI'] as const

// ─── component ────────────────────────────────────────────────────────────────

export function KPIBar({ metrics, satellite, connected, linkLost, tleSource, tleGroup, tleCount, selectedSatId, cmdState }: Props) {
  const { label: passLabel, detail: passDetail, color: passColor, delivery } = useNextDelivery()
  const inferenceDetail = useInferenceDetail()
  const cloudEntries    = useCloudCover()
  const throughputBps   = useThroughput(metrics?.bytes_received_orbitron ?? null)

  const [linkOpen, setLinkOpen]   = useState(false)
  const [activeNav, setActiveNav] = useState<string | null>(null)
  const [aiHover, setAiHover]     = useState(false)
  const linkRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handler(e: MouseEvent) {
      if (linkRef.current && !linkRef.current.contains(e.target as Node)) setLinkOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  useEffect(() => {
    const h = (e: Event) => setActiveNav((e as CustomEvent<string>).detail || null)
    window.addEventListener('nav-section', h)
    return () => window.removeEventListener('nav-section', h)
  }, [])

  function dispatchNav(section: string) {
    setActiveNav(section)
    window.dispatchEvent(new CustomEvent('nav-section', { detail: section }))
  }

  const inferenceLabel = satellite?.inference_label ?? 'NOMINAL'
  const inferenceColor = ANOMALY_COLOR[inferenceLabel] ?? 'var(--text-dim)'
  const preFaultClass  = inferenceDetail?.pre_fault_class ?? ''
  const preFaultChan   = inferenceDetail?.pre_fault_chan  ?? ''
  const preFaultColor  = ANOMALY_COLOR[preFaultClass] ?? 'var(--amber)'

  const [eclipseStart,   setEclipseStart]   = useState<number | null>(null)
  const [eclipseElapsed, setEclipseElapsed] = useState(0)
  useEffect(() => {
    if (inferenceLabel === 'ECLIPSE_COMPUTE') {
      if (eclipseStart === null) setEclipseStart(Date.now())
    } else { setEclipseStart(null); setEclipseElapsed(0) }
  }, [inferenceLabel, eclipseStart])
  useEffect(() => {
    if (eclipseStart === null) return
    const id = setInterval(() => setEclipseElapsed(Math.floor((Date.now() - eclipseStart!) / 1000)), 1000)
    return () => clearInterval(id)
  }, [eclipseStart])

  const satMode     = cmdState.modes[selectedSatId]
  const isRebooting = !!cmdState.rebootPhase[selectedSatId]
  const isSafe      = satMode === 'safe'
  const dotColor    = !connected ? 'var(--red)' : isSafe ? 'var(--amber)' : 'var(--green)'
  const dotAnim     = connected && !isSafe && !isRebooting
    ? 'liveDotBlink 1s step-start infinite'
    : connected && isSafe ? 'liveDotBlink 2s step-start infinite' : 'none'
  const liveLabel     = connected ? (isSafe ? 'SAFE' : isRebooting ? 'REBOOTING' : 'LIVE') : 'OFFLINE'
  const liveTextColor = !connected ? 'var(--red)' : isSafe ? 'var(--amber)' : 'var(--green)'
  const offlineText   = linkLost ? 'LINK LOST' : 'AWAITING LINK...'
  const offlineColor  = linkLost ? 'var(--red)' : 'var(--text-dim)'

  const flowing       = delivery?.flowing ?? false
  const sessionBytes  = metrics?.bytes_received_orbitron ?? 0
  const hasThroughput = throughputBps > 10

  const linkDotColor = !connected
    ? 'var(--red)'
    : (metrics?.nacks_issued ?? 0) > 0
      ? 'var(--amber)'
      : flowing ? 'var(--green)' : 'var(--cyan)'

  const satShortName = selectedSatId.replace(/\s*\(.*?\)\s*$/, '').trim()

  return (
    <div style={{ position: 'fixed', top: 0, left: 0, right: 0, zIndex: 110 }}>

      {/* Pre-fault alert banner */}
      {preFaultClass && (
        <div style={{
          background: `color-mix(in srgb, ${preFaultColor} 15%, rgba(4,13,28,0.97))`,
          borderBottom: `1px solid ${preFaultColor}`,
          padding: '3px 20px',
          display: 'flex', alignItems: 'center', gap: 10,
        }}>
          <span style={{ color: preFaultColor, fontSize: 9, fontWeight: 700, letterSpacing: 2 }}>▲ PRE-FAULT</span>
          <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>
            {preFaultClass} trend on {preFaultChan} — z-score rising
          </span>
        </div>
      )}

      {/* ── NAV BAR — 48px ── */}
      <div style={{
        display: 'flex', alignItems: 'stretch',
        height: 48,
        background: 'rgba(4,13,28,0.97)',
        borderBottom: '1px solid var(--border)',
        backdropFilter: 'blur(8px)',
      }}>

        {/* Brand */}
        <div style={{ padding: '0 18px', display: 'flex', alignItems: 'center', flexShrink: 0 }}>
          <span style={{ color: 'var(--cyan)', fontSize: 13, letterSpacing: 3, fontWeight: 700 }}>ORBITRON</span>
        </div>

        {DIVIDER}

        {/* Nav items */}
        {NAV_ITEMS.map(item => {
          const navKey  = item.toLowerCase()
          const active  = activeNav === navKey
          const isAI    = item === 'AI'
          const aiAnom  = isAI && inferenceLabel !== 'NOMINAL'
          const btnColor = active ? 'var(--cyan)' : aiAnom ? inferenceColor : 'var(--text-mid)'
          return (
            <div key={item} style={{ position: 'relative', display: 'flex', alignItems: 'stretch' }}>
              <button
                onMouseEnter={() => isAI && setAiHover(true)}
                onMouseLeave={() => isAI && setAiHover(false)}
                onClick={() => dispatchNav(navKey)}
                style={{
                  padding: '0 14px',
                  background: 'transparent', border: 'none',
                  borderBottom: active ? '2px solid var(--cyan)' : '2px solid transparent',
                  color: btnColor, fontSize: 11, letterSpacing: 1.5,
                  cursor: 'pointer', fontFamily: 'inherit',
                  display: 'flex', alignItems: 'center', gap: 5,
                  whiteSpace: 'nowrap',
                }}
              >
                {item}
                {aiAnom && (
                  <span style={{ width: 5, height: 5, borderRadius: '50%', background: inferenceColor, flexShrink: 0 }} />
                )}
              </button>

              {/* AI hover tooltip */}
              {isAI && aiHover && inferenceDetail?.channels && (
                <div style={{
                  position: 'absolute', top: '100%', left: 0, zIndex: 300,
                  background: 'rgba(4,13,28,0.97)', border: '1px solid var(--border)',
                  padding: '10px 14px', minWidth: 230, backdropFilter: 'blur(8px)',
                }}>
                  <div style={{ color: inferenceColor, fontSize: 11, fontWeight: 700, letterSpacing: 1, marginBottom: 4 }}>
                    {inferenceLabel}
                    {inferenceDetail.kp_index != null && (
                      <span style={{ color: 'var(--text-dim)', fontSize: 9, fontWeight: 400, marginLeft: 8 }}>
                        Kp {inferenceDetail.kp_index.toFixed(1)}
                      </span>
                    )}
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2, marginBottom: 7 }}>
                    Z-SCORES vs 30-FRAME BASELINE
                  </div>
                  {([
                    ['bat_v',     'BAT V',    inferenceDetail.channels.bat_v],
                    ['chassis_c', 'CHASSIS T', inferenceDetail.channels.chassis_c],
                    ['rssi',      'RSSI',      inferenceDetail.channels.rssi],
                    ['att_vel',   'ATT VEL',  inferenceDetail.channels.att_vel],
                  ] as [string, string, ChannelDetail][]).map(([chKey, chLabel, ch]) => {
                    const zAbs   = Math.abs(ch.z)
                    const zColor = zAbs > 2 ? 'var(--red)' : zAbs > 1 ? 'var(--amber)' : 'var(--green)'
                    return (
                      <div key={chKey} style={{ display: 'flex', justifyContent: 'space-between', gap: 16, marginBottom: 4 }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>{chLabel}</span>
                        <span style={{ color: zColor, fontSize: 9, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}>
                          z={ch.z >= 0 ? '+' : ''}{ch.z.toFixed(2)}
                        </span>
                      </div>
                    )
                  })}
                  {inferenceLabel === 'ECLIPSE_COMPUTE' && eclipseStart !== null && (
                    <div style={{ color: 'var(--purple)', fontSize: 8, letterSpacing: 1, marginTop: 6 }}>
                      ECLIPSE +{String(Math.floor(eclipseElapsed / 60)).padStart(2, '0')}:{String(eclipseElapsed % 60).padStart(2, '0')}
                    </div>
                  )}
                  {preFaultChan && (
                    <div style={{ marginTop: 7, borderTop: '1px solid var(--border)', paddingTop: 6, color: preFaultColor, fontSize: 8, letterSpacing: 1 }}>
                      ▲ {preFaultChan} → {preFaultClass}
                    </div>
                  )}
                </div>
              )}
            </div>
          )
        })}

        {/* LINK ▾ dropdown */}
        <div ref={linkRef} style={{ position: 'relative', display: 'flex', alignItems: 'stretch' }}>
          <button
            onClick={() => setLinkOpen(o => !o)}
            style={{
              padding: '0 14px',
              background: 'transparent', border: 'none',
              borderBottom: linkOpen ? '2px solid var(--cyan)' : '2px solid transparent',
              color: linkOpen ? 'var(--cyan)' : 'var(--text-mid)',
              fontSize: 11, letterSpacing: 1.5,
              cursor: 'pointer', fontFamily: 'inherit',
              display: 'flex', alignItems: 'center', gap: 6,
              whiteSpace: 'nowrap',
            }}
          >
            <span style={{
              width: 6, height: 6, borderRadius: '50%',
              background: linkDotColor, boxShadow: `0 0 5px ${linkDotColor}`, flexShrink: 0,
            }} />
            LINK
            <span style={{ fontSize: 7, opacity: 0.5 }}>{linkOpen ? '▲' : '▼'}</span>
          </button>

          {linkOpen && (
            <div style={{
              position: 'absolute', top: '100%', left: 0, zIndex: 200,
              background: 'rgba(4,13,28,0.97)', border: '1px solid var(--border)',
              backdropFilter: 'blur(8px)', minWidth: 260,
            }}>

              {/* Link metrics */}
              <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)' }}>
                <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2, marginBottom: 8 }}>LINK METRICS</div>
                {isRebooting ? (
                  <div style={{ color: 'var(--amber)', fontSize: 11, letterSpacing: 2 }}>REBOOTING OBC...</div>
                ) : !connected ? (
                  <div style={{ color: offlineColor, fontSize: 11, letterSpacing: 1 }}>{offlineText}</div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                    {kpiCards(metrics).map(({ label, value, color, nacks }) => (
                      <div key={label} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>{label}</span>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                          <span style={{ color, fontSize: 13, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}>{value}</span>
                          {nacks != null && nacks > 0 && (
                            <span style={{ color: 'var(--amber)', fontSize: 8, border: '1px solid var(--amber)', borderRadius: 2, padding: '0 2px' }}>
                              {nacks}N
                            </span>
                          )}
                        </div>
                      </div>
                    ))}
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>THROUGHPUT</span>
                      <span style={{
                        color: hasThroughput ? (flowing ? 'var(--green)' : 'var(--cyan)') : 'var(--text-dim)',
                        fontSize: 13, fontWeight: 700, fontVariantNumeric: 'tabular-nums',
                      }}>
                        {hasThroughput ? fmtBps(throughputBps) : '—'}
                      </span>
                    </div>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>SESSION</span>
                      <span style={{
                        color: sessionBytes > 0 ? 'var(--teal)' : 'var(--text-dim)',
                        fontSize: 13, fontWeight: 700, fontVariantNumeric: 'tabular-nums',
                      }}>
                        {sessionBytes > 0 ? fmtBytes(sessionBytes) : '—'}
                      </span>
                    </div>
                  </div>
                )}
              </div>

              {/* Cloud cover */}
              <div style={{ padding: '10px 14px' }}>
                <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2, marginBottom: 8 }}>CLOUD COVER</div>
                {cloudEntries.length === 0 ? (
                  <div style={{ color: 'var(--text-dim)', fontSize: 9, opacity: 0.5 }}>POLLING...</div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                    {cloudEntries.map(gs => (
                      <div key={gs.name} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>☁ {gsShortName(gs.name)}</span>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <span style={{ color: cloudColor(gs.cloud_cover_pct, gs.impaired), fontSize: 8, opacity: 0.7 }}>
                            {gs.impaired ? 'IMPAIRED' : gs.source === 'open-meteo' ? 'LIVE' : 'EST'}
                          </span>
                          <span style={{
                            color: cloudColor(gs.cloud_cover_pct, gs.impaired),
                            fontSize: 13, fontWeight: 700, fontVariantNumeric: 'tabular-nums',
                          }}>
                            {gs.cloud_cover_pct != null ? `${gs.cloud_cover_pct.toFixed(0)}%` : '—'}
                          </span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>

            </div>
          )}
        </div>

        {/* Spacer */}
        <div style={{ flex: 1 }} />

        {/* Pass countdown */}
        {passLabel !== '—' && (
          <div style={{
            display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'flex-end',
            padding: '0 14px', borderLeft: '1px solid var(--border)', flexShrink: 0,
          }}>
            <span style={{
              color: passColor, fontSize: 13, fontWeight: 700, letterSpacing: 0.5,
              fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap', lineHeight: 1.2,
            }}>
              {passLabel}
            </span>
            {passDetail && (
              <span style={{ color: passColor, fontSize: 8, opacity: 0.6, letterSpacing: 0.3, whiteSpace: 'nowrap' }}>
                {passDetail}
              </span>
            )}
            <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1.5, marginTop: 1 }}>NEXT DELIVERY</span>
          </div>
        )}

        {/* Satellite status */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '0 14px', borderLeft: '1px solid var(--border)', flexShrink: 0 }}>
          <div style={{
            width: 7, height: 7, borderRadius: '50%',
            background: dotColor, boxShadow: `0 0 6px ${dotColor}`,
            animation: dotAnim, flexShrink: 0,
          }} />
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <span style={{ color: liveTextColor, fontSize: 11, fontWeight: 700, letterSpacing: 1, whiteSpace: 'nowrap', lineHeight: 1.2 }}>
              {satShortName}
            </span>
            <span style={{ color: liveTextColor, fontSize: 9, opacity: 0.7, letterSpacing: 1.5, lineHeight: 1.2 }}>
              {liveLabel}
            </span>
          </div>
        </div>

        {/* TLE badge */}
        <div style={{ padding: '0 12px', borderLeft: '1px solid var(--border)', flexShrink: 0, display: 'flex', alignItems: 'center' }}>
          <TleSelector source={tleSource} group={tleGroup} count={tleCount} />
        </div>

      </div>
    </div>
  )
}
