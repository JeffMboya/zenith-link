import { useState, useRef, useEffect, useCallback, useMemo } from 'react'

interface LogEntry {
  ts: number
  text: string
  type: 'sent' | 'ack' | 'error'
}

interface Command {
  id: string
  label: string
  description: string
  category: 'MODE' | 'SYSTEM' | 'TELEMETRY' | 'DIAGNOSTIC' | 'COMPUTE' | 'DEPLOY'
  commandId: number
  payloadBytes?: number[]
  destructive?: boolean
}

interface SatItem {
  id: string
  label: string
  description: string
  kind: 'satellite' | 'groundstation'
}

const COMMANDS: Command[] = [
  { id: 'safe_mode',        label: 'SAFE MODE',                   description: 'Reduce power, halt non-essential subsystems',                    category: 'MODE',       commandId: 0x03, payloadBytes: [0x01], destructive: true },
  { id: 'nominal',          label: 'NOMINAL',                      description: 'Return to nominal operations',                                   category: 'MODE',       commandId: 0x03, payloadBytes: [0x00] },
  { id: 'reboot_obc',       label: 'REBOOT OBC',                   description: 'Soft-reset onboard computer (sequence counter loss)',            category: 'SYSTEM',     commandId: 0x02,                      destructive: true },
  { id: 'dump_tlm',         label: 'DUMP TELEMETRY',               description: 'Request current health classification and state',                category: 'TELEMETRY',  commandId: 0x01 },
  { id: 'set_pitch',        label: 'SET PITCH THRESHOLD',          description: 'Set attitude deadband — mode 0x02',                              category: 'DIAGNOSTIC', commandId: 0x03, payloadBytes: [0x02] },
  { id: 'inference',        label: 'QUERY HEALTH AI',              description: 'Return current onboard health classification',                   category: 'DIAGNOSTIC', commandId: 0x01 },
  { id: 'health_scan',      label: 'HEALTH SCAN',                  description: 'Scan last 30 frames, return anomaly summary',                    category: 'COMPUTE',    commandId: 0x04, payloadBytes: [0x01] },
  { id: 'eclipse_forecast', label: 'ECLIPSE FORECAST',             description: 'Compute next eclipse entry/exit from orbital state',             category: 'COMPUTE',    commandId: 0x04, payloadBytes: [0x02] },
  { id: 'link_budget',      label: 'LINK BUDGET',                  description: 'Link margin to all ground stations from current altitude',       category: 'COMPUTE',    commandId: 0x04, payloadBytes: [0x03] },
  { id: 'deploy_anomaly',   label: 'DEPLOY ANOMALY DETECTOR',      description: 'Upload 26KB health monitor · 1.3s @ 20 KB/s',                   category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x01] },
  { id: 'deploy_compress',  label: 'DEPLOY TELEMETRY COMPRESSOR',  description: 'Upload 12KB delta-compression agent · 0.6s @ 20 KB/s',          category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x02] },
  { id: 'deploy_inference', label: 'DEPLOY EDGE INFERENCE AGENT',  description: 'Upload 8KB SLM-class edge agent · 0.4s @ 20 KB/s',             category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x03] },
  { id: 'deploy_orbit',     label: 'DEPLOY ORBIT PREDICTOR',       description: 'Upload 18KB orbit event scheduler · 0.9s @ 20 KB/s',            category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x04] },
  { id: 'deploy_fedlearn',  label: 'DEPLOY FEDERATED LEARNER',     description: 'Upload 40KB federated gradient accumulator · 2.0s @ 20 KB/s',   category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x05] },
  { id: 'deploy_linkopt',   label: 'DEPLOY LINK OPTIMIZER',        description: 'Upload 15KB dynamic downlink scheduler · 0.75s @ 20 KB/s',      category: 'DEPLOY',     commandId: 0x05, payloadBytes: [0x06] },
]

const NODES: SatItem[] = [
  { id: 'ISS (ZARYA)',    label: 'ISS (ZARYA)',       description: 'Primary · LEO 408 km · 51.6°',         kind: 'satellite' },
  { id: 'CSS (TIANHE)',   label: 'Tiangong',          description: 'Primary · LEO 380 km · 41.5°',         kind: 'satellite' },
  { id: 'NOAA-20',        label: 'NOAA-20',           description: 'ISL Relay · 824 km · 98.7° sun-sync',  kind: 'satellite' },
  { id: 'Sentinel-2A',    label: 'Sentinel-2A',       description: 'ISL Relay · 786 km · 98.6° sun-sync',  kind: 'satellite' },
  { id: 'Landsat-9',      label: 'Landsat-9',         description: 'ISL Relay · 705 km · 98.2° sun-sync',  kind: 'satellite' },
  { id: 'Aqua',           label: 'Aqua',              description: 'ISL Relay · 705 km · 98.2° sun-sync',  kind: 'satellite' },
  { id: 'Terra',          label: 'Terra',             description: 'ISL Relay · 705 km · 98.2° sun-sync',  kind: 'satellite' },
  { id: 'NOAA-19',        label: 'NOAA-19',           description: 'ISL Relay · 870 km · 99.1° sun-sync',  kind: 'satellite' },
  { id: 'gs-nairobi',     label: 'Nairobi',           description: 'Ground Station · 1.3°S  36.8°E',       kind: 'groundstation' },
  { id: 'gs-svalbard',    label: 'Svalbard',          description: 'Ground Station · 78.2°N  15.6°E',      kind: 'groundstation' },
  { id: 'gs-punta',       label: 'Punta Arenas',      description: 'Ground Station · 53.2°S  70.9°W',      kind: 'groundstation' },
]

const CATEGORY_COLOR: Record<Command['category'], string> = {
  MODE:       '#50b8d0',
  SYSTEM:     '#d08030',
  TELEMETRY:  '#40d080',
  DIAGNOSTIC: '#9060d0',
  COMPUTE:    '#50b8d0',
  DEPLOY:     '#40d080',
}

function fuzzyMatch(haystack: string, needle: string): boolean {
  if (!needle) return true
  const h = haystack.toLowerCase()
  const n = needle.toLowerCase()
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
  const h = haystack.toLowerCase()
  const n = needle.toLowerCase()
  // Contiguous match scores highest
  if (h.includes(n)) return 100 + (h.startsWith(n) ? 50 : 0)
  // Sequential character match
  return 50
}

type Mode = 'command' | 'satellite'

export function CommandPanel() {
  const [open, setOpen]               = useState(false)
  const [query, setQuery]             = useState('')
  const [selected, setSelected]       = useState(0)
  const [log, setLog]                 = useState<LogEntry[]>([])
  const [sending, setSending]         = useState(false)
  const [confirming, setConfirming]   = useState<Command | null>(null)
  const [confirmInput, setConfirmInput] = useState('')
  const [recentIds, setRecentIds]     = useState<string[]>([])

  const inputRef   = useRef<HTMLInputElement>(null)
  const confirmRef = useRef<HTMLInputElement>(null)
  const listRef    = useRef<HTMLDivElement>(null)

  // Detect prefix mode
  const mode: Mode = query.startsWith('@') || query.startsWith('#') ? 'satellite' : 'command'
  const searchTerm = query.startsWith('>') || query.startsWith('@') || query.startsWith('#')
    ? query.slice(1).trim()
    : query.trim()

  // Keyboard shortcut to open
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        setOpen(o => !o)
      }
      if (e.key === 'Escape') {
        if (confirming) { setConfirming(null); setConfirmInput('') }
        else setOpen(false)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [confirming])

  useEffect(() => {
    if (open && !confirming) {
      setQuery(''); setSelected(0)
      setTimeout(() => inputRef.current?.focus(), 30)
    }
  }, [open, confirming])

  useEffect(() => {
    if (confirming) setTimeout(() => confirmRef.current?.focus(), 30)
  }, [confirming])

  // Filtered + scored results
  const filteredCommands = useMemo(() => {
    if (mode !== 'command') return []
    return COMMANDS
      .filter(c => fuzzyMatch(c.label + ' ' + c.description + ' ' + c.category, searchTerm))
      .sort((a, b) => {
        // Recent items first
        const ai = recentIds.indexOf(a.id)
        const bi = recentIds.indexOf(b.id)
        if (ai !== -1 && bi === -1) return -1
        if (bi !== -1 && ai === -1) return 1
        if (ai !== -1 && bi !== -1) return ai - bi
        return fuzzyScore(b.label, searchTerm) - fuzzyScore(a.label, searchTerm)
      })
  }, [query, mode, searchTerm, recentIds])

  const filteredNodes = useMemo(() => {
    if (mode !== 'satellite') return []
    const filterKind = query.startsWith('#') ? 'groundstation' : undefined
    return NODES
      .filter(n => (!filterKind || n.kind === filterKind) && fuzzyMatch(n.label + ' ' + n.description, searchTerm))
  }, [query, mode, searchTerm])

  const totalItems = mode === 'command' ? filteredCommands.length : filteredNodes.length

  const transmit = useCallback(async (cmd: Command) => {
    if (sending) return
    setSending(true)
    setConfirming(null)
    setConfirmInput('')
    setOpen(false)
    setRecentIds(prev => [cmd.id, ...prev.filter(id => id !== cmd.id)].slice(0, 5))
    setLog(l => [...l, { ts: Date.now(), text: `TX  ${cmd.category}: ${cmd.label}`, type: 'sent' }])
    try {
      const payloadB64 = cmd.payloadBytes?.length
        ? btoa(String.fromCharCode(...cmd.payloadBytes))
        : undefined
      const res = await fetch('/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
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
    } finally {
      setSending(false)
    }
  }, [sending])

  const selectCommand = useCallback((cmd: Command) => {
    if (cmd.destructive) { setConfirming(cmd); setConfirmInput('') }
    else transmit(cmd)
  }, [transmit])

  const selectNode = useCallback((node: SatItem) => {
    window.dispatchEvent(new CustomEvent('select-sat', { detail: node.id }))
    setOpen(false)
  }, [])

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSelected(s => Math.min(s + 1, totalItems - 1)) }
    if (e.key === 'ArrowUp')   { e.preventDefault(); setSelected(s => Math.max(s - 1, 0)) }
    if (e.key === 'Enter') {
      if (mode === 'command' && filteredCommands[selected]) selectCommand(filteredCommands[selected])
      if (mode === 'satellite' && filteredNodes[selected]) selectNode(filteredNodes[selected])
    }
  }

  const recentCommands = COMMANDS.filter(c => recentIds.includes(c.id))
    .sort((a, b) => recentIds.indexOf(a.id) - recentIds.indexOf(b.id))

  const showRecent = mode === 'command' && !searchTerm && recentCommands.length > 0

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        aria-label="Open command palette (Ctrl+K)"
        style={{
          position: 'fixed', bottom: 0, left: 0, zIndex: 115,
          padding: '6px 14px',
          background: 'rgba(4,13,28,0.92)',
          borderTop: `1px solid ${sending ? '#d08030' : '#1a2a3a'}`,
          borderRight: '1px solid #1a2a3a',
          backdropFilter: 'blur(8px)',
          borderRadius: '0 4px 0 0',
          display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer',
        }}
      >
        <span style={{ color: sending ? '#d08030' : '#90b4cc', fontSize: 10, letterSpacing: 1.5 }}>
          {sending ? '⟳ TRANSMITTING' : '⌨ UPLINK CMD'}
        </span>
        {!sending && (
          <span style={{
            color: '#6a8090', fontSize: 9, letterSpacing: 1,
            border: '1px solid #1e3040', padding: '1px 5px', borderRadius: 2,
          }}>
            ⌃K
          </span>
        )}
      </button>
    )
  }

  return (
    <>
      {/* Palette — floats top-center, no backdrop, globe stays visible */}
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Uplink command palette"
        style={{
          position: 'fixed',
          top: 56,
          left: '50%',
          transform: 'translateX(-50%)',
          width: 640,
          zIndex: 300,
          background: '#07111f',
          border: `1px solid ${confirming ? '#c03030' : '#1e3040'}`,
          borderRadius: 8,
          boxShadow: confirming
            ? '0 20px 60px rgba(200,32,48,0.3), 0 0 0 1px rgba(200,32,48,0.2)'
            : '0 20px 60px rgba(0,0,0,0.85), 0 0 0 1px rgba(255,255,255,0.04)',
          overflow: 'hidden',
          transition: 'border-color 0.15s, box-shadow 0.15s',
        }}
      >
        {confirming ? (
          /* ── Confirmation view ── */
          <>
            <div style={{ padding: '14px 18px', background: 'rgba(200,32,48,0.07)', borderBottom: '1px solid rgba(200,32,48,0.25)' }}>
              <div style={{ color: '#c04040', fontSize: 9, letterSpacing: 2, marginBottom: 5, fontWeight: 700 }}>
                ⚠ DESTRUCTIVE — CONFIRMATION REQUIRED
              </div>
              <div style={{ color: '#e0eef8', fontSize: 13, fontWeight: 700, letterSpacing: 0.5, marginBottom: 3 }}>
                {confirming.category}: {confirming.label}
              </div>
              <div style={{ color: '#8ab0c0', fontSize: 10 }}>{confirming.description}</div>
              <div style={{ color: '#4a6a7a', fontSize: 9, marginTop: 4, fontFamily: 'monospace' }}>
                TC cmd_id=0x{confirming.commandId.toString(16).padStart(2,'0')}
                {confirming.payloadBytes?.length ? ' payload=[' + confirming.payloadBytes.map(b => '0x'+b.toString(16).padStart(2,'0')).join(',') + ']' : ''}
              </div>
            </div>
            <div style={{ padding: '14px 18px', borderBottom: '1px solid #0e1a28' }}>
              <div style={{ color: '#8ab0c0', fontSize: 10, letterSpacing: 0.5, marginBottom: 10 }}>
                Type <span style={{ color: '#c04040', fontWeight: 700 }}>CONFIRM</span> and press Enter to transmit — Esc to cancel
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
                  style={{
                    flex: 1, background: 'transparent', border: 'none', outline: 'none',
                    color: confirmInput.trim().toUpperCase() === 'CONFIRM' ? '#c04040' : '#e0eef8',
                    fontSize: 13, fontFamily: 'monospace', letterSpacing: 2,
                  }}
                />
                <button
                  onClick={() => { if (confirmInput.trim().toUpperCase() === 'CONFIRM') transmit(confirming) }}
                  disabled={confirmInput.trim().toUpperCase() !== 'CONFIRM'}
                  style={{
                    padding: '5px 14px', fontSize: 10, letterSpacing: 1,
                    background: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'rgba(200,32,48,0.15)' : 'transparent',
                    border: `1px solid ${confirmInput.trim().toUpperCase() === 'CONFIRM' ? '#c04040' : '#1e3040'}`,
                    color: confirmInput.trim().toUpperCase() === 'CONFIRM' ? '#c04040' : '#4a6a7a',
                    cursor: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'pointer' : 'not-allowed',
                    borderRadius: 4,
                  }}
                >
                  TRANSMIT
                </button>
              </div>
            </div>
            <div style={{ padding: '8px 18px' }}>
              <button
                onClick={() => { setConfirming(null); setConfirmInput('') }}
                style={{ background: 'none', border: 'none', color: '#8ab0c0', fontSize: 10, cursor: 'pointer', letterSpacing: 0.5 }}
              >
                ← Cancel
              </button>
            </div>
          </>
        ) : (
          /* ── Search view ── */
          <>
            {/* Input bar */}
            <div style={{
              display: 'flex', alignItems: 'center',
              padding: '12px 16px',
              borderBottom: '1px solid #0e1a28', gap: 10,
            }}>
              <span style={{ color: '#4a6a7a', fontSize: 13, fontWeight: 300, flexShrink: 0, fontFamily: 'monospace' }}>
                {mode === 'satellite' ? (query.startsWith('#') ? '#' : '@') : '>'}
              </span>
              <input
                ref={inputRef}
                value={query}
                onChange={e => { setQuery(e.target.value); setSelected(0) }}
                onKeyDown={handleKey}
                placeholder={
                  mode === 'satellite'
                    ? (query.startsWith('#') ? 'Search ground stations...' : 'Search satellites...')
                    : 'Search commands...'
                }
                style={{
                  flex: 1, background: 'transparent', border: 'none', outline: 'none',
                  color: '#e0eef8', fontSize: 13,
                  '::placeholder': { color: '#4a6a7a' },
                } as React.CSSProperties}
              />
              <button
                onClick={() => setOpen(false)}
                style={{ background: 'none', border: '1px solid #1e3040', color: '#8ab0c0', fontSize: 9, padding: '2px 6px', borderRadius: 3, cursor: 'pointer', letterSpacing: 1, flexShrink: 0 }}
              >
                ESC
              </button>
            </div>

            {/* Results */}
            <div ref={listRef} style={{ maxHeight: 380, overflowY: 'auto' }}>

              {/* Recently used */}
              {showRecent && (
                <>
                  <div style={{ padding: '8px 16px 4px', color: '#4a6a7a', fontSize: 9, letterSpacing: 2, fontWeight: 700 }}>
                    RECENTLY USED
                  </div>
                  {recentCommands.map((cmd, i) => (
                    <CommandRow
                      key={cmd.id} cmd={cmd} selected={i === selected}
                      onHover={() => setSelected(i)}
                      onClick={() => selectCommand(cmd)}
                    />
                  ))}
                  <div style={{ height: 1, background: '#0e1a28', margin: '4px 0' }} />
                </>
              )}

              {/* Command mode */}
              {mode === 'command' && (
                <>
                  {filteredCommands.length === 0 ? (
                    <div style={{ padding: '24px 16px', color: '#4a6a7a', fontSize: 10, textAlign: 'center' }}>
                      No commands match "{searchTerm}"
                    </div>
                  ) : (
                    <>
                      {searchTerm && (
                        <div style={{ padding: '8px 16px 4px', color: '#4a6a7a', fontSize: 9, letterSpacing: 2, fontWeight: 700 }}>
                          COMMANDS
                        </div>
                      )}
                      {filteredCommands.map((cmd, i) => {
                        const idx = showRecent ? recentCommands.length + i : i
                        return (
                          <CommandRow
                            key={cmd.id} cmd={cmd} selected={idx === selected}
                            onHover={() => setSelected(idx)}
                            onClick={() => selectCommand(cmd)}
                          />
                        )
                      })}
                    </>
                  )}
                </>
              )}

              {/* Satellite/GS mode */}
              {mode === 'satellite' && (
                <>
                  {filteredNodes.length === 0 ? (
                    <div style={{ padding: '24px 16px', color: '#4a6a7a', fontSize: 10, textAlign: 'center' }}>
                      No results for "{searchTerm}"
                    </div>
                  ) : (
                    filteredNodes.map((node, i) => (
                      <button
                        key={node.id}
                        onClick={() => selectNode(node)}
                        onMouseEnter={() => setSelected(i)}
                        style={{
                          width: '100%', display: 'flex', alignItems: 'center', gap: 12,
                          padding: '10px 16px', border: 'none', cursor: 'pointer', textAlign: 'left',
                          background: i === selected ? 'rgba(144,180,204,0.07)' : 'transparent',
                          borderLeft: i === selected ? '2px solid #90b4cc' : '2px solid transparent',
                        }}
                      >
                        <span style={{
                          fontSize: 8, letterSpacing: 1, padding: '2px 6px',
                          border: `1px solid ${node.kind === 'satellite' ? '#50b8d0' : '#40d080'}`,
                          color: node.kind === 'satellite' ? '#50b8d0' : '#40d080',
                          borderRadius: 2, flexShrink: 0, minWidth: 52, textAlign: 'center',
                        }}>
                          {node.kind === 'satellite' ? 'SAT' : 'GS'}
                        </span>
                        <span style={{ flex: 1 }}>
                          <div style={{ color: '#e0eef8', fontSize: 11, fontWeight: 600, letterSpacing: 0.3 }}>
                            {node.label}
                          </div>
                          <div style={{ color: '#8ab0c0', fontSize: 9, marginTop: 1 }}>
                            {node.description}
                          </div>
                        </span>
                        <span style={{ color: '#4a6a7a', fontSize: 9 }}>↵</span>
                      </button>
                    ))
                  )}
                </>
              )}

              {/* TX log */}
              {log.length > 0 && (
                <div style={{ borderTop: '1px solid #0e1a28', background: '#050e1c', padding: '8px 16px', maxHeight: 100, overflowY: 'auto' }}>
                  {log.slice(-8).map((e, i) => (
                    <div key={i} style={{ display: 'flex', gap: 10, fontSize: 9, lineHeight: 1.8, fontFamily: 'monospace' }}>
                      <span style={{ color: '#3a5568', flexShrink: 0 }}>
                        {new Date(e.ts).toLocaleTimeString('en-US', { hour12: false })}
                      </span>
                      <span style={{ color: e.type === 'sent' ? '#50b8d0' : e.type === 'ack' ? '#40d080' : '#e05050' }}>
                        {e.text}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Footer hints */}
            <div style={{
              borderTop: '1px solid #0e1a28', padding: '7px 16px',
              display: 'flex', gap: 0, alignItems: 'center',
              background: '#050e1c',
            }}>
              {[
                ['↑↓', 'navigate'],
                ['↵', 'select'],
                ['>', 'commands'],
                ['@', 'satellites'],
                ['#', 'ground stations'],
              ].map(([key, label]) => (
                <span key={key} style={{ display: 'flex', alignItems: 'center', gap: 4, marginRight: 14 }}>
                  <span style={{
                    color: '#8ab0c8', fontSize: 8, fontFamily: 'monospace',
                    border: '1px solid #1e3040', padding: '1px 4px', borderRadius: 3,
                    letterSpacing: 0,
                  }}>{key}</span>
                  <span style={{ color: '#4a6a7a', fontSize: 8, letterSpacing: 0.5 }}>{label}</span>
                </span>
              ))}
              {sending && (
                <span style={{ color: '#d08030', fontSize: 9, marginLeft: 'auto', letterSpacing: 1 }}>
                  TRANSMITTING...
                </span>
              )}
            </div>
          </>
        )}
      </div>

      {/* Dim overlay — just barely, so globe is still visible */}
      <div
        onClick={() => { setOpen(false); setConfirming(null) }}
        style={{ position: 'fixed', inset: 0, zIndex: 299, background: 'rgba(2,7,16,0.35)' }}
      />
    </>
  )
}

function CommandRow({ cmd, selected, onHover, onClick }: {
  cmd: Command
  selected: boolean
  onHover: () => void
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      onMouseEnter={onHover}
      style={{
        width: '100%', display: 'flex', alignItems: 'flex-start', gap: 12,
        padding: '10px 16px', border: 'none', cursor: 'pointer', textAlign: 'left',
        background: selected
          ? cmd.destructive ? 'rgba(200,32,48,0.07)' : 'rgba(144,180,204,0.07)'
          : 'transparent',
        borderLeft: selected
          ? `2px solid ${cmd.destructive ? '#c04040' : '#90b4cc'}`
          : '2px solid transparent',
      }}
    >
      <span style={{
        fontSize: 8, letterSpacing: 1, padding: '2px 6px', marginTop: 1,
        border: `1px solid ${cmd.destructive ? '#c04040' : CATEGORY_COLOR[cmd.category]}`,
        color: cmd.destructive ? '#c04040' : CATEGORY_COLOR[cmd.category],
        borderRadius: 2, flexShrink: 0, minWidth: 72, textAlign: 'center',
      }}>
        {cmd.destructive ? '⚠ ' : ''}{cmd.category}
      </span>
      <span style={{ flex: 1 }}>
        <div style={{ color: cmd.destructive ? '#c04040' : '#e0eef8', fontSize: 11, fontWeight: 600, letterSpacing: 0.3 }}>
          {cmd.label}
        </div>
        <div style={{ color: '#8ab0c0', fontSize: 9, marginTop: 2 }}>
          {cmd.description}
        </div>
        <div style={{ color: '#3a5568', fontSize: 8, marginTop: 2, fontFamily: 'monospace' }}>
          TC 0x{cmd.commandId.toString(16).padStart(2,'0')}
          {cmd.payloadBytes?.length ? ' [' + cmd.payloadBytes.map(b => '0x'+b.toString(16).padStart(2,'0')).join(',') + ']' : ''}
        </div>
      </span>
      <span style={{ color: '#3a5568', fontSize: 9, paddingTop: 1 }}>
        {cmd.destructive ? '⚠↵' : '↵'}
      </span>
    </button>
  )
}
