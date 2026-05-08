import { useState, useEffect } from 'react'
import type { AutonomousEvent } from '../types'

type Tab = 'FLEET' | 'EVENTS'

const CLASS_COLOR: Record<string, string> = {
  NOMINAL: '#40d080',
  ECLIPSE_ENTRY: '#50b8d0',
  ECLIPSE_COMPUTE: '#9060d0',
  POWER_ANOMALY: '#e05050',
  THERMAL_EVENT: '#d08030',
  ATTITUDE_INSTABILITY: '#d08030',
  RF_DEGRADATION: '#d08030',
}

interface RelayStatus {
  online: boolean
  bufferHasData: boolean
  inContact: boolean
  aosSec: number | null
}

function useRelayStatus(path: string, windowsPath: string): RelayStatus {
  const [s, setS] = useState<RelayStatus>({ online: false, bufferHasData: false, inContact: false, aosSec: null })
  useEffect(() => {
    let t: ReturnType<typeof setTimeout>
    async function poll() {
      const [hr, wr] = await Promise.allSettled([
        fetch(path).then(r => r.ok ? r.json() : Promise.reject()),
        fetch(windowsPath).then(r => r.ok ? r.json() : Promise.reject()),
      ])
      const h = hr.status === 'fulfilled' ? hr.value as { buffer_has_data: boolean } : null
      const w = wr.status === 'fulfilled' ? wr.value as { in_contact: boolean; windows: { aos: string }[] } : null
      const inContact = w?.in_contact ?? false
      let aosSec: number | null = null
      if (!inContact && w?.windows?.length)
        aosSec = Math.max(0, Math.round((new Date(w.windows[0].aos).getTime() - Date.now()) / 1000))
      setS({ online: hr.status === 'fulfilled', bufferHasData: h?.buffer_has_data ?? false, inContact, aosSec })
      t = setTimeout(poll, 5000)
    }
    poll()
    return () => clearTimeout(t)
  }, [path, windowsPath])
  return s
}

function fmtAOS(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${String(s).padStart(2, '0')}s`
  return `${s}s`
}

function relayState(r: RelayStatus): { color: string; label: string; sub: string | null } {
  if (!r.online)       return { color: '#e05050', label: 'OFFLINE',    sub: null }
  if (r.inContact)     return { color: '#40d080', label: 'IN CONTACT', sub: null }
  if (r.bufferHasData) return { color: '#50b8d0', label: 'BUFFERED',   sub: r.aosSec != null ? fmtAOS(r.aosSec) : null }
  return                      { color: '#6a8090', label: 'IDLE',       sub: r.aosSec != null ? fmtAOS(r.aosSec) : null }
}

// 5-bar link quality indicator
function LinkBars({ online }: { online: boolean }) {
  const lit = online ? 5 : 0
  return (
    <div style={{ display: 'flex', gap: 2, alignItems: 'flex-end', height: 9 }}>
      {[1,2,3,4,5].map(i => (
        <div key={i} style={{
          width: 3,
          height: 2 + i * 1.4,
          background: i <= lit ? '#40d080' : '#0e1a24',
          borderRadius: 1,
          transition: 'background 0.3s',
        }} />
      ))}
    </div>
  )
}

// Section header: LABEL ──────── N  (with optional collapse toggle)
function Section({ label, count, open, onToggle }: { label: string; count: number; open?: boolean; onToggle?: () => void }) {
  return (
    <div
      onClick={onToggle}
      style={{
        display: 'flex', alignItems: 'center', gap: 8,
        padding: '18px 14px 7px',
        cursor: onToggle ? 'pointer' : 'default',
        userSelect: 'none',
      }}
    >
      <span style={{ color: '#7a9ab0', fontSize: 7, fontWeight: 700, letterSpacing: 2.5, whiteSpace: 'nowrap' }}>
        {label.toUpperCase()}
      </span>
      <div style={{ flex: 1, height: 1, background: '#0e1a24' }} />
      <span style={{ color: '#6a8090', fontSize: 7, letterSpacing: 1 }}>{count}</span>
      {onToggle && (
        <span style={{ color: '#6a8090', fontSize: 9, lineHeight: 1 }}>{open ? '▾' : '▸'}</span>
      )}
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
  const [open,       setOpen]       = useState(true)
  const [tab,        setTab]        = useState<Tab>('FLEET')
  const [selectedId, setSelectedId] = useState(primarySatId)
  const [relaysOpen, setRelaysOpen] = useState(true)
  const [gsOpen,     setGsOpen]     = useState(true)
  const [events,     setEvents]     = useState<AutonomousEvent[]>([])

  // Track globe selection
  useEffect(() => {
    const h = (e: Event) => setSelectedId((e as CustomEvent<string>).detail)
    window.addEventListener('select-sat', h)
    return () => window.removeEventListener('select-sat', h)
  }, [])

  const relay1 = useRelayStatus('/relay1/health', '/relay1/windows')
  const relay2 = useRelayStatus('/relay2/health', '/relay2/windows')
  const relay3 = useRelayStatus('/relay3/health', '/relay3/windows')
  const relay4 = useRelayStatus('/relay4/health', '/relay4/windows')
  const relay5 = useRelayStatus('/relay5/health', '/relay5/windows')
  const relay6 = useRelayStatus('/relay6/health', '/relay6/windows')

  useEffect(() => {
    let t: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const r = await fetch('/events')
        if (r.ok) { const d = await r.json(); if (Array.isArray(d)) setEvents(d.slice(0, 20)) }
      } catch { /* silent */ }
      t = setTimeout(poll, 3000)
    }
    poll()
    return () => clearTimeout(t)
  }, [])

  const nonNominalCount = events.filter(e => e.class !== 'NOMINAL').length

  const primaries = [
    {
      id: primarySatId,
      name: tleSource === 'tle' ? primarySatId : 'Satellite-1',
      meta: tleSource === 'tle' ? `LEO · 408 km · 51.6° · ${(tleGroup ?? 'TLE').toUpperCase()}` : 'LEO · 408 km · 51.6° · SIM',
      online: primaryOnline,
    },
    {
      id: 'CSS (TIANHE)',
      name: 'Tiangong',
      meta: 'LEO · 380 km · 41.5° · TLE',
      online: primaryOnline,
    },
  ]

  const relays = [
    { id: 'NOAA-20',     name: 'NOAA-20',     status: relay1 },
    { id: 'Sentinel-2A', name: 'Sentinel-2A', status: relay2 },
    { id: 'Landsat-9',   name: 'Landsat-9',   status: relay3 },
    { id: 'Aqua',        name: 'Aqua',        status: relay4 },
    { id: 'Terra',       name: 'Terra',       status: relay5 },
    { id: 'NOAA-19',     name: 'NOAA-19',     status: relay6 },
  ]

  const groundStations = [
    { name: 'Nairobi',      coords: '1.3°S  36.8°E' },
    { name: 'Svalbard',     coords: '78.2°N  15.6°E' },
    { name: 'Punta Arenas', coords: '53.2°S  70.9°W' },
  ]

  return (
    <div style={{
      position: 'fixed', top: 48, left: 0, bottom: 0, zIndex: 109,
      width: open ? 310 : 32,
      transition: 'width 0.2s ease',
      background: 'linear-gradient(180deg, #060f1e 0%, #040c18 100%)',
      borderRight: '1px solid #0e1a28',
      boxShadow: open ? '4px 0 24px rgba(0,0,0,0.4)' : 'none',
      overflow: 'hidden',
      display: 'flex', flexDirection: 'column',
    }}>

      {/* Toggle */}
      <button onClick={() => setOpen(o => !o)} title={open ? 'Collapse' : 'Expand'} style={{
        height: 40, flexShrink: 0, background: 'none', border: 'none',
        borderBottom: '1px solid #0e1a28', cursor: 'pointer',
        display: 'flex', alignItems: 'center', gap: 8,
        padding: open ? '0 14px' : '0', justifyContent: open ? 'flex-start' : 'center', width: '100%',
      }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 3, flexShrink: 0 }}>
          {[0,1,2].map(i => <div key={i} style={{ width: 13, height: 1.5, background: '#6a8090', borderRadius: 1 }} />)}
        </div>
        {open && <span style={{ color: '#6a8090', fontSize: 8, letterSpacing: 3 }}>OPERATOR</span>}
      </button>

      {/* Tabs */}
      {open && (
        <div style={{ display: 'flex', borderBottom: '1px solid #0e1a28', flexShrink: 0 }}>
          {(['FLEET', 'EVENTS'] as Tab[]).map(t => (
            <button key={t} onClick={() => setTab(t)} style={{
              flex: 1, padding: '8px 0', position: 'relative',
              background: 'none', border: 'none', cursor: 'pointer',
              borderBottom: tab === t ? '1px solid #4a7090' : '1px solid transparent',
              color: tab === t ? '#7a9ab0' : '#6a8090',
              fontSize: 8, letterSpacing: 2, fontFamily: 'inherit', fontWeight: 600,
            }}>
              {t}
              {t === 'EVENTS' && nonNominalCount > 0 && (
                <span style={{
                  position: 'absolute', top: 5, right: 8,
                  background: '#b03030', color: '#fff',
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
              {/* ── Primary Spacecraft ── */}
              <Section label="Primary Spacecraft" count={primaries.length} />

              {primaries.map(p => {
                const active = p.id === selectedId
                const liveColor = p.online ? '#40d080' : '#e05050'
                const isPrimary = p.id === primarySatId
                return (
                  <div
                    key={p.id}
                    onClick={() => {
                      setSelectedId(p.id)
                      window.dispatchEvent(new CustomEvent('select-sat', { detail: p.id }))
                    }}
                    style={{
                      padding: active ? '16px 14px 16px 12px' : '12px 14px 12px 14px',
                      borderBottom: '1px solid #0a1520',
                      borderLeft: active ? `2px solid ${liveColor}` : '2px solid transparent',
                      background: active ? `color-mix(in srgb, ${liveColor} 5%, #060f1e)` : 'transparent',
                      cursor: 'pointer',
                      transition: 'all 0.15s',
                    }}
                    onMouseEnter={e => { if (!active) e.currentTarget.style.background = '#0a1828' }}
                    onMouseLeave={e => { if (!active) e.currentTarget.style.background = 'transparent' }}
                  >
                    {/* Name row */}
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 7 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                        <div style={{
                          width: active ? 7 : 5, height: active ? 7 : 5,
                          borderRadius: '50%', background: liveColor, flexShrink: 0,
                          boxShadow: active ? `0 0 6px ${liveColor}` : 'none',
                          transition: 'all 0.15s',
                        }} />
                        <span style={{
                          color: active ? '#e0eef8' : isPrimary ? '#a0b4c4' : '#7a8e9e',
                          fontSize: active ? 12 : 10,
                          fontWeight: active ? 700 : 500,
                          letterSpacing: active ? 0.4 : 0.2,
                          transition: 'all 0.15s',
                        }}>
                          {p.name}
                        </span>
                      </div>
                      <span style={{ color: liveColor, fontSize: 7, fontWeight: 700, letterSpacing: 1.5 }}>
                        {p.online ? 'LIVE' : 'OFFLINE'}
                      </span>
                    </div>

                    {/* Meta row — always visible */}
                    <div style={{ color: '#708898', fontSize: 7, letterSpacing: 0.4, paddingLeft: 14, marginBottom: 7 }}>
                      {p.meta}
                    </div>

                    {/* Link bars — always visible */}
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, paddingLeft: 14 }}>
                      <LinkBars online={p.online} />
                      <span style={{ color: '#708898', fontSize: 7, letterSpacing: 1.5 }}>LINK</span>
                    </div>
                  </div>
                )
              })}

              {/* ── ISL Relay Mesh ── */}
              <Section
                label="ISL Relay Mesh"
                count={relays.length}
                open={relaysOpen}
                onToggle={() => setRelaysOpen(o => !o)}
              />

              {relaysOpen && relays.map(node => {
                const s = relayState(node.status)
                const active = node.id === selectedId
                return (
                  <div
                    key={node.id}
                    onClick={() => {
                      setSelectedId(node.id)
                      window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))
                    }}
                    style={{
                      padding: '11px 14px',
                      borderBottom: '1px solid #0a1520',
                      borderLeft: active ? `2px solid ${s.color}` : '2px solid transparent',
                      background: active ? `color-mix(in srgb, ${s.color} 4%, #060f1e)` : 'transparent',
                      cursor: 'pointer',
                    }}
                    onMouseEnter={e => { if (!active) e.currentTarget.style.background = '#0a1828' }}
                    onMouseLeave={e => { if (!active) e.currentTarget.style.background = 'transparent' }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                        <div style={{ width: 5, height: 5, borderRadius: '50%', background: s.color, flexShrink: 0 }} />
                        <span style={{ color: active ? '#a0b0c0' : '#6a8090', fontSize: 9, fontWeight: 500, letterSpacing: 0.3 }}>
                          {node.name}
                        </span>
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        {s.sub && (
                          <span style={{ color: '#6a8090', fontSize: 7, fontVariantNumeric: 'tabular-nums' }}>
                            {s.sub}
                          </span>
                        )}
                        <span style={{ color: s.color, fontSize: 7, fontWeight: 700, letterSpacing: 1 }}>
                          {s.label}
                        </span>
                      </div>
                    </div>
                  </div>
                )
              })}

              {/* ── Ground Stations ── */}
              <Section
                label="Ground Stations"
                count={groundStations.length}
                open={gsOpen}
                onToggle={() => setGsOpen(o => !o)}
              />

              {gsOpen && groundStations.map(gs => (
                <div key={gs.name} style={{ padding: '9px 14px 9px 16px', borderBottom: '1px solid #0a1520', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                    <div style={{ width: 4, height: 4, background: '#6a8090', borderRadius: 1, flexShrink: 0 }} />
                    <span style={{ color: '#7a9ab0', fontSize: 8, fontWeight: 500 }}>{gs.name}</span>
                  </div>
                  <span style={{ color: '#6a8898', fontSize: 7, fontVariantNumeric: 'tabular-nums', letterSpacing: 0.3 }}>
                    {gs.coords}
                  </span>
                </div>
              ))}

              <div style={{ height: 20 }} />
            </>
          )}

          {tab === 'EVENTS' && (
            <>
              <Section label="Autonomous Events" count={events.length} />
              {events.length === 0 ? (
                <div style={{ padding: '28px 14px', color: '#6a8090', fontSize: 9, textAlign: 'center' }}>
                  No events recorded
                </div>
              ) : events.map((ev, i) => {
                const color = CLASS_COLOR[ev.class] ?? '#6a8090'
                const isAnomalous = ev.class !== 'NOMINAL'
                const ts = new Date(ev.at).toISOString().slice(11, 19) + 'Z'
                return (
                  <div key={i} style={{
                    padding: '8px 14px',
                    borderBottom: '1px solid #0a1520',
                    borderLeft: isAnomalous ? `2px solid ${color}` : '2px solid transparent',
                    background: isAnomalous && i === 0 ? `color-mix(in srgb, ${color} 5%, transparent)` : 'transparent',
                    opacity: Math.max(0.35, 1 - i * 0.045),
                  }}>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 3 }}>
                      <span style={{ color: '#6a8090', fontSize: 7, fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}>{ts}</span>
                      <span style={{ color, fontSize: 8, fontWeight: 700, letterSpacing: 0.8 }}>{ev.class}</span>
                    </div>
                    <div style={{ color: '#708898', fontSize: 8, lineHeight: 1.5 }}>{ev.action}</div>
                  </div>
                )
              })}
              <div style={{ height: 20 }} />
            </>
          )}
        </div>
      )}
    </div>
  )
}
