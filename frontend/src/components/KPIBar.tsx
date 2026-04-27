import { useEffect, useState } from 'react'
import type { LinkMetrics } from '../types'
import { TleSelector } from './TleSelector'

const LINK_COST_PER_MB = 15
const WINDOWS_POLL_MS = 30_000

interface Props {
  metrics: LinkMetrics | null
  connected: boolean
  linkLost: boolean
  tleSource: 'sim' | 'tle'
  tleGroup: string
  tleCount: number
}

interface KPI {
  label: string
  value: string
  color: string
}

interface ContactWindow {
  aos: string
  duration_sec: number
  max_elevation_deg: number
}

function useNextPass() {
  const [label, setLabel] = useState<string | null>(null)
  const [color, setColor] = useState('var(--text-dim)')
  const [urgent, setUrgent] = useState(false)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>

    async function poll() {
      try {
        const res = await fetch('/windows')
        if (!res.ok) return
        const data = await res.json() as { windows: ContactWindow[] }
        if (!data.windows?.length) { setLabel('NO PASS'); setColor('var(--text-dim)'); setUrgent(false); return }

        const w = data.windows[0]
        const aosMs = new Date(w.aos).getTime()
        const now = Date.now()
        const diffSec = Math.round((aosMs - now) / 1000)

        if (diffSec < 0) {
          const losSec = Math.round((aosMs + w.duration_sec * 1000 - now) / 1000)
          if (losSec > 0) { setLabel(`IN PASS ${losSec}s`); setColor('var(--green)'); setUrgent(true) }
          else { setLabel('PASS DONE'); setColor('var(--text-dim)'); setUrgent(false) }
        } else {
          const h = Math.floor(diffSec / 3600)
          const m = Math.floor((diffSec % 3600) / 60)
          const s = diffSec % 60
          const elev = w.max_elevation_deg.toFixed(0)
          setLabel(h > 0 ? `${h}h ${m}m · ${elev}°` : m > 0 ? `${m}m ${s}s · ${elev}°` : `${s}s · ${elev}°`)
          const isUrgent = diffSec < 300
          setColor(isUrgent ? 'var(--amber)' : 'var(--cyan)')
          setUrgent(isUrgent)
        }
      } catch { /* keep last */ }
      timer = setTimeout(poll, WINDOWS_POLL_MS)
    }

    poll()
    return () => clearTimeout(timer)
  }, [])

  return { label, color, urgent }
}

function kpis(m: LinkMetrics): KPI[] {
  const zenithMB = m.bytes_received_zenith / 1e6
  const jsonMB = m.bytes_equivalent_json / 1e6
  const savedMB = Math.max(0, jsonMB - zenithMB)
  const eff = jsonMB > 0 ? ((savedMB / jsonMB) * 100).toFixed(1) : '0.0'
  const effNum = parseFloat(eff)
  return [
    { label: 'PACKETS ACK', value: m.packets_received.toLocaleString(), color: 'var(--cyan)' },
    { label: 'NACKs', value: m.nacks_issued.toLocaleString(), color: m.nacks_issued > 0 ? 'var(--amber)' : 'var(--green)' },
    { label: 'DROPPED', value: m.packets_dropped_simulated.toLocaleString(), color: 'var(--amber)' },
    { label: 'EFFICIENCY', value: `${eff}%`, color: effNum >= 80 ? 'var(--green)' : effNum >= 60 ? 'var(--amber)' : 'var(--red)' },
    { label: 'BYTES SAVED', value: `${(savedMB * 1000).toFixed(1)} KB`, color: 'var(--teal)' },
    { label: 'COST SAVED', value: `$${(savedMB * LINK_COST_PER_MB).toFixed(4)}`, color: 'var(--purple)' },
  ]
}

export function KPIBar({ metrics, connected, linkLost, tleSource, tleGroup, tleCount }: Props) {
  const { label: passLabel, color: passColor, urgent: passUrgent } = useNextPass()

  const offlineText = linkLost ? 'LINK LOST' : 'AWAITING LINK ACQUISITION...'
  const offlineColor = linkLost ? 'var(--red)' : 'var(--text-dim)'

  return (
    <div style={{
      position: 'fixed', top: 0, left: 0, right: 0, zIndex: 100,
      display: 'flex', alignItems: 'center', gap: 1,
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

      {/* KPI tiles */}
      <div style={{ flex: 1, display: 'flex' }}>
        {metrics
          ? kpis(metrics).map(({ label, value, color }) => (
              <div key={label} style={{
                flex: 1, padding: '6px 12px', borderRight: '1px solid var(--bg-dark)',
                display: 'flex', flexDirection: 'column', alignItems: 'center',
              }}>
                <span style={{ color, fontSize: 16, fontWeight: 700, lineHeight: 1.2 }}>{value}</span>
                <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 2, marginTop: 2 }}>{label}</span>
              </div>
            ))
          : <div style={{ padding: '12px 20px', color: offlineColor, fontSize: 10, letterSpacing: 2, fontWeight: linkLost ? 700 : 400 }}>
              {offlineText}
            </div>
        }
      </div>

      {/* NEXT PASS — elevated tile */}
      <div style={{
        padding: '6px 16px', borderLeft: '1px solid var(--border)',
        display: 'flex', flexDirection: 'column', alignItems: 'center', minWidth: 118,
        background: passUrgent ? 'rgba(240,168,0,0.07)' : 'transparent',
        borderTop: passUrgent ? '2px solid var(--amber)' : '2px solid transparent',
      }}>
        <span style={{ color: passColor, fontSize: 14, fontWeight: 700, lineHeight: 1.2, letterSpacing: 1 }}>
          {passLabel ?? '—'}
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
  )
}
