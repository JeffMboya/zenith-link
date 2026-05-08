import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
import { PRIMARY_SATELLITES } from '../data/primarySatellites'

interface LogEntry { ts: number; text: string; type: 'sent' | 'ack' | 'error' }

interface Command {
  id: string; label: string; description: string
  category: 'MODE' | 'SYSTEM' | 'TELEMETRY' | 'DIAGNOSTIC' | 'COMPUTE' | 'DEPLOY'
  commandId: number; payloadBytes?: number[]; destructive?: boolean
}

interface SatItem { id: string; label: string; description: string; kind: 'satellite' | 'groundstation' }

const COMMANDS: Command[] = [
  { id: 'safe_mode',        label: 'SAFE MODE',                  description: 'Reduce power, halt non-essential subsystems',                  category: 'MODE',       commandId: 0x03, payloadBytes: [0x01], destructive: true },
  { id: 'nominal',          label: 'NOMINAL',                    description: 'Return to nominal operations',                                 category: 'MODE',       commandId: 0x03, payloadBytes: [0x00] },
  { id: 'reboot_obc',       label: 'REBOOT OBC',                 description: 'Soft-reset onboard computer (sequence counter loss)',          category: 'SYSTEM',     commandId: 0x02, destructive: true },
  { id: 'dump_tlm',         label: 'DUMP TELEMETRY',             description: 'Request current health classification and state',              category: 'TELEMETRY',  commandId: 0x01 },
  { id: 'set_pitch',        label: 'SET PITCH THRESHOLD',        description: 'Set attitude deadband — mode 0x02',                            category: 'DIAGNOSTIC', commandId: 0x03, payloadBytes: [0x02] },
  { id: 'inference',        label: 'QUERY HEALTH AI',            description: 'Return current onboard health classification',                 category: 'DIAGNOSTIC', commandId: 0x01 },
  { id: 'health_scan',      label: 'HEALTH SCAN',                description: 'Scan last 30 frames, return anomaly summary',                  category: 'COMPUTE',    commandId: 0x04, payloadBytes: [0x01] },
  { id: 'eclipse_forecast', label: 'ECLIPSE FORECAST',           description: 'Compute next eclipse entry/exit from orbital state',           category: 'COMPUTE',    commandId: 0x04, payloadBytes: [0x02] },
  { id: 'link_budget',      label: 'LINK BUDGET',                description: 'Link margin to all ground stations from current altitude',     category: 'COMPUTE',    commandId: 0x04, payloadBytes: [0x03] },
  { id: 'deploy_anomaly',   label: 'DEPLOY ANOMALY DETECTOR',    description: 'Upload 26KB health monitor · 1.3s @ 20 KB/s',                 category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x01] },
  { id: 'deploy_compress',  label: 'DEPLOY TELEMETRY COMPRESSOR',description: 'Upload 12KB delta-compression agent · 0.6s @ 20 KB/s',        category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x02] },
  { id: 'deploy_inference', label: 'DEPLOY EDGE INFERENCE AGENT',description: 'Upload 8KB SLM-class edge agent · 0.4s @ 20 KB/s',           category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x03] },
  { id: 'deploy_orbit',     label: 'DEPLOY ORBIT PREDICTOR',     description: 'Upload 18KB orbit event scheduler · 0.9s @ 20 KB/s',          category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x04] },
  { id: 'deploy_fedlearn',  label: 'DEPLOY FEDERATED LEARNER',   description: 'Upload 40KB federated gradient accumulator · 2.0s @ 20 KB/s', category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x05] },
  { id: 'deploy_linkopt',   label: 'DEPLOY LINK OPTIMIZER',      description: 'Upload 15KB dynamic downlink scheduler · 0.75s @ 20 KB/s',    category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x06] },
]

const NODES: SatItem[] = [
  { id: 'ISS (ZARYA)',  label: 'ISS',           description: 'Primary · LEO 408 km · 51.6°',        kind: 'satellite'      },
  { id: 'CSS (TIANHE)', label: 'Tiangong',      description: 'Primary · LEO 380 km · 41.5°',        kind: 'satellite'      },
  { id: 'HST',          label: 'Hubble',        description: 'Primary · LEO 540 km · 28.5°',        kind: 'satellite'      },
  { id: 'NOAA-20',      label: 'NOAA-20',       description: 'ISL Relay · 824 km · 98.7° sun-sync', kind: 'satellite'      },
  { id: 'Sentinel-2A',  label: 'Sentinel-2A',   description: 'ISL Relay · 786 km · 98.6° sun-sync', kind: 'satellite'      },
  { id: 'Landsat-9',    label: 'Landsat-9',     description: 'ISL Relay · 705 km · 98.2° sun-sync', kind: 'satellite'      },
  { id: 'Aqua',         label: 'Aqua',          description: 'ISL Relay · 705 km · 98.2° sun-sync', kind: 'satellite'      },
  { id: 'Terra',        label: 'Terra',         description: 'ISL Relay · 705 km · 98.2° sun-sync', kind: 'satellite'      },
  { id: 'NOAA-19',      label: 'NOAA-19',       description: 'ISL Relay · 870 km · 99.1° sun-sync', kind: 'satellite'      },
  { id: 'gs-nairobi',    label: 'Nairobi',       description: 'Ground Station · 1.3°S  36.8°E',    kind: 'groundstation' },
  { id: 'gs-svalbard',   label: 'Svalbard',      description: 'Ground Station · 78.2°N  15.6°E',   kind: 'groundstation' },
  { id: 'gs-punta',      label: 'Punta Arenas',  description: 'Ground Station · 53.2°S  70.9°W',   kind: 'groundstation' },
  { id: 'gs-fairbanks',  label: 'Fairbanks',     description: 'Ground Station · 64.8°N  147.7°W',  kind: 'groundstation' },
  { id: 'gs-bangalore',  label: 'Bangalore',     description: 'Ground Station · 13.0°N  77.6°E',   kind: 'groundstation' },
  { id: 'gs-perth',      label: 'Perth',         description: 'Ground Station · 32.0°S  115.9°E',  kind: 'groundstation' },
]

const CATEGORY_COLOR: Record<Command['category'], string> = {
  MODE: '#50b8d0', SYSTEM: '#d08030', TELEMETRY: '#40d080',
  DIAGNOSTIC: '#9060d0', COMPUTE: '#50b8d0', DEPLOY: '#40d080',
}

function fuzzyMatch(haystack: string, needle: string): boolean {
  if (!needle) return true
  const h = haystack.toLowerCase(), n = needle.toLowerCase()
  let hi = 0
  for (let ni = 0; ni < n.length; ni++) {
    hi = h.indexOf(n[ni], hi)
    if (hi === -1) return false
    hi++
  }
  return true
}

function fuzzyScore(haystack: string, needle: string): number {
  if (!needle) return 0
  const h = haystack.toLowerCase(), n = needle.toLowerCase()
  if (h.startsWith(n)) return 150
  if (h.includes(n)) return 100
  return 50
}

type Mode = 'command' | 'satellite'

interface Props { selectedSatId: string }

export function CommandPanel({ selectedSatId }: Props) {
  const [query,         setQuery]         = useState('')
  const [selected,      setSelected]      = useState(0)
  const [log,           setLog]           = useState<LogEntry[]>([])
  const [sending,       setSending]       = useState(false)
  const [confirming,    setConfirming]    = useState<Command | null>(null)
  const [confirmInput,  setConfirmInput]  = useState('')
  const [recentIds,     setRecentIds]     = useState<string[]>([])
  const [focused,       setFocused]       = useState(false)

  const inputRef   = useRef<HTMLInputElement>(null)
  const confirmRef = useRef<HTMLInputElement>(null)

  // Ctrl+K focuses the input
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        inputRef.current?.focus()
      }
      if (e.key === 'Escape') {
        if (confirming) { setConfirming(null); setConfirmInput('') }
        else { setQuery(''); inputRef.current?.blur() }
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [confirming])

  useEffect(() => {
    if (confirming) setTimeout(() => confirmRef.current?.focus(), 30)
  }, [confirming])

  const mode: Mode = query.startsWith('@') || query.startsWith('#') ? 'satellite' : 'command'
  const searchTerm = (query.startsWith('>') || query.startsWith('@') || query.startsWith('#'))
    ? query.slice(1).trim() : query.trim()

  // Show results when: user typed something OR typed /list
  const isListCmd = query.trim().toLowerCase() === '/list'
  const showResults = (focused && query.length > 0) || isListCmd

  const filteredCommands = useMemo(() => {
    if (mode !== 'command') return []
    const term = isListCmd ? '' : searchTerm
    return COMMANDS
      .filter(c => fuzzyMatch(c.label + ' ' + c.description + ' ' + c.category, term))
      .sort((a, b) => {
        const ai = recentIds.indexOf(a.id), bi = recentIds.indexOf(b.id)
        if (ai !== -1 && bi === -1) return -1
        if (bi !== -1 && ai === -1) return 1
        if (ai !== -1 && bi !== -1) return ai - bi
        return fuzzyScore(b.label, term) - fuzzyScore(a.label, term)
      })
  }, [query, mode, searchTerm, recentIds, isListCmd])

  const filteredNodes = useMemo(() => {
    if (mode !== 'satellite') return []
    const filterKind = query.startsWith('#') ? 'groundstation' : undefined
    return NODES.filter(n => (!filterKind || n.kind === filterKind) && fuzzyMatch(n.label + ' ' + n.description, searchTerm))
  }, [query, mode, searchTerm])

  const totalItems = mode === 'command' ? filteredCommands.length : filteredNodes.length

  const transmit = useCallback(async (cmd: Command) => {
    if (sending) return
    setSending(true)
    setConfirming(null); setConfirmInput('')
    setQuery(''); inputRef.current?.blur()
    setRecentIds(prev => [cmd.id, ...prev.filter(id => id !== cmd.id)].slice(0, 5))
    setLog(l => [...l, { ts: Date.now(), text: `TX  ${cmd.category}: ${cmd.label}`, type: 'sent' }])
    try {
      const payloadB64 = cmd.payloadBytes?.length ? btoa(String.fromCharCode(...cmd.payloadBytes)) : undefined
      // Resolve backend target from selectedSatId
      const sat = PRIMARY_SATELLITES.find(s => s.tleName === selectedSatId)
      const target = sat?.scTarget ?? 'sc1'
      const res = await fetch(`/command?target=${target}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command_id: cmd.commandId, ...(payloadB64 ? { payload_b64: payloadB64 } : {}) }),
      })
      const body = await res.json() as { accepted?: boolean; message?: string; error?: string }
      setLog(l => [...l, {
        ts: Date.now(),
        text: body.accepted ? `ACK${body.message ? ' · ' + body.message : ''}` : `ERR ${body.error ?? body.message ?? 'rejected'}`,
        type: body.accepted ? 'ack' : 'error',
      }])
    } catch {
      setLog(l => [...l, { ts: Date.now(), text: 'SEND FAILED — link down?', type: 'error' }])
    } finally { setSending(false) }
  }, [sending])

  const selectCommand = useCallback((cmd: Command) => {
    if (cmd.destructive) { setConfirming(cmd); setConfirmInput('') }
    else transmit(cmd)
  }, [transmit])

  const selectNode = useCallback((node: SatItem) => {
    window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))
    setQuery(''); inputRef.current?.blur()
  }, [])

  const handleKey = (e: React.KeyboardEvent) => {
    if (!showResults) return
    if (e.key === 'ArrowDown') { e.preventDefault(); setSelected(s => Math.min(s + 1, totalItems - 1)) }
    if (e.key === 'ArrowUp')   { e.preventDefault(); setSelected(s => Math.max(s - 1, 0)) }
    if (e.key === 'Enter') {
      if (mode === 'command' && filteredCommands[selected]) selectCommand(filteredCommands[selected])
      if (mode === 'satellite' && filteredNodes[selected]) selectNode(filteredNodes[selected])
    }
  }

  const recentCommands = COMMANDS
    .filter(c => recentIds.includes(c.id))
    .sort((a, b) => recentIds.indexOf(a.id) - recentIds.indexOf(b.id))

  const showRecent = mode === 'command' && !searchTerm && !isListCmd && recentCommands.length > 0

  const prefix = mode === 'satellite' ? (query.startsWith('#') ? '#' : '@') : '>'

  return (
    <>
      {/* Persistent command bar — always visible */}
      <div style={{
        position: 'fixed',
        top: 56,
        left: '50%',
        transform: 'translateX(-50%)',
        width: 620,
        zIndex: 300,
      }}>

        {/* Confirmation view — overlays the input when active */}
        {confirming ? (
          <div style={{
            background: '#07111f',
            border: '1px solid #c03030',
            borderRadius: 8,
            boxShadow: '0 20px 60px rgba(200,32,48,0.3)',
            overflow: 'hidden',
          }}>
            <div style={{ padding: '14px 18px', background: 'rgba(200,32,48,0.07)', borderBottom: '1px solid rgba(200,32,48,0.25)' }}>
              <div style={{ color: '#c04040', fontSize: 9, letterSpacing: 2, marginBottom: 5, fontWeight: 700 }}>⚠ DESTRUCTIVE — CONFIRMATION REQUIRED</div>
              <div style={{ color: '#e0eef8', fontSize: 13, fontWeight: 700, marginBottom: 3 }}>{confirming.category}: {confirming.label}</div>
              <div style={{ color: '#8ab0c0', fontSize: 10 }}>{confirming.description}</div>
              <div style={{ color: '#4a6a7a', fontSize: 9, marginTop: 4, fontFamily: 'monospace' }}>
                TC cmd_id=0x{confirming.commandId.toString(16).padStart(2,'0')}
                {confirming.payloadBytes?.length ? ' payload=[' + confirming.payloadBytes.map(b => '0x'+b.toString(16).padStart(2,'0')).join(',') + ']' : ''}
              </div>
            </div>
            <div style={{ padding: '14px 18px', borderBottom: '1px solid #0e1a28' }}>
              <div style={{ color: '#8ab0c0', fontSize: 10, marginBottom: 10 }}>
                Type <span style={{ color: '#c04040', fontWeight: 700 }}>CONFIRM</span> and press Enter · Esc to cancel
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <span style={{ color: '#c04040', fontSize: 14 }}>›</span>
                <input
                  ref={confirmRef}
                  value={confirmInput}
                  onChange={e => setConfirmInput(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && confirmInput.trim().toUpperCase() === 'CONFIRM') transmit(confirming)
                    if (e.key === 'Escape') { setConfirming(null); setConfirmInput('') }
                  }}
                  placeholder="CONFIRM"
                  style={{ flex: 1, background: 'transparent', border: 'none', outline: 'none', color: confirmInput.trim().toUpperCase() === 'CONFIRM' ? '#c04040' : '#e0eef8', fontSize: 13, fontFamily: 'monospace', letterSpacing: 2 }}
                />
                <button
                  onClick={() => { if (confirmInput.trim().toUpperCase() === 'CONFIRM') transmit(confirming) }}
                  disabled={confirmInput.trim().toUpperCase() !== 'CONFIRM'}
                  style={{
                    padding: '5px 14px', fontSize: 10, letterSpacing: 1,
                    background: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'rgba(200,32,48,0.15)' : 'transparent',
                    border: `1px solid ${confirmInput.trim().toUpperCase() === 'CONFIRM' ? '#c04040' : '#1e3040'}`,
                    color: confirmInput.trim().toUpperCase() === 'CONFIRM' ? '#c04040' : '#4a6a7a',
                    cursor: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'pointer' : 'not-allowed', borderRadius: 4,
                  }}
                >TRANSMIT</button>
              </div>
            </div>
            <div style={{ padding: '8px 18px' }}>
              <button onClick={() => { setConfirming(null); setConfirmInput('') }}
                style={{ background: 'none', border: 'none', color: '#8ab0c0', fontSize: 10, cursor: 'pointer' }}>
                ← Cancel
              </button>
            </div>
          </div>
        ) : (
          <div>
            {/* Always-visible input bar */}
            <div style={{
              display: 'flex', alignItems: 'center', gap: 10,
              padding: '0 16px',
              height: 42,
              background: showResults ? '#07111f' : 'rgba(7,17,31,0.85)',
              border: `1px solid ${showResults ? 'var(--border)' : 'var(--border)'}`,
              borderRadius: showResults ? '8px 8px 0 0' : 8,
              backdropFilter: 'blur(12px)',
              boxShadow: showResults
                ? '0 2px 0 #0e1a28'
                : '0 4px 20px rgba(0,0,0,0.4)',
              transition: 'border-radius 0.1s, border-color 0.15s, box-shadow 0.15s',
            }}>
              <span style={{ color: focused ? '#90b4cc' : '#4a6a7a', fontSize: 13, fontFamily: 'monospace', flexShrink: 0, transition: 'color 0.15s' }}>
                {prefix}
              </span>
              <input
                ref={inputRef}
                value={query}
                onChange={e => { setQuery(e.target.value); setSelected(0) }}
                onKeyDown={handleKey}
                onFocus={() => setFocused(true)}
                onBlur={() => setTimeout(() => setFocused(false), 150)}
                placeholder={focused ? 'Type /list to see all commands' : 'Uplink command · ⌃K to focus'}
                style={{
                  flex: 1, background: 'transparent', border: 'none', outline: 'none',
                  color: '#e0eef8', fontSize: 13,
                }}
              />
              {sending && <span style={{ color: '#d08030', fontSize: 9, letterSpacing: 1, flexShrink: 0 }}>TRANSMITTING...</span>}
              {!sending && log.length > 0 && (
                <span style={{ color: log[log.length-1].type === 'ack' ? '#40d080' : '#e05050', fontSize: 9, letterSpacing: 0.5, flexShrink: 0 }}>
                  {log[log.length-1].type === 'ack' ? '✓ ACK' : '✗ ERR'}
                </span>
              )}
              {focused && (
                <span style={{ color: '#3a5568', fontSize: 9, border: '1px solid #1e3040', padding: '1px 5px', borderRadius: 3, flexShrink: 0 }}>
                  ESC
                </span>
              )}
            </div>

            {/* Results dropdown — only when typing */}
            {showResults && (
              <div style={{
                background: '#07111f',
                border: '1px solid var(--border)',
                borderTop: 'none',
                borderRadius: '0 0 8px 8px',
                boxShadow: '0 16px 48px rgba(0,0,0,0.7)',
                overflow: 'hidden',
              }}>
                <div style={{ maxHeight: 360, overflowY: 'auto' }}>

                  {/* Recently used */}
                  {showRecent && (
                    <>
                      <div style={{ padding: '8px 16px 4px', color: '#3a5568', fontSize: 9, letterSpacing: 2, fontWeight: 700 }}>RECENTLY USED</div>
                      {recentCommands.map((cmd, i) => (
                        <CommandRow key={cmd.id} cmd={cmd} selected={i === selected} onHover={() => setSelected(i)} onClick={() => selectCommand(cmd)} />
                      ))}
                      <div style={{ height: 1, background: '#0e1a28', margin: '4px 0' }} />
                    </>
                  )}

                  {/* Commands */}
                  {mode === 'command' && (
                    filteredCommands.length === 0
                      ? <div style={{ padding: '20px 16px', color: '#3a5568', fontSize: 10, textAlign: 'center' }}>No commands match</div>
                      : filteredCommands.map((cmd, i) => {
                          const idx = showRecent ? recentCommands.length + i : i
                          return <CommandRow key={cmd.id} cmd={cmd} selected={idx === selected} onHover={() => setSelected(idx)} onClick={() => selectCommand(cmd)} />
                        })
                  )}

                  {/* Satellite / GS mode */}
                  {mode === 'satellite' && (
                    filteredNodes.length === 0
                      ? <div style={{ padding: '20px 16px', color: '#3a5568', fontSize: 10, textAlign: 'center' }}>No results</div>
                      : filteredNodes.map((node, i) => (
                          <button key={node.id} onClick={() => selectNode(node)} onMouseEnter={() => setSelected(i)}
                            style={{
                              width: '100%', display: 'flex', alignItems: 'center', gap: 12,
                              padding: '10px 16px', border: 'none', cursor: 'pointer', textAlign: 'left',
                              background: i === selected ? 'rgba(144,180,204,0.07)' : 'transparent',
                              borderLeft: i === selected ? '2px solid #90b4cc' : '2px solid transparent',
                            }}>
                            <span style={{ fontSize: 8, letterSpacing: 1, padding: '2px 6px', border: `1px solid ${node.kind === 'satellite' ? '#50b8d0' : '#40d080'}`, color: node.kind === 'satellite' ? '#50b8d0' : '#40d080', borderRadius: 2, flexShrink: 0, minWidth: 52, textAlign: 'center' }}>
                              {node.kind === 'satellite' ? 'SAT' : 'GS'}
                            </span>
                            <span style={{ flex: 1 }}>
                              <div style={{ color: '#e0eef8', fontSize: 11, fontWeight: 600 }}>{node.label}</div>
                              <div style={{ color: '#8ab0c0', fontSize: 9, marginTop: 1 }}>{node.description}</div>
                            </span>
                            <span style={{ color: '#3a5568', fontSize: 9 }}>↵</span>
                          </button>
                        ))
                  )}

                  {/* TX log */}
                  {log.length > 0 && (
                    <div style={{ borderTop: '1px solid #0e1a28', background: '#050e1c', padding: '8px 16px' }}>
                      {log.slice(-5).map((e, i) => (
                        <div key={i} style={{ display: 'flex', gap: 10, fontSize: 9, lineHeight: 1.8, fontFamily: 'monospace' }}>
                          <span style={{ color: '#2a3a4a', flexShrink: 0 }}>{new Date(e.ts).toLocaleTimeString('en-US', { hour12: false })}</span>
                          <span style={{ color: e.type === 'sent' ? '#50b8d0' : e.type === 'ack' ? '#40d080' : '#e05050' }}>{e.text}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                {/* Footer hints */}
                <div style={{ borderTop: '1px solid #0e1a28', padding: '6px 16px', display: 'flex', gap: 0, background: '#050e1c' }}>
                  {[['↑↓','navigate'],['↵','select'],['>','commands'],['@','satellites'],['#','ground stations'],['/list','show all']].map(([k,l]) => (
                    <span key={k} style={{ display: 'flex', alignItems: 'center', gap: 4, marginRight: 12 }}>
                      <span style={{ color: '#8ab0c8', fontSize: 10, fontFamily: 'monospace', border: '1px solid var(--border)', padding: '1px 4px', borderRadius: 3 }}>{k}</span>
                      <span style={{ color: '#5878a0', fontSize: 10 }}>{l}</span>
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Backdrop — only when results are open, light so globe stays visible */}
      {(showResults || !!confirming) && (
        <div
          onClick={() => { setQuery(''); setConfirming(null); inputRef.current?.blur() }}
          style={{ position: 'fixed', inset: 0, zIndex: 299, background: 'rgba(2,7,16,0.3)' }}
        />
      )}

      {/* Persistent mini-log — always visible below input bar, last 3 entries */}
      {!confirming && log.length > 0 && (
        <div style={{
          marginTop: 3,
          background: 'rgba(5,14,28,0.92)',
          border: '1px solid var(--border)',
          borderRadius: 6,
          padding: '5px 12px',
          backdropFilter: 'blur(8px)',
        }}>
          {log.slice(-3).map((e, i) => (
            <div key={i} style={{ display: 'flex', gap: 8, fontSize: 9, lineHeight: 1.7, fontFamily: 'monospace' }}>
              <span style={{ color: 'var(--text-dim)', flexShrink: 0 }}>
                {new Date(e.ts).toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })}
              </span>
              <span style={{ color: e.type === 'sent' ? 'var(--cyan)' : e.type === 'ack' ? 'var(--green)' : 'var(--red)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {e.text}
              </span>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

function CommandRow({ cmd, selected, onHover, onClick }: { cmd: Command; selected: boolean; onHover: () => void; onClick: () => void }) {
  return (
    <button onClick={onClick} onMouseEnter={onHover} style={{
      width: '100%', display: 'flex', alignItems: 'flex-start', gap: 12,
      padding: '10px 16px', border: 'none', cursor: 'pointer', textAlign: 'left',
      background: selected ? (cmd.destructive ? 'rgba(200,32,48,0.07)' : 'rgba(144,180,204,0.07)') : 'transparent',
      borderLeft: selected ? `2px solid ${cmd.destructive ? '#c04040' : '#90b4cc'}` : '2px solid transparent',
    }}>
      <span style={{ fontSize: 8, letterSpacing: 1, padding: '2px 6px', marginTop: 1, border: `1px solid ${cmd.destructive ? '#c04040' : CATEGORY_COLOR[cmd.category]}`, color: cmd.destructive ? '#c04040' : CATEGORY_COLOR[cmd.category], borderRadius: 2, flexShrink: 0, minWidth: 72, textAlign: 'center' }}>
        {cmd.destructive ? '⚠ ' : ''}{cmd.category}
      </span>
      <span style={{ flex: 1 }}>
        <div style={{ color: cmd.destructive ? '#c04040' : '#e0eef8', fontSize: 11, fontWeight: 600, letterSpacing: 0.3 }}>{cmd.label}</div>
        <div style={{ color: '#8ab0c0', fontSize: 9, marginTop: 2 }}>{cmd.description}</div>
        <div style={{ color: '#2a3a4a', fontSize: 8, marginTop: 2, fontFamily: 'monospace' }}>
          TC 0x{cmd.commandId.toString(16).padStart(2,'0')}{cmd.payloadBytes?.length ? ' ['+cmd.payloadBytes.map(b=>'0x'+b.toString(16).padStart(2,'0')).join(',')+']' : ''}
        </div>
      </span>
      <span style={{ color: '#3a5568', fontSize: 9, paddingTop: 1 }}>{cmd.destructive ? '⚠↵' : '↵'}</span>
    </button>
  )
}
