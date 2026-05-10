import { useState, useEffect } from 'react'
import { Satellite, Radio, MapPin } from 'lucide-react'
import type { AutonomousEvent } from '../types'
import { PRIMARY_SATELLITES } from '../data/primarySatellites'
import { GROUND_STATIONS } from '../data/groundStations'
import type { SatCommandStateMap } from '../hooks/useSatCommandState'

type Tab = 'FLEET' | 'EVENTS'

const CLASS_COLOR: Record<string, string> = {
  NOMINAL:              '#40d080',
  ECLIPSE_ENTRY:        '#50b8d0',
  ECLIPSE_COMPUTE:      '#9060d0',
  POWER_ANOMALY:        '#e05050',
  THERMAL_EVENT:        '#d08030',
  ATTITUDE_INSTABILITY: '#d08030',
  RF_DEGRADATION:       '#d08030',
}

interface RelayStatus {
  online: boolean; bufferHasData: boolean; inContact: boolean; aosSec: number | null
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
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${String(s).padStart(2, '0')}s`
  return `${s}s`
}

function relayState(r: RelayStatus): { color: string; label: string; sub: string | null } {
  if (!r.online)       return { color: '#e05050', label: 'OFFLINE',    sub: null }
  if (r.inContact)     return { color: '#40d080', label: 'IN CONTACT', sub: null }
  if (r.bufferHasData) return { color: '#50b8d0', label: 'BUFFERED',   sub: r.aosSec != null ? fmtAOS(r.aosSec) : null }
  return                      { color: '#8ab0c8', label: 'IDLE',       sub: r.aosSec != null ? fmtAOS(r.aosSec) : null }
}

// Signal bars driven by RSSI value (-120 to -60 dBm → 0 to 5 bars)
function rssiToBars(rssi: number | undefined): number {
  if (rssi === undefined || rssi === 0) return 0
  // Typical satellite RSSI range: -95 (very weak) to -75 (strong)
  const clamped = Math.max(-100, Math.min(-60, rssi))
  const normalized = (clamped + 100) / 40  // 0.0 to 1.0
  return Math.max(1, Math.round(normalized * 5))
}

// Per-satellite RSSI — subscribed via custom event from CommandEngine / WS state
function useSatRSSI(satId: string): number | undefined {
  const [rssi, setRSSI] = useState<number | undefined>(undefined)
  useEffect(() => {
    const h = (e: Event) => {
      const { id, rssi: v } = (e as CustomEvent<{ id: string; rssi: number }>).detail
      if (id === satId) setRSSI(v)
    }
    window.addEventListener('sat-rssi', h)
    return () => window.removeEventListener('sat-rssi', h)
  }, [satId])
  return rssi
}

function LinkBars({ satId, online }: { satId: string; online: boolean }) {
  const rssi = useSatRSSI(satId)
  const lit = online ? rssiToBars(rssi) : 0
  return (
    <div style={{ display: 'flex', gap: 2, alignItems: 'flex-end', height: 9 }}>
      {[1,2,3,4,5].map(i => (
        <div key={i} style={{
          width: 3,
          height: 2 + i * 1.4,
          background: i <= lit ? '#40d080' : '#0e1a24',
          borderRadius: 1,
          transition: 'background 0.4s',
        }} />
      ))}
    </div>
  )
}

function Section({
  label, count, open, onToggle, icon,
}: {
  label: string; count: number; open?: boolean; onToggle?: () => void; icon?: React.ReactNode
}) {
  return (
    <div
      onClick={onToggle}
      role={onToggle ? 'button' : undefined}
      tabIndex={onToggle ? 0 : undefined}
      onKeyDown={e => onToggle && e.key === 'Enter' && onToggle()}
      aria-expanded={open}
      style={{
        display: 'flex', alignItems: 'center', gap: 8,
        padding: '18px 14px 7px',
        cursor: onToggle ? 'pointer' : 'default',
        userSelect: 'none',
      }}
    >
      {icon && <span style={{ color: '#90b4cc', display: 'flex', alignItems: 'center' }}>{icon}</span>}
      <span style={{ color: '#90b4cc', fontSize: 10, fontWeight: 700, letterSpacing: 2.5, whiteSpace: 'nowrap' }}>
        {label.toUpperCase()}
      </span>
      <div style={{ flex: 1, height: 1, background: '#0e1a24' }} />
      <span style={{ color: '#8ab0c8', fontSize: 10, letterSpacing: 1 }}>{count}</span>
      {onToggle && (
        <span style={{ color: '#8ab0c8', fontSize: 12, lineHeight: 1 }}>{open ? '▾' : '▸'}</span>
      )}
    </div>
  )
}

interface Props {
  satConnected:  Record<string, boolean>
  primarySatIds: string[]
  tleSource:     'sim' | 'tle'
  tleGroup?:     string
  cmdState:      SatCommandStateMap
}

interface SatEvent extends AutonomousEvent {
  satId?: string
}

export function OperatorPanel({ satConnected, primarySatIds, cmdState }: Props) {
  const [open,       setOpen]       = useState(true)
  const [tab,        setTab]        = useState<Tab>('FLEET')
  const [selectedId, setSelectedId] = useState(primarySatIds[0])
  const [relaysOpen, setRelaysOpen] = useState(true)
  const [gsOpen,     setGsOpen]     = useState(true)
  const [events,     setEvents]     = useState<SatEvent[]>([])

  useEffect(() => {
    const h = (e: Event) => setSelectedId((e as CustomEvent<string>).detail)
    window.addEventListener('select-sat', h)
    return () => window.removeEventListener('select-sat', h)
  }, [])

  // Listen for tab switch requests from MissionFooter alert badges
  useEffect(() => {
    const h = (e: Event) => setTab((e as CustomEvent<Tab>).detail)
    window.addEventListener('switch-tab', h)
    return () => window.removeEventListener('switch-tab', h)
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
        if (r.ok) {
          const d = await r.json()
          if (Array.isArray(d)) {
            const mapped: SatEvent[] = d.slice(0, 20).map((ev: SatEvent) => ({
              ...ev,
              // Default satId to first primary if not present (legacy events)
              satId: ev.satId ?? primarySatIds[0],
            }))
            setEvents(mapped)
          }
        }
      } catch { /* silent */ }
      t = setTimeout(poll, 3000)
    }
    poll()
    return () => clearTimeout(t)
  }, [primarySatIds])

  const anomalousEvents = events.filter(e => e.class !== 'NOMINAL')
  const nonNominalCount = anomalousEvents.length

  // Publish alert counts to MissionFooter
  useEffect(() => {
    const error   = events.filter(e => e.class === 'POWER_ANOMALY' || e.class === 'ATTITUDE_INSTABILITY').length
    const warning = events.filter(e => e.class === 'THERMAL_EVENT' || e.class === 'RF_DEGRADATION').length
    const info    = events.filter(e => e.class === 'ECLIPSE_ENTRY' || e.class === 'ECLIPSE_COMPUTE').length
    window.dispatchEvent(new CustomEvent('alert-counts', { detail: { error, warning, info } }))
  }, [events])

  const relays = [
    { id: 'NOAA-20',     name: 'NOAA-20',     status: relay1 },
    { id: 'Sentinel-2A', name: 'Sentinel-2A', status: relay2 },
    { id: 'Landsat-9',   name: 'Landsat-9',   status: relay3 },
    { id: 'Aqua',        name: 'Aqua',        status: relay4 },
    { id: 'Terra',       name: 'Terra',       status: relay5 },
    { id: 'NOAA-19',     name: 'NOAA-19',     status: relay6 },
  ]

  // Collapsed sidebar icon color based on section health
  const primaryHealth = primarySatIds.some(id => !satConnected[id]) ? '#e05050'
    : primarySatIds.some(id => satConnected[id] === false) ? '#d08030' : '#90b4cc'
  const relayHealth = relays.some(r => !r.status.online) ? '#d08030' : '#90b4cc'

  return (
    <div
      role="navigation"
      aria-label="Operator fleet panel"
      style={{
        position: 'fixed', top: 96, left: 0, bottom: 28, zIndex: 109,
        width: open ? 310 : 52,
        transition: 'width 0.2s ease',
        background: 'linear-gradient(180deg, #060f1e 0%, #040c18 100%)',
        borderRight: '1px solid #0e1a28',
        boxShadow: open ? '4px 0 24px rgba(0,0,0,0.4)' : 'none',
        overflow: 'hidden',
        display: 'flex', flexDirection: 'column',
      }}
    >
      {/* Toggle button */}
      <button
        onClick={() => setOpen(o => !o)}
        title={open ? 'Collapse sidebar' : 'Expand sidebar'}
        aria-expanded={open}
        aria-label={open ? 'Collapse fleet panel' : 'Expand fleet panel'}
        style={{
          height: 40, flexShrink: 0, background: 'none', border: 'none',
          borderBottom: '1px solid #0e1a28', cursor: 'pointer',
          display: 'flex', alignItems: 'center', gap: 8,
          padding: open ? '0 14px' : '0 0',
          justifyContent: open ? 'flex-start' : 'center',
          width: '100%',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 3, flexShrink: 0 }}>
          {[0,1,2].map(i => <div key={i} style={{ width: 13, height: 1.5, background: '#8ab0c8', borderRadius: 1 }} />)}
        </div>
        {open && <span style={{ color: '#8ab0c8', fontSize: 11, letterSpacing: 3 }}>OPERATOR</span>}
      </button>

      {/* Collapsed icon strip */}
      {!open && (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', paddingTop: 14, gap: 22 }}>
          <div title="Primary Spacecraft" style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 5 }}>
            <Satellite size={18} color={primaryHealth} strokeWidth={1.5} aria-label="Primary Spacecraft" />
            <div style={{ display: 'flex', flexDirection: 'row', gap: 3 }}>
              {primarySatIds.map(id => (
                <div key={id} style={{
                  width: 5, height: 5, borderRadius: '50%',
                  background: satConnected[id] ? '#40d080' : '#e05050',
                }} />
              ))}
            </div>
          </div>
          <span title="ISL Relay Mesh"><Radio size={18} color={relayHealth} strokeWidth={1.5} aria-label="ISL Relay Mesh" /></span>
          <span title="Ground Stations"><MapPin size={18} color="#90b4cc" strokeWidth={1.5} aria-label="Ground Stations" /></span>
        </div>
      )}

      {/* Tabs */}
      {open && (
        <div style={{ display: 'flex', gap: 6, padding: '8px 10px', borderBottom: '1px solid #0e1a28', flexShrink: 0, background: '#040c18' }}>
          {(['FLEET', 'EVENTS'] as Tab[]).map(t => {
            const active = tab === t
            return (
              <button
                key={t}
                onClick={() => setTab(t)}
                role="tab"
                aria-selected={active}
                style={{
                  flex: 1, padding: '6px 0', position: 'relative',
                  background: active ? '#0e1e30' : 'transparent',
                  border: active ? '1px solid #1e3a50' : '1px solid #0e1a28',
                  borderRadius: 5, cursor: 'pointer',
                  color: active ? '#c0d8ec' : '#6a8898',
                  fontSize: 10, letterSpacing: 2, fontFamily: 'inherit',
                  fontWeight: active ? 700 : 500,
                  display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 7,
                  transition: 'all 0.15s',
                }}
              >
                <span>{t}</span>
                {t === 'EVENTS' && nonNominalCount > 0 && (
                  <span style={{
                    background: '#c03030', color: '#fff',
                    fontSize: 9, fontWeight: 700,
                    padding: '1px 5px', borderRadius: 10, lineHeight: 1.4,
                  }}>
                    {nonNominalCount > 9 ? '9+' : nonNominalCount}
                  </span>
                )}
              </button>
            )
          })}
        </div>
      )}

      {open && (
        <div style={{ flex: 1, overflowY: 'auto' }} role="tabpanel">
          {tab === 'FLEET' && (
            <>
              <Section
                label="Primary Spacecraft"
                count={primarySatIds.length}
                icon={<Satellite size={12} strokeWidth={1.5} />}
              />

              {PRIMARY_SATELLITES.map(sat => {
                const active      = sat.tleName === selectedId
                const online      = !!satConnected[sat.tleName]
                const satMode     = cmdState.modes[sat.tleName]
                const isRebooting = !!cmdState.rebootPhase[sat.tleName]
                const isExecuting = !!cmdState.executingSats[sat.tleName]
                const isSafe      = satMode === 'safe'
                const liveColor   = !online ? '#e05050' : isSafe ? '#f0a800' : isRebooting ? '#e05050' : '#40d080'
                const statusLabel = !online ? 'OFFLINE' : isRebooting ? 'REBOOTING' : isSafe ? 'SAFE MODE' : 'LIVE'
                const dotAnim     = online && !isSafe && !isRebooting && active ? 'liveDotBlink 1s step-start infinite'
                  : online && isSafe ? 'liveDotBlink 2s step-start infinite' : 'none'
                return (
                  <div
                    key={sat.tleName}
                    onClick={() => {
                      setSelectedId(sat.tleName)
                      window.dispatchEvent(new CustomEvent('select-sat', { detail: sat.tleName }))
                    }}
                    role="button"
                    tabIndex={0}
                    aria-label={`Select ${sat.displayName}, ${statusLabel}`}
                    onKeyDown={e => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        setSelectedId(sat.tleName)
                        window.dispatchEvent(new CustomEvent('select-sat', { detail: sat.tleName }))
                      }
                    }}
                    style={{
                      padding: active ? '16px 14px 16px 12px' : '12px 14px 12px 14px',
                      borderBottom: '1px solid #0a1520',
                      borderLeft: active ? `2px solid ${liveColor}` : '2px solid transparent',
                      background: active ? `color-mix(in srgb, ${liveColor} 5%, #060f1e)` : 'transparent',
                      cursor: 'pointer', transition: 'all 0.15s',
                    }}
                    onMouseEnter={e => { if (!active) e.currentTarget.style.background = '#0a1828' }}
                    onMouseLeave={e => { if (!active) e.currentTarget.style.background = 'transparent' }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 7 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                        <div style={{
                          width: active ? 7 : 5, height: active ? 7 : 5,
                          borderRadius: '50%', background: liveColor, flexShrink: 0,
                          boxShadow: active ? `0 0 6px ${liveColor}` : 'none',
                          animation: dotAnim,
                          transition: 'background 0.3s, all 0.15s',
                        }} />
                        <span style={{
                          color: active ? '#e0eef8' : '#a0b4c4',
                          fontSize: active ? 15 : 13,
                          fontWeight: active ? 700 : 500,
                          letterSpacing: active ? 0.4 : 0.2,
                          transition: 'all 0.15s',
                        }}>
                          {sat.displayName}
                        </span>
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        {isExecuting && (
                          <span style={{ color: 'var(--amber)', fontSize: 8, letterSpacing: 1, border: '1px solid var(--amber)', padding: '1px 4px', borderRadius: 2, animation: 'spin 1.2s linear infinite', display: 'inline-block' }}>⟳</span>
                        )}
                        <span style={{ color: liveColor, fontSize: 10, fontWeight: 700, letterSpacing: 1.5 }}>
                          {statusLabel}
                        </span>
                      </div>
                    </div>
                    <div style={{ color: '#8ab0c0', fontSize: 10, letterSpacing: 0.4, paddingLeft: 14, marginBottom: 7 }}>
                      LEO · {sat.altKm} km · {sat.incDeg}° · TLE
                      {(cmdState.deployedAgents[sat.tleName] ?? []).length > 0 && (
                        <span style={{ color: 'var(--teal)', marginLeft: 6, fontSize: 8 }}>
                          {(cmdState.deployedAgents[sat.tleName] ?? []).length} agent{(cmdState.deployedAgents[sat.tleName] ?? []).length > 1 ? 's' : ''} active
                        </span>
                      )}
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, paddingLeft: 14 }}>
                      <LinkBars satId={sat.tleName} online={online} />
                      <span style={{ color: '#8ab0c0', fontSize: 10, letterSpacing: 1.5 }}>LINK</span>
                    </div>
                  </div>
                )
              })}

              <Section
                label="ISL Relay Mesh"
                count={relays.length}
                open={relaysOpen}
                onToggle={() => setRelaysOpen(o => !o)}
                icon={<Radio size={12} strokeWidth={1.5} />}
              />

              {relaysOpen && relays.map(node => {
                const s      = relayState(node.status)
                const active = node.id === selectedId
                return (
                  <div
                    key={node.id}
                    onClick={() => {
                      setSelectedId(node.id)
                      window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))
                    }}
                    role="button"
                    tabIndex={0}
                    aria-label={`Select relay ${node.name}, ${s.label}`}
                    onKeyDown={e => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        setSelectedId(node.id)
                        window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))
                      }
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
                        <span style={{ color: active ? '#a0b0c0' : '#8ab0c8', fontSize: 12, fontWeight: 500, letterSpacing: 0.3 }}>
                          {node.name}
                        </span>
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        {s.sub && <span style={{ color: '#8ab0c8', fontSize: 10, fontVariantNumeric: 'tabular-nums' }}>{s.sub}</span>}
                        <span style={{ color: s.color, fontSize: 10, fontWeight: 700, letterSpacing: 1 }}>{s.label}</span>
                      </div>
                    </div>
                  </div>
                )
              })}

              <Section
                label="Ground Stations"
                count={GROUND_STATIONS.length}
                open={gsOpen}
                onToggle={() => setGsOpen(o => !o)}
                icon={<MapPin size={12} strokeWidth={1.5} />}
              />

              {gsOpen && GROUND_STATIONS.map(gs => (
                <div
                  key={gs.name}
                  style={{ padding: '9px 14px 9px 16px', borderBottom: '1px solid #0a1520', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                    <div style={{ width: 4, height: 4, background: gs.color, borderRadius: 1, flexShrink: 0 }} />
                    <span style={{ color: '#90b4cc', fontSize: 11, fontWeight: 500 }}>{gs.name}</span>
                  </div>
                  <span style={{ color: '#8ab0c0', fontSize: 10, fontVariantNumeric: 'tabular-nums', letterSpacing: 0.3 }}>
                    {gs.lat > 0 ? `${gs.lat.toFixed(1)}°N` : `${Math.abs(gs.lat).toFixed(1)}°S`}{' '}
                    {gs.lon > 0 ? `${gs.lon.toFixed(1)}°E` : `${Math.abs(gs.lon).toFixed(1)}°W`}
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
                <div style={{ padding: '28px 14px', color: '#8ab0c8', fontSize: 12, textAlign: 'center' }}>
                  No events recorded
                </div>
              ) : events.map((ev, i) => {
                const color      = CLASS_COLOR[ev.class] ?? '#8ab0c8'
                const isAnomalous = ev.class !== 'NOMINAL'
                const ts         = new Date(ev.at).toISOString().slice(11, 19) + 'Z'
                const satDisplay = ev.satId
                  ? PRIMARY_SATELLITES.find(s => s.tleName === ev.satId)?.displayName ?? ev.satId.split(' ')[0]
                  : null
                return (
                  <div key={i} style={{
                    padding: '8px 14px',
                    borderBottom: '1px solid #0a1520',
                    borderLeft: isAnomalous ? `2px solid ${color}` : '2px solid transparent',
                    background: isAnomalous && i === 0 ? `color-mix(in srgb, ${color} 5%, transparent)` : 'transparent',
                    opacity: Math.max(0.35, 1 - i * 0.045),
                  }}>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 3 }}>
                      <span style={{ color: '#8ab0c8', fontSize: 10, fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}>{ts}</span>
                      <span style={{ color, fontSize: 11, fontWeight: 700, letterSpacing: 0.8 }}>{ev.class}</span>
                      {satDisplay && (
                        <span style={{ color: 'var(--cyan)', fontSize: 9, letterSpacing: 1, border: '1px solid var(--cyan)', padding: '0 4px', borderRadius: 2 }}>
                          {satDisplay}
                        </span>
                      )}
                    </div>
                    <div style={{ color: '#8ab0c0', fontSize: 11, lineHeight: 1.5 }}>{ev.action}</div>
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
