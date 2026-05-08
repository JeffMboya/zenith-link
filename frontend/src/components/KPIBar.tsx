import { useEffect, useState, useRef } from 'react'
import type { LinkMetrics, SatelliteState } from '../types'
import type { SatCommandStateMap } from '../hooks/useSatCommandState'
import { TleSelector } from './TleSelector'

const INFERENCE_POLL_MS = 2_000

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

interface DeliveryState {
  aosMs:     number | null
  durationMs: number
  relay:     string
  gs:        string
  flowing:   boolean
}

function useNextDelivery() {
  const [delivery, setDelivery] = useState<DeliveryState | null>(null)
  const [label, setLabel]       = useState('—')
  const [detail, setDetail]     = useState('')
  const [color, setColor]       = useState('var(--text-dim)')
  const [urgent, setUrgent]     = useState(false)

  // Consume delivery-state dispatched by useContactWindows in App.tsx (single 36-fetch poll)
  useEffect(() => {
    const h = (e: Event) => setDelivery((e as CustomEvent<DeliveryState>).detail)
    window.addEventListener('delivery-state', h)
    return () => window.removeEventListener('delivery-state', h)
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

export function KPIBar({ metrics, satellite, connected, linkLost, tleSource, tleGroup, tleCount, selectedSatId, cmdState }: Props) {
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

  // Derive alive dot appearance from command state of selected satellite
  const satMode     = cmdState.modes[selectedSatId]
  const isRebooting = !!cmdState.rebootPhase[selectedSatId]
  const isSafe      = satMode === 'safe'
  const dotColor    = !connected ? 'var(--red)' : isSafe ? 'var(--amber)' : 'var(--green)'
  const dotGlow     = !connected ? 'var(--red)' : isSafe ? 'var(--amber)' : 'var(--green)'
  const dotAnim     = connected && !isSafe && !isRebooting
    ? 'liveDotBlink 1s step-start infinite'
    : connected && isSafe
      ? 'liveDotBlink 2s step-start infinite'  // slow amber pulse in safe mode
      : 'none'
  const liveLabel   = connected
    ? (isSafe ? 'SAFE MODE' : isRebooting ? 'REBOOTING' : 'LIVE')
    : 'OFFLINE'
  const liveTextColor = !connected ? 'var(--red)' : isSafe ? 'var(--amber)' : 'var(--green)'
  // During reboot, show blank metrics
  const showMetrics = !!metrics && !isRebooting

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
            background: dotColor,
            boxShadow: `0 0 8px ${dotGlow}`,
            flexShrink: 0,
            animation: dotAnim,
          }} />
          <span style={{ color: liveTextColor, fontSize: 12, fontWeight: 700, letterSpacing: 1.5 }}>
            {liveLabel}
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
          {showMetrics
            ? kpis(metrics!).map(({ label, value, color, nacks }) => (
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
            : isRebooting
              ? (
                  <div style={{ padding: '0 20px', color: 'var(--amber)', fontSize: 10, letterSpacing: 2, fontWeight: 700 }}>
                    REBOOTING OBC...
                  </div>
                )
              : connected
              ? (
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
