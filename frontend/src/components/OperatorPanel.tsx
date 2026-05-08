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
      setStatus({
        online: hr.status === 'fulfilled',
        bufferHasData: h?.buffer_has_data ?? false,
        inContact,
        aosSec,
      })
      timer = setTimeout(poll, 5000)
    }
    poll()
    return () => clearTimeout(timer)
  }, [path, windowsPath])

  return status
}

function StatusDot({ online, active }: { online: boolean; active?: boolean }) {
  const color = !online ? 'var(--red)' : active ? 'var(--green)' : 'var(--amber)'
  return (
    <div style={{
      width: 6, height: 6, borderRadius: '50%', flexShrink: 0,
      background: color,
      boxShadow: online ? `0 0 4px ${color}` : 'none',
    }} />
  )
}

function fmtAOS(sec: number): string {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? `${m}m ${String(s).padStart(2, '0')}s` : `${s}s`
}

interface Props {
  primaryOnline: boolean
  primarySatId: string
  tleSource: 'sim' | 'tle'
}

export function OperatorPanel({ primaryOnline, primarySatId, tleSource }: Props) {
  const [open, setOpen] = useState(true)
  const [tab, setTab] = useState<Tab>('FLEET')
  const [events, setEvents] = useState<AutonomousEvent[]>([])

  const relay1 = useRelayStatus('/relay/health', '/relay/windows')
  const relay2 = useRelayStatus('/relay2/health', '/relay2/windows')

  
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/events')
        if (res.ok) {
          const data = await res.json() as AutonomousEvent[] | null
          if (Array.isArray(data)) setEvents(data.slice(0, 20))
        }
      } catch {  }
      timer = setTimeout(poll, 3000)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])

  const nonNominalCount = events.filter(e => e.class !== 'NOMINAL').length

  const relayDetail = (r: RelayStatus) => {
    if (!r.online) return { label: 'OFFLINE', color: 'var(--red)' }
    if (r.inContact) return { label: 'IN CONTACT', color: 'var(--green)' }
    if (r.bufferHasData) return {
      label: r.aosSec !== null ? `BUFFERED · AOS ${fmtAOS(r.aosSec)}` : 'BUFFERED',
      color: 'var(--cyan)',
    }
    return {
      label: r.aosSec !== null ? `EMPTY · AOS ${fmtAOS(r.aosSec)}` : 'EMPTY',
      color: 'var(--amber)',
    }
  }

  const r1 = relayDetail(relay1)
  const r2 = relayDetail(relay2)

  const TABS: Tab[] = ['FLEET', 'EVENTS']

  return (
    <div style={{
      position: 'fixed', top: 48, left: 0, bottom: 0, zIndex: 109,
      width: open ? 320 : 32,
      transition: 'width 0.2s ease',
      background: 'rgba(4,13,28,0.96)',
      borderRight: '1px solid var(--border)',
      backdropFilter: 'blur(10px)',
      overflow: 'hidden',
      display: 'flex', flexDirection: 'column',
    }}>
      
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
            <div key={i} style={{
              width: 14, height: 1.5,
              background: open ? 'var(--cyan)' : 'var(--text-dim)',
              borderRadius: 1,
            }} />
          ))}
        </div>
        {open && (
          <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 3, whiteSpace: 'nowrap' }}>
            OPERATOR
          </span>
        )}
      </button>

      
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
                  fontSize: 7, display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontWeight: 700,
                }}>
                  {nonNominalCount > 9 ? '9+' : nonNominalCount}
                </span>
              )}
            </button>
          ))}
        </div>
      )}

      
      {open && (
        <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>

          
          {tab === 'FLEET' && (
            <div>
              
              <div style={{ padding: '6px 14px 4px', color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>
                PRIMARY SPACECRAFT
              </div>
              <div
                onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: primarySatId }))}
                style={{
                  padding: '8px 14px', borderBottom: '1px solid var(--bg-dark)',
                  cursor: 'pointer', display: 'flex', alignItems: 'flex-start', gap: 10,
                }}
              >
                <StatusDot online={primaryOnline} active={primaryOnline} />
                <div style={{ flex: 1 }}>
                  <div style={{ color: 'var(--cyan)', fontSize: 10, fontWeight: 700, letterSpacing: 1, marginBottom: 2 }}>
                    {tleSource === 'tle' ? primarySatId : 'Satellite-1'}
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 0.5, marginBottom: 4 }}>
                    {tleSource === 'tle' ? '~500 km · 97.4° · PLANET LABS TLE' : '500 km · 97.4° · SUN-SYNC SIM'}
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ color: primaryOnline ? 'var(--green)' : 'var(--red)', fontSize: 8, fontWeight: 700, letterSpacing: 1 }}>
                      {primaryOnline ? '● LIVE' : '● OFFLINE'}
                    </span>
                    <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>
                      {tleSource === 'tle' ? 'ENRICHED TELEMETRY' : 'FULL TELEMETRY'}
                    </span>
                  </div>
                </div>
              </div>

              
              <div style={{ padding: '10px 14px 4px', color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>
                ISL RELAY NODES
              </div>
              {([
                { id: 'Satellite-2', name: 'Satellite-2', orbit: '700 km · 98° · SUN-SYNC', status: r1, relay: relay1 },
                { id: 'Satellite-3', name: 'Satellite-3', orbit: '550 km · 53°', status: r2, relay: relay2 },
              ]).map(node => (
                <div
                  key={node.id}
                  onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))}
                  style={{
                    padding: '8px 14px', borderBottom: '1px solid var(--bg-dark)',
                    cursor: 'pointer', display: 'flex', alignItems: 'flex-start', gap: 10,
                  }}
                >
                  <StatusDot online={node.relay.online} active={node.relay.inContact} />
                  <div style={{ flex: 1 }}>
                    <div style={{ color: 'var(--teal)', fontSize: 10, fontWeight: 700, letterSpacing: 1, marginBottom: 2 }}>
                      {node.name}
                    </div>
                    <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 0.5, marginBottom: 4 }}>
                      {node.orbit}
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span style={{ color: node.status.color, fontSize: 8, fontWeight: 700, letterSpacing: 1 }}>
                        {node.status.label}
                      </span>
                    </div>
                  </div>
                </div>
              ))}

              
              <div style={{
                margin: '10px 14px 0',
                padding: '8px 10px',
                background: 'var(--bg-deep)',
                border: '1px solid var(--border)',
                borderRadius: 3,
              }}>
                <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2, marginBottom: 4 }}>CONSTELLATION</div>
                <div style={{ color: 'var(--text-mid)', fontSize: 8, lineHeight: 1.5 }}>
                  {tleSource === 'tle'
                    ? `Live Planet Labs constellation. Primary: ${primarySatId}. Satellite-2 / Satellite-3 are ISL relay nodes.`
                    : 'Satellite-1 simulated primary + Satellite-2 / Satellite-3 ISL relay nodes. Switch to live TLE data via the KPI bar.'
                  }
                </div>
              </div>
            </div>
          )}

          
          {tab === 'EVENTS' && (
            <div>
              <div style={{ padding: '0 14px 8px', color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>
                AUTONOMOUS EVENTS · LAST {events.length}
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
                        background: isAnomalous && i === 0
                          ? `color-mix(in srgb, ${color} 6%, transparent)`
                          : 'transparent',
                        opacity: 1 - i * 0.04,
                      }}
                    >
                      <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', marginBottom: 2 }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 7, fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}>
                          {ts}
                        </span>
                        <span style={{ color, fontSize: 8, fontWeight: 700, letterSpacing: 1 }}>
                          {ev.class}
                        </span>
                      </div>
                      <div style={{ color: 'var(--text-mid)', fontSize: 8, lineHeight: 1.4 }}>
                        {ev.action}
                      </div>
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
