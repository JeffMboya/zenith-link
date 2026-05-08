import { useState, useEffect } from 'react'
import type { AutonomousEvent } from '../types'

type Tab = 'FLEET' | 'EVENTS'

const CLASS_COLOR: Record<string, string> = {
  NOMINAL: 'var(--green)',
  ECLIPSE_ENTRY: 'var(--cyan)',
  ECLIPSE_COMPUTE: 'var(--purple)',
  POWER_ANOMALY: 'var(--red)',
  THERMAL_EVENT: 'var(--amber)',
  ATTITUDE_INSTABILITY: 'var(--amber)',
  RF_DEGRADATION: 'var(--amber)',
}

interface RelayStatus {
  online: boolean
  bufferHasData: boolean
  inContact: boolean
  aosSec: number | null
}

function useRelayStatus(path: string, windowsPath: string): RelayStatus {
  const [status, setStatus] = useState<RelayStatus>({
    online: false, bufferHasData: false, inContact: false, aosSec: null,
  })
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      const [hr, wr] = await Promise.allSettled([
        fetch(path).then(r => r.ok ? r.json() : Promise.reject()),
        fetch(windowsPath).then(r => r.ok ? r.json() : Promise.reject()),
      ])
      const h = hr.status === 'fulfilled' ? hr.value as { buffer_has_data: boolean } : null
      const w = wr.status === 'fulfilled' ? wr.value as { in_contact: boolean; windows: { aos: string }[] } : null
      const inContact = w?.in_contact ?? false
      let aosSec: number | null = null
      if (!inContact && w?.windows?.length) {
        aosSec = Math.max(0, Math.round((new Date(w.windows[0].aos).getTime() - Date.now()) / 1000))
      }
      setStatus({ online: hr.status === 'fulfilled', bufferHasData: h?.buffer_has_data ?? false, inContact, aosSec })
      timer = setTimeout(poll, 5000)
    }
    poll()
    return () => clearTimeout(timer)
  }, [path, windowsPath])
  return status
}

function fmtAOS(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${String(s).padStart(2, '0')}s`
  return `${s}s`
}

function SectionHeader({ label, count, accent }: { label: string; count: number; accent: string }) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 8,
      padding: '10px 14px 6px',
      borderTop: '1px solid var(--border)',
      marginTop: 4,
    }}>
      <div style={{ width: 2, height: 14, background: accent, borderRadius: 1, flexShrink: 0 }} />
      <span style={{ color: accent, fontSize: 8, fontWeight: 700, letterSpacing: 2, flex: 1 }}>{label}</span>
      <span style={{
        background: `color-mix(in srgb, ${accent} 15%, transparent)`,
        color: accent, fontSize: 7, fontWeight: 700,
        padding: '1px 5px', borderRadius: 8, letterSpacing: 1,
      }}>{count}</span>
    </div>
  )
}

function StatusBadge({ online, inContact, bufferHasData, aosSec }: RelayStatus) {
  let label: string
  let color: string
  if (!online) { label = 'OFFLINE'; color = 'var(--red)' }
  else if (inContact) { label = 'IN CONTACT'; color = 'var(--green)' }
  else if (bufferHasData) {
    label = aosSec !== null ? `BUFFERED · ${fmtAOS(aosSec)}` : 'BUFFERED'
    color = 'var(--cyan)'
  } else {
    label = aosSec !== null ? `IDLE · ${fmtAOS(aosSec)}` : 'IDLE'
    color = 'var(--amber)'
  }
  return (
    <span style={{
      color, fontSize: 7, fontWeight: 700, letterSpacing: 1,
      background: `color-mix(in srgb, ${color} 12%, transparent)`,
      padding: '2px 6px', borderRadius: 3, whiteSpace: 'nowrap', flexShrink: 0,
    }}>{label}</span>
  )
}

interface Props {
  primaryOnline: boolean
  primarySatId: string
  tleSource: 'sim' | 'tle'
  tleGroup?: string
}

export function OperatorPanel({ primaryOnline, primarySatId, tleSource, tleGroup }: Props) {
  const [open, setOpen] = useState(true)
  const [tab, setTab] = useState<Tab>('FLEET')
  const [events, setEvents] = useState<AutonomousEvent[]>([])

  const relay1 = useRelayStatus('/relay1/health', '/relay1/windows')
  const relay2 = useRelayStatus('/relay2/health', '/relay2/windows')
  const relay3 = useRelayStatus('/relay3/health', '/relay3/windows')
  const relay4 = useRelayStatus('/relay4/health', '/relay4/windows')
  const relay5 = useRelayStatus('/relay5/health', '/relay5/windows')
  const relay6 = useRelayStatus('/relay6/health', '/relay6/windows')

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/events')
        if (res.ok) {
          const data = await res.json() as AutonomousEvent[] | null
          if (Array.isArray(data)) setEvents(data.slice(0, 20))
        }
      } catch { /* silent */ }
      timer = setTimeout(poll, 3000)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])

  const nonNominalCount = events.filter(e => e.class !== 'NOMINAL').length
  const TABS: Tab[] = ['FLEET', 'EVENTS']

  const primaries = [
    {
      id: primarySatId,
      label: tleSource === 'tle' ? primarySatId : 'Satellite-1',
      orbit: tleSource === 'tle' ? `408 km · 51.6° · ${(tleGroup ?? 'TLE').toUpperCase()}` : '408 km · 51.6° · SIM',
      online: primaryOnline,
    },
    {
      id: 'CSS (TIANHE)',
      label: 'Tiangong',
      orbit: '380 km · 41.5° · LIVE TLE',
      online: primaryOnline,
    },
  ]

  const relays = [
    { id: 'NOAA-20',     name: 'NOAA-20',     orbit: '824 km · 98.7°', status: relay1 },
    { id: 'Sentinel-2A', name: 'Sentinel-2A', orbit: '786 km · 98.6°', status: relay2 },
    { id: 'Landsat-9',   name: 'Landsat-9',   orbit: '705 km · 98.2°', status: relay3 },
    { id: 'Aqua',        name: 'Aqua',        orbit: '705 km · 98.2°', status: relay4 },
    { id: 'Terra',       name: 'Terra',       orbit: '705 km · 98.2°', status: relay5 },
    { id: 'NOAA-19',     name: 'NOAA-19',     orbit: '870 km · 99.1°', status: relay6 },
  ]

  return (
    <div style={{
      position: 'fixed', top: 48, left: 0, bottom: 0, zIndex: 109,
      width: open ? 300 : 32,
      transition: 'width 0.2s ease',
      background: 'rgba(4,13,28,0.97)',
      borderRight: '1px solid var(--border)',
      backdropFilter: 'blur(10px)',
      overflow: 'hidden',
      display: 'flex', flexDirection: 'column',
    }}>

      {/* Collapse toggle */}
      <button
        onClick={() => setOpen(o => !o)}
        title={open ? 'Collapse panel' : 'Expand panel'}
        style={{
          height: 40, flexShrink: 0,
          background: 'none', border: 'none', borderBottom: '1px solid var(--border)',
          cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8,
          padding: open ? '0 12px' : '0',
          justifyContent: open ? 'flex-start' : 'center',
          width: '100%',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2.5, flexShrink: 0 }}>
          {[0, 1, 2].map(i => (
            <div key={i} style={{ width: 14, height: 1.5, background: open ? 'var(--cyan)' : 'var(--text-dim)', borderRadius: 1 }} />
          ))}
        </div>
        {open && (
          <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 3, whiteSpace: 'nowrap' }}>
            OPERATOR
          </span>
        )}
      </button>

      {/* Tabs */}
      {open && (
        <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
          {TABS.map(t => (
            <button
              key={t}
              onClick={() => setTab(t)}
              style={{
                flex: 1, padding: '7px 0', position: 'relative',
                background: 'none', border: 'none', cursor: 'pointer',
                borderBottom: tab === t ? '2px solid var(--cyan)' : '2px solid transparent',
                color: tab === t ? 'var(--cyan)' : 'var(--text-dim)',
                fontSize: 8, letterSpacing: 2, fontFamily: 'inherit', fontWeight: tab === t ? 700 : 400,
              }}
            >
              {t}
              {t === 'EVENTS' && nonNominalCount > 0 && (
                <span style={{
                  position: 'absolute', top: 4, right: 4,
                  width: 14, height: 14, borderRadius: '50%',
                  background: 'var(--red)', color: '#fff',
                  fontSize: 7, display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: 700,
                }}>
                  {nonNominalCount > 9 ? '9+' : nonNominalCount}
                </span>
              )}
            </button>
          ))}
        </div>
      )}

      {open && (
        <div style={{ flex: 1, overflowY: 'auto' }}>

          {tab === 'FLEET' && (
            <>
              {/* Primary spacecraft */}
              <SectionHeader label="PRIMARY SPACECRAFT" count={primaries.length} accent="var(--cyan)" />
              {primaries.map(p => (
                <div
                  key={p.id}
                  onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: p.id }))}
                  style={{
                    padding: '10px 14px',
                    borderBottom: '1px solid var(--bg-dark)',
                    borderLeft: '2px solid transparent',
                    cursor: 'pointer',
                    transition: 'background 0.1s',
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(0,224,240,0.04)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                >
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 4 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                      <div style={{
                        width: 7, height: 7, borderRadius: '50%', flexShrink: 0,
                        background: p.online ? 'var(--green)' : 'var(--red)',
                        boxShadow: p.online ? '0 0 5px var(--green)' : 'none',
                      }} />
                      <span style={{ color: 'var(--cyan)', fontSize: 11, fontWeight: 700, letterSpacing: 0.5 }}>
                        {p.label}
                      </span>
                    </div>
                    <span style={{
                      color: p.online ? 'var(--green)' : 'var(--red)',
                      fontSize: 7, fontWeight: 700, letterSpacing: 1,
                      background: p.online ? 'rgba(0,232,120,0.12)' : 'rgba(240,80,80,0.12)',
                      padding: '2px 6px', borderRadius: 3,
                    }}>
                      {p.online ? 'LIVE' : 'OFFLINE'}
                    </span>
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 0.5, paddingLeft: 14 }}>
                    {p.orbit}
                  </div>
                </div>
              ))}

              {/* ISL relay mesh */}
              <SectionHeader label="ISL RELAY MESH" count={relays.length} accent="var(--teal)" />
              {relays.map(node => (
                <div
                  key={node.id}
                  onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))}
                  style={{
                    padding: '8px 14px',
                    borderBottom: '1px solid var(--bg-dark)',
                    cursor: 'pointer',
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(0,180,160,0.04)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                >
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 3 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                      <div style={{
                        width: 6, height: 6, borderRadius: '50%', flexShrink: 0,
                        background: !node.status.online ? 'var(--red)' : node.status.inContact ? 'var(--green)' : 'var(--cyan)',
                        boxShadow: node.status.online ? `0 0 4px ${node.status.inContact ? 'var(--green)' : 'var(--cyan)'}` : 'none',
                      }} />
                      <span style={{ color: 'var(--teal)', fontSize: 10, fontWeight: 700, letterSpacing: 0.5 }}>
                        {node.name}
                      </span>
                    </div>
                    <StatusBadge {...node.status} />
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 0.5, paddingLeft: 13 }}>
                    {node.orbit} · SUN-SYNC
                  </div>
                </div>
              ))}

              {/* Ground stations summary */}
              <SectionHeader label="GROUND STATIONS" count={3} accent="var(--amber)" />
              {[
                { name: 'Nairobi', coords: '1.3°S · 36.8°E' },
                { name: 'Svalbard', coords: '78.2°N · 15.6°E' },
                { name: 'Punta Arenas', coords: '53.2°S · 70.9°W' },
              ].map(gs => (
                <div key={gs.name} style={{ padding: '7px 14px', borderBottom: '1px solid var(--bg-dark)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                    <div style={{ width: 5, height: 5, borderRadius: 1, background: 'var(--amber)', flexShrink: 0 }} />
                    <span style={{ color: 'var(--text-body)', fontSize: 9, fontWeight: 600, letterSpacing: 0.5 }}>{gs.name}</span>
                  </div>
                  <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 0.5 }}>{gs.coords}</span>
                </div>
              ))}
            </>
          )}

          {tab === 'EVENTS' && (
            <div>
              <div style={{ padding: '10px 14px 6px', borderTop: '1px solid var(--border)', marginTop: 4, display: 'flex', alignItems: 'center', gap: 8 }}>
                <div style={{ width: 2, height: 14, background: 'var(--purple)', borderRadius: 1 }} />
                <span style={{ color: 'var(--purple)', fontSize: 8, fontWeight: 700, letterSpacing: 2, flex: 1 }}>AUTONOMOUS EVENTS</span>
                <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>LAST {events.length}</span>
              </div>
              {events.length === 0 ? (
                <div style={{ padding: '20px 14px', color: 'var(--text-dim)', fontSize: 9, textAlign: 'center' }}>
                  NO EVENTS YET
                </div>
              ) : (
                events.map((ev, i) => {
                  const color = CLASS_COLOR[ev.class] ?? 'var(--text-dim)'
                  const isAnomalous = ev.class !== 'NOMINAL'
                  const ts = new Date(ev.at).toISOString().slice(11, 19) + 'Z'
                  return (
                    <div
                      key={i}
                      style={{
                        padding: '6px 14px',
                        borderBottom: '1px solid var(--bg-dark)',
                        borderLeft: isAnomalous ? `2px solid ${color}` : '2px solid transparent',
                        background: isAnomalous && i === 0 ? `color-mix(in srgb, ${color} 6%, transparent)` : 'transparent',
                        opacity: 1 - i * 0.04,
                      }}
                    >
                      <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', marginBottom: 2 }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 7, fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}>{ts}</span>
                        <span style={{ color, fontSize: 8, fontWeight: 700, letterSpacing: 1 }}>{ev.class}</span>
                      </div>
                      <div style={{ color: 'var(--text-mid)', fontSize: 8, lineHeight: 1.4 }}>{ev.action}</div>
                    </div>
                  )
                })
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
