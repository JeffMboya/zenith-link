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

// Status derives a dot colour and a short label — the only coloured elements per row.
function statusInfo(r: RelayStatus): { color: string; label: string; sub: string | null } {
  if (!r.online)    return { color: '#e05050', label: 'OFFLINE',    sub: null }
  if (r.inContact)  return { color: '#40d080', label: 'IN CONTACT', sub: null }
  if (r.bufferHasData) return {
    color: '#50b8d0',
    label: 'BUFFERED',
    sub: r.aosSec !== null ? `AOS ${fmtAOS(r.aosSec)}` : null,
  }
  return {
    color: '#9090a0',
    label: 'IDLE',
    sub: r.aosSec !== null ? `AOS ${fmtAOS(r.aosSec)}` : null,
  }
}

// Thin section divider — structure only, no colour accent.
function Divider({ label, count }: { label: string; count: number }) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 8,
      padding: '14px 14px 5px',
    }}>
      <span style={{ color: '#3a4a5c', fontSize: 7, fontWeight: 700, letterSpacing: 2, flex: 1, textTransform: 'uppercase' }}>
        {label}
      </span>
      <span style={{ color: '#2a3a4c', fontSize: 7, letterSpacing: 1 }}>{count}</span>
    </div>
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
      name: tleSource === 'tle' ? primarySatId : 'Satellite-1',
      meta: tleSource === 'tle' ? `408 km · 51.6° · ${(tleGroup ?? 'TLE').toUpperCase()}` : '408 km · 51.6° · SIM',
      online: primaryOnline,
    },
    {
      id: 'CSS (TIANHE)',
      name: 'Tiangong',
      meta: '380 km · 41.5° · TLE',
      online: primaryOnline,
    },
  ]

  const relays = [
    { id: 'NOAA-20',     name: 'NOAA-20',     meta: '824 km · 98.7°', status: relay1 },
    { id: 'Sentinel-2A', name: 'Sentinel-2A', meta: '786 km · 98.6°', status: relay2 },
    { id: 'Landsat-9',   name: 'Landsat-9',   meta: '705 km · 98.2°', status: relay3 },
    { id: 'Aqua',        name: 'Aqua',        meta: '705 km · 98.2°', status: relay4 },
    { id: 'Terra',       name: 'Terra',       meta: '705 km · 98.2°', status: relay5 },
    { id: 'NOAA-19',     name: 'NOAA-19',     meta: '870 km · 99.1°', status: relay6 },
  ]

  const groundStations = [
    { name: 'Nairobi',       coords: '1.3°S  36.8°E' },
    { name: 'Svalbard',      coords: '78.2°N  15.6°E' },
    { name: 'Punta Arenas',  coords: '53.2°S  70.9°W' },
  ]

  return (
    <div style={{
      position: 'fixed', top: 48, left: 0, bottom: 0, zIndex: 109,
      width: open ? 290 : 32,
      transition: 'width 0.2s ease',
      background: '#050e1c',
      borderRight: '1px solid #111e2e',
      overflow: 'hidden',
      display: 'flex', flexDirection: 'column',
    }}>

      {/* Toggle */}
      <button
        onClick={() => setOpen(o => !o)}
        title={open ? 'Collapse' : 'Expand'}
        style={{
          height: 40, flexShrink: 0,
          background: 'none', border: 'none', borderBottom: '1px solid #111e2e',
          cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8,
          padding: open ? '0 14px' : '0',
          justifyContent: open ? 'flex-start' : 'center', width: '100%',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 3, flexShrink: 0 }}>
          {[0,1,2].map(i => (
            <div key={i} style={{ width: 13, height: 1.5, background: '#2a3a50', borderRadius: 1 }} />
          ))}
        </div>
        {open && <span style={{ color: '#2a3a50', fontSize: 8, letterSpacing: 3 }}>OPERATOR</span>}
      </button>

      {/* Tabs */}
      {open && (
        <div style={{ display: 'flex', borderBottom: '1px solid #111e2e', flexShrink: 0 }}>
          {TABS.map(t => (
            <button key={t} onClick={() => setTab(t)} style={{
              flex: 1, padding: '8px 0', position: 'relative',
              background: 'none', border: 'none', cursor: 'pointer',
              borderBottom: tab === t ? '1px solid #4a7090' : '1px solid transparent',
              color: tab === t ? '#8ab0c8' : '#2e4050',
              fontSize: 8, letterSpacing: 2, fontFamily: 'inherit', fontWeight: 600,
            }}>
              {t}
              {t === 'EVENTS' && nonNominalCount > 0 && (
                <span style={{
                  position: 'absolute', top: 4, right: 8,
                  background: '#c03030', color: '#fff',
                  fontSize: 7, fontWeight: 700, padding: '1px 4px', borderRadius: 3,
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
              {/* Primaries */}
              <Divider label="Primary Spacecraft" count={primaries.length} />
              {primaries.map(p => {
                const col = p.online ? '#40d080' : '#e05050'
                return (
                  <div
                    key={p.id}
                    onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: p.id }))}
                    style={{ padding: '9px 14px', borderBottom: '1px solid #0a1520', cursor: 'pointer' }}
                    onMouseEnter={e => (e.currentTarget.style.background = '#0a1828')}
                    onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <div style={{ width: 6, height: 6, borderRadius: '50%', background: col, flexShrink: 0 }} />
                        <span style={{ color: '#c8d8e8', fontSize: 11, fontWeight: 600, letterSpacing: 0.3 }}>
                          {p.name}
                        </span>
                      </div>
                      <span style={{ color: col, fontSize: 7, fontWeight: 700, letterSpacing: 1.5 }}>
                        {p.online ? 'LIVE' : 'OFFLINE'}
                      </span>
                    </div>
                    <div style={{ color: '#2e4050', fontSize: 7, letterSpacing: 0.5, marginTop: 3, paddingLeft: 14 }}>
                      {p.meta}
                    </div>
                  </div>
                )
              })}

              {/* Relays */}
              <Divider label="ISL Relay Mesh" count={relays.length} />
              {relays.map(node => {
                const s = statusInfo(node.status)
                return (
                  <div
                    key={node.id}
                    onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))}
                    style={{ padding: '7px 14px', borderBottom: '1px solid #0a1520', cursor: 'pointer' }}
                    onMouseEnter={e => (e.currentTarget.style.background = '#0a1828')}
                    onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <div style={{ width: 5, height: 5, borderRadius: '50%', background: s.color, flexShrink: 0 }} />
                        <span style={{ color: '#8a9aaa', fontSize: 10, fontWeight: 600, letterSpacing: 0.3 }}>
                          {node.name}
                        </span>
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        {s.sub && (
                          <span style={{ color: '#2e4050', fontSize: 7, letterSpacing: 0.5 }}>{s.sub}</span>
                        )}
                        <span style={{ color: s.color, fontSize: 7, fontWeight: 700, letterSpacing: 1 }}>
                          {s.label}
                        </span>
                      </div>
                    </div>
                    <div style={{ color: '#1e2e3e', fontSize: 7, marginTop: 2, paddingLeft: 13 }}>
                      {node.meta} · SUN-SYNC
                    </div>
                  </div>
                )
              })}

              {/* Ground Stations */}
              <Divider label="Ground Stations" count={groundStations.length} />
              {groundStations.map(gs => (
                <div key={gs.name} style={{ padding: '6px 14px', borderBottom: '1px solid #0a1520', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <div style={{ width: 4, height: 4, background: '#2e4050', borderRadius: 1, flexShrink: 0 }} />
                    <span style={{ color: '#607080', fontSize: 9, fontWeight: 600, letterSpacing: 0.3 }}>{gs.name}</span>
                  </div>
                  <span style={{ color: '#1e2e3e', fontSize: 7, letterSpacing: 0.5, fontVariantNumeric: 'tabular-nums' }}>{gs.coords}</span>
                </div>
              ))}
            </>
          )}

          {tab === 'EVENTS' && (
            <>
              <Divider label="Autonomous Events" count={events.length} />
              {events.length === 0 ? (
                <div style={{ padding: '24px 14px', color: '#1e2e3e', fontSize: 9, textAlign: 'center' }}>
                  No events yet
                </div>
              ) : (
                events.map((ev, i) => {
                  const color = CLASS_COLOR[ev.class] ?? '#607080'
                  const isAnomalous = ev.class !== 'NOMINAL'
                  const ts = new Date(ev.at).toISOString().slice(11, 19) + 'Z'
                  return (
                    <div key={i} style={{
                      padding: '7px 14px',
                      borderBottom: '1px solid #0a1520',
                      borderLeft: isAnomalous ? `2px solid ${color}` : '2px solid transparent',
                      opacity: Math.max(0.4, 1 - i * 0.045),
                    }}>
                      <div style={{ display: 'flex', gap: 8, alignItems: 'baseline', marginBottom: 2 }}>
                        <span style={{ color: '#1e2e3e', fontSize: 7, fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}>{ts}</span>
                        <span style={{ color, fontSize: 8, fontWeight: 700, letterSpacing: 0.8 }}>{ev.class}</span>
                      </div>
                      <div style={{ color: '#3a5060', fontSize: 8, lineHeight: 1.4 }}>{ev.action}</div>
                    </div>
                  )
                })
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}
