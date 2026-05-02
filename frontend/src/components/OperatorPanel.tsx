import { useState, useCallback, useEffect } from 'react'
import type { AutonomousEvent } from '../types'

type Tab = 'FLEET' | 'MARKET' | 'EVENTS'

interface WorkloadCard {
  profileId: number
  name: string
  description: string
  sizeBytes: number
  cpuPct: number
  memMB: number
  badge?: string
}

const WORKLOADS: WorkloadCard[] = [
  {
    profileId: 0x01,
    name: 'anomaly-detector-v1',
    description: '26KB statistical health monitor. Rolling z-score baseline, downlink prioritization by class.',
    sizeBytes: 26112,
    cpuPct: 4,
    memMB: 3,
    badge: 'STL-01',
  },
  {
    profileId: 0x02,
    name: 'telemetry-compressor-v1',
    description: '12KB delta-compression agent. Reduces downlink bandwidth on constrained RF links. 72% compression vs raw JSON.',
    sizeBytes: 12288,
    cpuPct: 2,
    memMB: 2,
  },
  {
    profileId: 0x03,
    name: 'edge-inference-agent-v1',
    description: '8KB SLM-class behavioral classifier. Autonomous diagnostics — interprets inputs, responds to conditions.',
    sizeBytes: 8192,
    cpuPct: 6,
    memMB: 4,
    badge: 'STL-02',
  },
  {
    profileId: 0x04,
    name: 'orbit-predictor-v1',
    description: '18KB autonomous orbit event scheduler. Predicts eclipse, perigee, and apogee windows. Drives ECLIPSE_COMPUTE mode.',
    sizeBytes: 18432,
    cpuPct: 3,
    memMB: 2,
  },
  {
    profileId: 0x05,
    name: 'federated-learner-v1',
    description: '40KB federated gradient accumulator. Distributed model updates across satellite network.',
    sizeBytes: 40960,
    cpuPct: 12,
    memMB: 8,
    badge: 'FEDERATED',
  },
  {
    profileId: 0x06,
    name: 'link-optimizer-v1',
    description: '15KB dynamic downlink scheduler. Prioritizes frames by anomaly class and link margin.',
    sizeBytes: 15360,
    cpuPct: 3,
    memMB: 2,
  },
]

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

function Meter({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = Math.min(100, (value / max) * 100)
  return (
    <div style={{ width: 40, height: 3, background: 'var(--bg-dark)', borderRadius: 2, overflow: 'hidden' }}>
      <div style={{ width: `${pct}%`, height: '100%', background: color, borderRadius: 2 }} />
    </div>
  )
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
}

export function OperatorPanel({ primaryOnline }: Props) {
  const [open, setOpen] = useState(true)
  const [tab, setTab] = useState<Tab>('FLEET')
  const [deployLog, setDeployLog] = useState<{ profileId: number; msg: string; ok: boolean } | null>(null)
  const [deploying, setDeploying] = useState<number | null>(null)
  const [events, setEvents] = useState<AutonomousEvent[]>([])

  const relay1 = useRelayStatus('/relay/health', '/relay/windows')
  const relay2 = useRelayStatus('/relay2/health', '/relay2/windows')

  // Events polling — active regardless of tab so badge count stays live
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/events')
        if (res.ok) {
          const data = await res.json() as AutonomousEvent[] | null
          if (Array.isArray(data)) setEvents(data.slice(0, 20))
        }
      } catch { /* keep last */ }
      timer = setTimeout(poll, 3000)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])

  const deploy = useCallback(async (card: WorkloadCard) => {
    if (deploying !== null) return
    setDeploying(card.profileId)
    setDeployLog(null)
    try {
      const payloadB64 = btoa(String.fromCharCode(card.profileId))
      const res = await fetch('/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command_id: 0x05, payload_b64: payloadB64 }),
      })
      const body = await res.json() as { accepted?: boolean; message?: string; error?: string }
      setDeployLog({
        profileId: card.profileId,
        msg: body.accepted ? (body.message ?? 'DEPLOYED') : (body.error ?? body.message ?? 'REJECTED'),
        ok: !!body.accepted,
      })
    } catch {
      setDeployLog({ profileId: card.profileId, msg: 'SEND FAILED — link down?', ok: false })
    } finally {
      setDeploying(null)
    }
  }, [deploying])

  const linkSec = (bytes: number) => (bytes / (20 * 1024)).toFixed(1)

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

  const TABS: Tab[] = ['FLEET', 'MARKET', 'EVENTS']

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
      {/* Toggle button — always visible in the collapsed strip */}
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

      {/* Tab bar — only when open */}
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

      {/* Content area */}
      {open && (
        <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>

          {/* ── FLEET ── */}
          {tab === 'FLEET' && (
            <div>
              {/* Primary spacecraft */}
              <div style={{ padding: '6px 14px 4px', color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>
                PRIMARY SPACECRAFT
              </div>
              <div
                onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: 'AT-1' }))}
                style={{
                  padding: '8px 14px', borderBottom: '1px solid var(--bg-dark)',
                  cursor: 'pointer', display: 'flex', alignItems: 'flex-start', gap: 10,
                }}
              >
                <StatusDot online={primaryOnline} active={primaryOnline} />
                <div style={{ flex: 1 }}>
                  <div style={{ color: 'var(--cyan)', fontSize: 10, fontWeight: 700, letterSpacing: 1, marginBottom: 2 }}>
                    AT-1 (SC-1)
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 0.5, marginBottom: 4 }}>
                    400 km · 51.6° · ISS-LIKE ORBIT
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ color: primaryOnline ? 'var(--green)' : 'var(--red)', fontSize: 8, fontWeight: 700, letterSpacing: 1 }}>
                      {primaryOnline ? '● LIVE' : '● OFFLINE'}
                    </span>
                    <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>
                      FULL TELEMETRY
                    </span>
                  </div>
                </div>
              </div>

              {/* ISL relay nodes */}
              <div style={{ padding: '10px 14px 4px', color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>
                ISL RELAY NODES
              </div>
              {([
                { id: 'SC-2', name: 'SC-2 / RELAY-1', orbit: '700 km · 98° · SUN-SYNC', status: r1, relay: relay1 },
                { id: 'SC-3', name: 'SC-3 / RELAY-2', orbit: '550 km · 53°', status: r2, relay: relay2 },
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

              {/* Constellation note */}
              <div style={{
                margin: '10px 14px 0',
                padding: '8px 10px',
                background: 'var(--bg-deep)',
                border: '1px solid var(--border)',
                borderRadius: 3,
              }}>
                <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2, marginBottom: 4 }}>CONSTELLATION</div>
                <div style={{ color: 'var(--text-mid)', fontSize: 8, lineHeight: 1.5 }}>
                  AT-1 primary + SC-2 / SC-3 ISL relay nodes. Switch to live TLE data via the KPI bar to overlay real constellations.
                </div>
              </div>
            </div>
          )}

          {/* ── MARKET ── */}
          {tab === 'MARKET' && (
            <div>
              <div style={{ padding: '0 14px 8px', color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>
                IN-ORBIT WORKLOADS · {WORKLOADS.length} AVAILABLE
              </div>
              {deployLog && (
                <div style={{
                  margin: '0 14px 10px',
                  padding: '6px 10px',
                  background: deployLog.ok ? 'rgba(0,232,120,0.08)' : 'rgba(240,80,60,0.08)',
                  border: `1px solid ${deployLog.ok ? 'var(--green)' : 'var(--red)'}`,
                  borderRadius: 3,
                }}>
                  <span style={{ color: deployLog.ok ? 'var(--green)' : 'var(--red)', fontSize: 8, letterSpacing: 1 }}>
                    {deployLog.msg}
                  </span>
                </div>
              )}
              {WORKLOADS.map(card => (
                <div key={card.profileId} style={{ padding: '10px 14px', borderBottom: '1px solid var(--bg-dark)' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 3 }}>
                    <span style={{ color: 'var(--cyan)', fontSize: 9, fontWeight: 700, letterSpacing: 0.5 }}>
                      {card.name}
                    </span>
                    {card.badge && (
                      <span style={{
                        color: 'var(--amber)', fontSize: 7, letterSpacing: 1.5,
                        border: '1px solid var(--amber)', padding: '0 3px', borderRadius: 2,
                      }}>
                        {card.badge}
                      </span>
                    )}
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 8, lineHeight: 1.4, marginBottom: 6 }}>
                    {card.description}
                  </div>
                  <div style={{ display: 'flex', gap: 12, marginBottom: 6, alignItems: 'center' }}>
                    <div>
                      <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1, marginBottom: 2 }}>CPU</div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                        <Meter value={card.cpuPct} max={20} color="var(--cyan)" />
                        <span style={{ color: 'var(--text-dim)', fontSize: 7 }}>{card.cpuPct}%</span>
                      </div>
                    </div>
                    <div>
                      <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1, marginBottom: 2 }}>MEM</div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                        <Meter value={card.memMB} max={16} color="var(--teal)" />
                        <span style={{ color: 'var(--text-dim)', fontSize: 7 }}>{card.memMB}MB</span>
                      </div>
                    </div>
                    <div>
                      <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1, marginBottom: 2 }}>LINK</div>
                      <span style={{ color: 'var(--text-dim)', fontSize: 7 }}>
                        {(card.sizeBytes / 1024).toFixed(0)}KB · {linkSec(card.sizeBytes)}s
                      </span>
                    </div>
                  </div>
                  <button
                    onClick={() => deploy(card)}
                    disabled={deploying !== null}
                    style={{
                      width: '100%', padding: '5px 0',
                      background: deploying === card.profileId ? 'rgba(0,200,240,0.08)' : 'rgba(0,200,240,0.05)',
                      border: '1px solid var(--cyan)',
                      borderRadius: 3, cursor: deploying !== null ? 'not-allowed' : 'pointer',
                      color: deploying === card.profileId ? 'var(--text-dim)' : 'var(--cyan)',
                      fontSize: 9, letterSpacing: 2, fontFamily: 'inherit',
                      opacity: deploying !== null && deploying !== card.profileId ? 0.4 : 1,
                    }}
                  >
                    {deploying === card.profileId ? 'UPLOADING...' : 'DEPLOY TO AT-1'}
                  </button>
                </div>
              ))}
            </div>
          )}

          {/* ── EVENTS ── */}
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
