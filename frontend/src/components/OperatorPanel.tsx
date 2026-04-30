import { useState, useCallback } from 'react'

type Tab = 'FLEET' | 'MARKETPLACE' | 'ANALYTICS'

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
    description: '26KB statistical health monitor. Mirrors Satlyt STL-01 deployment — rolling z-score baseline, downlink prioritization by class.',
    sizeBytes: 26112,
    cpuPct: 4,
    memMB: 3,
    badge: 'STL-01 ANALOG',
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
    description: '8KB SLM-class behavioral classifier. Autonomous diagnostics — interprets inputs, responds to conditions. Same contract as STL-02 Gemma.',
    sizeBytes: 8192,
    cpuPct: 6,
    memMB: 4,
    badge: 'STL-02 ANALOG',
  },
  {
    profileId: 0x04,
    name: 'orbit-predictor-v1',
    description: '18KB autonomous orbit event scheduler. Predicts eclipse, perigee, and apogee windows. Drives ECLIPSE_COMPUTE mode scheduling.',
    sizeBytes: 18432,
    cpuPct: 3,
    memMB: 2,
  },
  {
    profileId: 0x05,
    name: 'federated-learner-v1',
    description: '40KB federated gradient accumulator. Distributed model updates across satellite network. Mirrors Satlyt federated mesh architecture.',
    sizeBytes: 40960,
    cpuPct: 12,
    memMB: 8,
    badge: 'FEDERATED',
  },
  {
    profileId: 0x06,
    name: 'link-optimizer-v1',
    description: '15KB dynamic downlink scheduler. Prioritizes frames by anomaly class and link margin. Mirrors STL-01 downlink prioritization.',
    sizeBytes: 15360,
    cpuPct: 3,
    memMB: 2,
  },
]

const FLEET = [
  { id: 'AT-1', name: 'AT-1 (SC-1)', orbit: '400 km · 51.6°', role: 'PRIMARY', color: 'var(--cyan)' },
  { id: 'SC-2', name: 'SC-2 / RELAY-1', orbit: '700 km · 98°', role: 'ISL RELAY', color: 'var(--teal)' },
  { id: 'SC-3', name: 'SC-3 / RELAY-2', orbit: '550 km · 53°', role: 'ISL RELAY', color: 'var(--teal)' },
]

function Meter({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = Math.min(100, (value / max) * 100)
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
      <div style={{ width: 48, height: 4, background: 'var(--bg-dark)', borderRadius: 2, overflow: 'hidden' }}>
        <div style={{ width: `${pct}%`, height: '100%', background: color, borderRadius: 2 }} />
      </div>
      <span style={{ color: 'var(--text-dim)', fontSize: 8, fontVariantNumeric: 'tabular-nums', minWidth: 24 }}>
        {value}%
      </span>
    </div>
  )
}

export function OperatorPanel() {
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState<Tab>('MARKETPLACE')
  const [deployLog, setDeployLog] = useState<{ profileId: number; msg: string; ok: boolean } | null>(null)
  const [deploying, setDeploying] = useState<number | null>(null)

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

  return (
    <>
      {/* Toggle button */}
      <button
        onClick={() => setOpen(o => !o)}
        style={{
          position: 'fixed', top: 54, left: 0, zIndex: 110,
          background: open ? 'rgba(0,200,240,0.12)' : 'rgba(4,13,28,0.85)',
          border: '1px solid var(--border)', borderLeft: 'none',
          borderRadius: '0 4px 4px 0',
          padding: '10px 8px', cursor: 'pointer',
          display: 'flex', flexDirection: 'column', gap: 2, alignItems: 'center',
        }}
      >
        {[0, 1, 2].map(i => (
          <div key={i} style={{ width: 14, height: 1.5, background: open ? 'var(--cyan)' : 'var(--text-dim)', borderRadius: 1 }} />
        ))}
      </button>

      {/* Panel */}
      {open && (
        <div style={{
          position: 'fixed', top: 42, left: 0, bottom: 0, zIndex: 109, width: 320,
          background: 'rgba(4,13,28,0.96)', borderRight: '1px solid var(--border)',
          display: 'flex', flexDirection: 'column',
          backdropFilter: 'blur(10px)',
        }}>
          {/* Tabs */}
          <div style={{ display: 'flex', borderBottom: '1px solid var(--border)' }}>
            {(['FLEET', 'MARKETPLACE', 'ANALYTICS'] as Tab[]).map(t => (
              <button
                key={t}
                onClick={() => setTab(t)}
                style={{
                  flex: 1, padding: '8px 0',
                  background: 'none', border: 'none', cursor: 'pointer',
                  borderBottom: tab === t ? '2px solid var(--cyan)' : '2px solid transparent',
                  color: tab === t ? 'var(--cyan)' : 'var(--text-dim)',
                  fontSize: 9, letterSpacing: 2, fontFamily: 'inherit', fontWeight: tab === t ? 700 : 400,
                }}
              >
                {t}
              </button>
            ))}
          </div>

          <div style={{ flex: 1, overflowY: 'auto', padding: '12px 0' }}>
            {tab === 'FLEET' && (
              <div>
                <div style={{ padding: '0 14px 8px', color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2 }}>
                  CONSTELLATION · {FLEET.length} NODES
                </div>
                {FLEET.map(sat => (
                  <div
                    key={sat.id}
                    onClick={() => window.dispatchEvent(new CustomEvent('select-sat', { detail: sat.id }))}
                    style={{
                      padding: '10px 14px', borderBottom: '1px solid var(--bg-dark)',
                      cursor: 'pointer', display: 'flex', alignItems: 'flex-start', gap: 10,
                    }}
                  >
                    <div style={{
                      width: 6, height: 6, borderRadius: '50%', marginTop: 2, flexShrink: 0,
                      background: sat.color, boxShadow: `0 0 5px ${sat.color}`,
                    }} />
                    <div style={{ flex: 1 }}>
                      <div style={{ color: sat.color, fontSize: 10, fontWeight: 700, letterSpacing: 1 }}>{sat.name}</div>
                      <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1, marginTop: 2 }}>{sat.orbit}</div>
                      <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>{sat.role}</div>
                    </div>
                  </div>
                ))}
              </div>
            )}

            {tab === 'MARKETPLACE' && (
              <div>
                <div style={{ padding: '0 14px 8px', color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2 }}>
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
                  <div
                    key={card.profileId}
                    style={{
                      padding: '10px 14px', borderBottom: '1px solid var(--bg-dark)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 6, marginBottom: 4 }}>
                      <div style={{ flex: 1 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 2 }}>
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
                        <div style={{ color: 'var(--text-dim)', fontSize: 8, lineHeight: 1.4, marginBottom: 5 }}>
                          {card.description}
                        </div>
                      </div>
                    </div>
                    <div style={{ display: 'flex', gap: 12, marginBottom: 6 }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>CPU</span>
                        <Meter value={card.cpuPct} max={20} color="var(--cyan)" />
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>MEM {card.memMB}MB</span>
                        <Meter value={card.memMB} max={16} color="var(--teal)" />
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                        <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>SIZE</span>
                        <span style={{ color: 'var(--text-dim)', fontSize: 8 }}>
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
                      {deploying === card.profileId ? 'UPLOADING...' : 'DEPLOY'}
                    </button>
                  </div>
                ))}
              </div>
            )}

            {tab === 'ANALYTICS' && (
              <div style={{ padding: '20px 14px' }}>
                <div style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2, marginBottom: 12 }}>
                  MISSION ANALYTICS
                </div>
                <div style={{ color: 'var(--text-dim)', fontSize: 9, lineHeight: 1.6 }}>
                  Deploy workloads via the Marketplace tab, then monitor their effect on the KPI bar — downlink efficiency, anomaly class, and inference confidence.
                </div>
                <div style={{ marginTop: 16, padding: '8px 10px', border: '1px solid var(--border)', borderRadius: 3 }}>
                  <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2, marginBottom: 4 }}>LINK BUDGET</div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 8, lineHeight: 1.5 }}>
                    Run LINK BUDGET via Ctrl+K → COMPUTE to see Friis equation margins across all three ground stations at current altitude.
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  )
}
