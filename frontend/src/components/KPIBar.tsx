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

interface ContactWindow { aos: string; los: string; duration_sec: number; max_elevation_deg: number }

const RELAYS = [
  { path: '/relay1', name: 'NOAA-20' },
  { path: '/relay2', name: 'Sentinel-2A' },
  { path: '/relay3', name: 'Landsat-9' },
  { path: '/relay4', name: 'Aqua' },
  { path: '/relay5', name: 'Terra' },
  { path: '/relay6', name: 'NOAA-19' },
]

const GS_LIST = [
  { name: 'Nairobi',      lat: -1.2864,  lon:  36.8172 },
  { name: 'Svalbard',     lat: 78.2232,  lon:  15.6267 },
  { name: 'Punta Arenas', lat: -53.1638, lon: -70.9171 },
  { name: 'Fairbanks',    lat: 64.8201,  lon: -147.720 },
  { name: 'Bangalore',    lat: 12.9716,  lon:  77.5946 },
  { name: 'Perth',        lat: -31.9505, lon: 115.8605 },
]

interface DeliveryState {
  aosMs: number | null
  durationMs: number
  relay: string
  gs: string
  flowing: boolean
}

function useNextDelivery() {
  const [delivery, setDelivery] = useState<DeliveryState | null>(null)
  const [label, setLabel]       = useState('—')
  const [detail, setDetail]     = useState('')
  const [color, setColor]       = useState('var(--text-dim)')
  const [urgent, setUrgent]     = useState(false)

  // Fetch all 36 relay×GS contact windows every 30s
  useEffect(() => {
    let t: ReturnType<typeof setTimeout>
    async function poll() {
      const combos = RELAYS.flatMap(relay =>
        GS_LIST.map(gs => ({
          relay: relay.name,
          gs: gs.name,
          url: `${relay.path}/windows?gs_lat=${gs.lat}&gs_lon=${gs.lon}`,
        }))
      )

      const results = await Promise.allSettled(
        combos.map(c => fetch(c.url).then(r => r.ok ? r.json() : Promise.reject()))
      )

      let earliest: DeliveryState | null = null

      results.forEach((r, i) => {
        if (r.status !== 'fulfilled') return
        const data = r.value as { windows: ContactWindow[]; in_contact: boolean }
        const { relay, gs } = combos[i]

        if (data.in_contact) {
          // Currently flowing — highest priority
          if (!earliest?.flowing) {
            earliest = { aosMs: null, durationMs: 0, relay, gs, flowing: true }
          }
          return
        }
        if (!data.windows?.length) return

        const aosMs = new Date(data.windows[0].aos).getTime()
        const losMs = new Date(data.windows[0].los).getTime()
        if (!earliest || earliest.flowing || (earliest.aosMs !== null && aosMs < earliest.aosMs)) {
          if (!earliest?.flowing) {
            earliest = { aosMs, durationMs: losMs - aosMs, relay, gs, flowing: false }
          }
        }
      })

      setDelivery(earliest)
      t = setTimeout(poll, WINDOWS_POLL_MS)
    }
    poll()
    return () => clearTimeout(t)
  }, [])

  // Tick every second to update countdown
  useEffect(() => {
    function tick() {
      if (!delivery) {
        setLabel('—'); setDetail(''); setColor('var(--text-dim)'); setUrgent(false)
        return
      }
      if (delivery.flowing) {
        setLabel('DATA FLOWING')
        setDetail(`${delivery.relay} → ${delivery.gs}`)
        setColor('var(--green)')
        setUrgent(true)
        return
      }
      if (delivery.aosMs === null) return
      const diffSec = Math.round((delivery.aosMs - Date.now()) / 1000)
      if (diffSec < 0) {
        const remaining = Math.round((delivery.aosMs + delivery.durationMs - Date.now()) / 1000)
        if (remaining > 0) {
          setLabel(`IN CONTACT · ${remaining}s`)
          setDetail(`${delivery.relay} → ${delivery.gs}`)
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
      setLabel(t)
      setDetail(`${delivery.relay} → ${delivery.gs}`)
      setColor(diffSec < 300 ? 'var(--amber)' : 'var(--cyan)')
      setUrgent(diffSec < 300)
    }
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [delivery])

  return { label, detail, color, urgent, delivery }
}

interface ChannelDetail { z: number; mean: number; std: number }
interface InferenceDetail {
  class_label: string; confidence: number
  channels: { bat_v: ChannelDetail; chassis_c: ChannelDetail; rssi: ChannelDetail; att_vel: ChannelDetail }
  pre_fault_class: string; pre_fault_chan: string
  storm_level?: string; kp_index?: number
}

function useInferenceDetail() {
  const [detail, setDetail] = useState<InferenceDetail | null>(null)
  useEffect(() => {
    let t: ReturnType<typeof setTimeout>
    async function poll() {
      try { const res = await fetch('/inference/state'); if (res.ok) setDetail(await res.json() as InferenceDetail) } catch { /* silent */ }
      t = setTimeout(poll, INFERENCE_POLL_MS)
    }
    poll()
    return () => clearTimeout(t)
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

function kpis(m: LinkMetrics) {
  const orbitronMB = m.bytes_received_orbitron / 1e6
  const jsonMB = m.bytes_equivalent_json / 1e6
  const savedMB = Math.max(0, jsonMB - orbitronMB)
  const eff = jsonMB > 0 ? ((savedMB / jsonMB) * 100).toFixed(1) : '0.0'
  const effNum = parseFloat(eff)
  return [
    { label: 'PACKETS', value: m.packets_received.toLocaleString(), color: 'var(--cyan)', nacks: m.nacks_issued },
    { label: 'EFFICIENCY', value: `${eff}%`, color: effNum >= 80 ? 'var(--green)' : effNum >= 60 ? 'var(--amber)' : 'var(--red)' },
    { label: 'SAVED', value: `${(savedMB * 1000).toFixed(1)} KB`, color: 'var(--teal)' },
  ]
}

const DIVIDER = <div style={{ width: 1, height: 28, background: 'var(--border)', flexShrink: 0 }} />

export function KPIBar({ metrics, satellite, connected, linkLost, tleSource, tleGroup, tleCount }: Props) {
  const { label: passLabel, detail: passDetail, color: passColor, urgent: passUrgent, delivery } = useNextDelivery()
  const inferenceDetail = useInferenceDetail()
  const aiRef = useRef<HTMLDivElement>(null)
  const [aiHover, setAiHover] = useState(false)

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
    const id = setInterval(() => setEclipseElapsed(Math.floor((Date.now() - eclipseStart) / 1000)), 1000)
    return () => clearInterval(id)
  }, [eclipseStart])

  const offlineText  = linkLost ? 'LINK LOST' : 'AWAITING LINK ACQUISITION...'
  const offlineColor = linkLost ? 'var(--red)' : 'var(--text-dim)'

  // Dispatch delivery-state to MissionFooter whenever it changes
  useEffect(() => {
    if (!delivery) return
    const snap = {
      aosMs:   'aosMs' in delivery ? (delivery as { aosMs: number | null }).aosMs : null,
      relay:   delivery.relay,
      gs:      delivery.gs,
      flowing: delivery.flowing,
    }
    window.dispatchEvent(new CustomEvent('delivery-state', { detail: snap }))
  }, [delivery])

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

      {/* Main bar */}
      <div style={{
        display: 'flex', alignItems: 'center',
        height: 48,
        background: 'rgba(4,13,28,0.94)',
        borderBottom: '1px solid var(--border)',
        backdropFilter: 'blur(8px)',
      }}>

        {/* 1. Brand — left anchor */}
        <div style={{ padding: '0 20px', display: 'flex', flexDirection: 'column', justifyContent: 'center', minWidth: 170, flexShrink: 0 }}>
          <span style={{ color: 'var(--cyan)', fontSize: 13, letterSpacing: 3, fontWeight: 700, lineHeight: 1.2 }}>
            ORBITRON
          </span>
          <span style={{ color: 'var(--text-dim)', fontSize: 10, letterSpacing: 2, marginTop: 1 }}>
            MISSION CONTROL
          </span>
        </div>

        {DIVIDER}

        {/* 2. Connection status — promoted, immediately after brand */}
        <div style={{
          padding: '0 18px',
          display: 'flex', alignItems: 'center', gap: 8,
          flexShrink: 0,
        }}>
          <div style={{
            width: 9, height: 9, borderRadius: '50%',
            background: connected ? 'var(--green)' : 'var(--red)',
            boxShadow: connected ? '0 0 8px var(--green)' : '0 0 8px var(--red)',
            flexShrink: 0,
            animation: connected ? 'liveDotBlink 1s step-start infinite' : 'none',
          }} />
          <span style={{
            color: connected ? 'var(--green)' : 'var(--red)',
            fontSize: 12, fontWeight: 700, letterSpacing: 1.5,
          }}>
            {connected ? 'LIVE' : 'OFFLINE'}
          </span>
        </div>

        {DIVIDER}

        {/* 3. Onboard AI health — fixed width to prevent layout shifts */}
        <div
          ref={aiRef}
          onMouseEnter={() => setAiHover(true)}
          onMouseLeave={() => setAiHover(false)}
          style={{
            position: 'relative',
            padding: '0 18px',
            display: 'flex', flexDirection: 'column', justifyContent: 'center',
            width: 168, flexShrink: 0,
            borderTop: inferenceLabel !== 'NOMINAL' ? `2px solid ${inferenceColor}` : '2px solid transparent',
            background: inferenceLabel !== 'NOMINAL' ? `color-mix(in srgb, ${inferenceColor} 7%, transparent)` : 'transparent',
            cursor: 'default', alignSelf: 'stretch',
          }}
        >
          <span style={{ color: inferenceColor, fontSize: 13, fontWeight: 700, letterSpacing: 0.5, lineHeight: 1.2, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {inferenceLabel}
          </span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 5, marginTop: 2 }}>
            <span style={{ color: 'var(--text-dim)', fontSize: 10, letterSpacing: 1.5 }}>ONBOARD AI</span>
            {inferenceDetail && (
              <span style={{ color: 'var(--text-dim)', fontSize: 8 }}>· Kp {(inferenceDetail.kp_index ?? 0).toFixed(1)}</span>
            )}
          </div>
          {inferenceLabel === 'ECLIPSE_COMPUTE' && eclipseStart !== null && (
            <span style={{ color: 'var(--purple)', fontSize: 8, letterSpacing: 1, marginTop: 1 }}>
              +{String(Math.floor(eclipseElapsed / 60)).padStart(2, '0')}:{String(eclipseElapsed % 60).padStart(2, '0')}
            </span>
          )}

          {/* AI hover tooltip */}
          {aiHover && inferenceDetail?.channels && (
            <div style={{
              position: 'absolute', top: '100%', left: 0, zIndex: 200,
              background: 'rgba(4,13,28,0.97)', border: '1px solid var(--border)',
              padding: '10px 14px', minWidth: 230, backdropFilter: 'blur(8px)',
            }}>
              <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2, marginBottom: 7 }}>
                Z-SCORES vs 30-FRAME BASELINE
              </div>
              {([
                ['bat_v',     'BAT V',     inferenceDetail.channels.bat_v],
                ['chassis_c', 'CHASSIS T', inferenceDetail.channels.chassis_c],
                ['rssi',      'RSSI',      inferenceDetail.channels.rssi],
                ['att_vel',   'ATT VEL',  inferenceDetail.channels.att_vel],
              ] as [string, string, ChannelDetail][]).map(([key, label, ch]) => {
                const zAbs = Math.abs(ch.z)
                const zColor = zAbs > 2 ? 'var(--red)' : zAbs > 1 ? 'var(--amber)' : 'var(--green)'
                return (
                  <div key={key} style={{ display: 'flex', justifyContent: 'space-between', gap: 16, marginBottom: 4 }}>
                    <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>{label}</span>
                    <span style={{ color: zColor, fontSize: 9, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}>
                      z={ch.z >= 0 ? '+' : ''}{ch.z.toFixed(2)}
                    </span>
                  </div>
                )
              })}
              {preFaultChan && (
                <div style={{ marginTop: 7, borderTop: '1px solid var(--border)', paddingTop: 6, color: preFaultColor, fontSize: 8, letterSpacing: 1 }}>
                  ▲ {preFaultChan} → {preFaultClass}
                </div>
              )}
            </div>
          )}
        </div>

        {DIVIDER}

        {/* 4. Next pass countdown — most time-critical, prominent */}
        <div style={{
          padding: '0 20px',
          display: 'flex', flexDirection: 'column', justifyContent: 'center',
          alignItems: 'flex-start', flexShrink: 0, minWidth: 160,
          borderTop: passUrgent
            ? `2px solid ${passColor}`
            : '2px solid transparent',
          background: passUrgent
            ? `color-mix(in srgb, ${passColor} 6%, transparent)`
            : 'transparent',
          alignSelf: 'stretch',
        }}>
          <span style={{ color: passColor, fontSize: 20, fontWeight: 700, lineHeight: 1.2, letterSpacing: 0.3, fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>
            {passLabel}
          </span>
          {passDetail && (
            <span style={{ color: passColor, fontSize: 9, letterSpacing: 0.5, marginTop: 2, opacity: 0.7, whiteSpace: 'nowrap' }}>
              {passDetail}
            </span>
          )}
          <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 2, marginTop: passDetail ? 1 : 2 }}>NEXT DELIVERY</span>
        </div>

        {DIVIDER}

        {/* 5. KPI stats — secondary metrics, flex fills remaining space */}
        <div style={{ flex: 1, display: 'flex', alignItems: 'center' }}>
          {metrics
            ? kpis(metrics).map(({ label, value, color, nacks }) => (
                <div key={label} style={{
                  padding: '0 16px', borderRight: '1px solid var(--bg-dark)',
                  display: 'flex', flexDirection: 'column', alignItems: 'center',
                  justifyContent: 'center', minWidth: 80, alignSelf: 'stretch',
                }}>
                  <span style={{ color, fontSize: 16, fontWeight: 700, lineHeight: 1.2, fontVariantNumeric: 'tabular-nums' }}>{value}</span>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 2 }}>
                    <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1.5 }}>{label}</span>
                    {nacks != null && nacks > 0 && (
                      <span style={{ color: 'var(--amber)', fontSize: 7, fontWeight: 700, border: '1px solid var(--amber)', borderRadius: 2, padding: '0 3px' }}>
                        {nacks}N
                      </span>
                    )}
                  </div>
                </div>
              ))
            : connected
              ? (
                  // TX details — show live protocol/bitrate when connected but metrics not yet populated
                  <div style={{ padding: '0 16px', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', minWidth: 100, alignSelf: 'stretch' }}>
                    <span style={{ color: 'var(--cyan)', fontSize: 11, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}>
                      ORBITRON v2
                    </span>
                    <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1.5 }}>PROTOCOL</span>
                  </div>
                )
              : (
                  <div style={{ padding: '0 20px', color: offlineColor, fontSize: 10, letterSpacing: 2, fontWeight: linkLost ? 700 : 400 }}>
                    {offlineText}
                  </div>
                )
          }
        </div>

        {/* 6. TLE badge — demoted to far right, small */}
        <div style={{ padding: '0 14px', borderLeft: '1px solid var(--border)', flexShrink: 0 }}>
          <TleSelector source={tleSource} group={tleGroup} count={tleCount} />
        </div>

      </div>
    </div>
  )
}
