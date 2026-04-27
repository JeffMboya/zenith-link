import { useState, useRef, useEffect, useCallback } from 'react'

interface LogEntry {
  ts: number
  text: string
  type: 'sent' | 'ack' | 'error'
}

interface Command {
  id: string
  label: string
  description: string
  category: 'MODE' | 'SYSTEM' | 'TELEMETRY' | 'DIAGNOSTIC'
  payload?: string   // the actual command string sent to the spacecraft
}

const COMMANDS: Command[] = [
  { id: 'safe_mode',    label: 'SAFE MODE',            description: 'Reduce power, halt non-essential subsystems', category: 'MODE',       payload: 'SAFE_MODE' },
  { id: 'nominal',      label: 'NOMINAL',               description: 'Return to nominal operations',               category: 'MODE',       payload: 'NOMINAL' },
  { id: 'reboot_obc',   label: 'REBOOT OBC',            description: 'Soft-reset onboard computer (seq loss)',     category: 'SYSTEM',     payload: 'REBOOT_OBC' },
  { id: 'dump_tlm',     label: 'DUMP TELEMETRY',        description: 'Force full telemetry frame downlink',        category: 'TELEMETRY',  payload: 'DUMP_TELEMETRY' },
  { id: 'set_pitch',    label: 'SET PITCH THRESHOLD',   description: 'Set attitude pitch deadband (0.02 default)', category: 'DIAGNOSTIC', payload: 'SET_THRESHOLD_PITCH 0.02' },
  { id: 'inference',    label: 'RUN INFERENCE',         description: 'Trigger onboard AI earth observation',       category: 'DIAGNOSTIC', payload: 'INFERENCE_RUN' },
]

const CATEGORY_COLOR: Record<Command['category'], string> = {
  MODE:       'var(--cyan)',
  SYSTEM:     'var(--amber)',
  TELEMETRY:  'var(--green)',
  DIAGNOSTIC: 'var(--purple)',
}

type LogColor = Record<LogEntry['type'], string>
const LOG_COLOR: LogColor = {
  sent: 'var(--cyan)',
  ack:  'var(--green)',
  error: 'var(--red)',
}

export function CommandPanel() {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const [log, setLog] = useState<LogEntry[]>([])
  const [sending, setSending] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const logRef = useRef<HTMLDivElement>(null)

  // Open with Ctrl+K / Cmd+K
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        setOpen(o => !o)
      }
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  // Focus input when opened
  useEffect(() => {
    if (open) {
      setQuery('')
      setSelected(0)
      setTimeout(() => inputRef.current?.focus(), 50)
    }
  }, [open])

  // Scroll log to bottom
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [log])

  const filtered = COMMANDS.filter(c =>
    !query || c.label.toLowerCase().includes(query.toLowerCase()) ||
    c.description.toLowerCase().includes(query.toLowerCase()) ||
    c.category.toLowerCase().includes(query.toLowerCase())
  )

  const send = useCallback(async (cmd: Command | string) => {
    const payload = typeof cmd === 'string' ? cmd : (cmd.payload ?? cmd.label)
    const label = typeof cmd === 'string' ? cmd : cmd.label
    if (!payload || sending) return
    setSending(true)
    setLog(l => [...l, { ts: Date.now(), text: `TX  ${label}`, type: 'sent' }])
    setOpen(false)
    setQuery('')

    try {
      const res = await fetch('/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: payload }),
      })
      const body = await res.json()
      setLog(l => [...l, {
        ts: Date.now(),
        text: body.status === 'queued'
          ? `ACK seq=${body.seq}${body.message ? ' · ' + body.message : ''}`
          : `ERR ${body.reason ?? 'unknown'}`,
        type: body.status === 'queued' ? 'ack' : 'error',
      }])
    } catch {
      setLog(l => [...l, { ts: Date.now(), text: 'SEND FAILED — link down?', type: 'error' }])
    } finally {
      setSending(false)
    }
  }, [sending])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSelected(s => Math.min(s + 1, filtered.length - 1)) }
    if (e.key === 'ArrowUp')   { e.preventDefault(); setSelected(s => Math.max(s - 1, 0)) }
    if (e.key === 'Enter' && filtered[selected]) send(filtered[selected])
  }

  return (
    <>
      {/* Trigger button — bottom-left, always visible */}
      <button
        onClick={() => setOpen(true)}
        aria-label="Open command palette (Ctrl+K)"
        style={{
          position: 'fixed', bottom: 0, left: 0, zIndex: 90,
          padding: '6px 14px',
          background: 'rgba(4,13,28,0.90)', borderTop: '1px solid var(--border)', borderRight: '1px solid var(--border)',
          backdropFilter: 'blur(6px)', borderRadius: '0 4px 0 0',
          display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer',
        }}
      >
        <span style={{ color: 'var(--amber)', fontSize: 9, letterSpacing: 2 }}>⌨ UPLINK CMD</span>
        <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1, border: '1px solid var(--border)', padding: '1px 4px', borderRadius: 2 }}>
          CTRL K
        </span>
      </button>

      {/* Command log — small persistent readout above trigger */}
      {log.length > 0 && !open && (
        <div
          ref={logRef}
          style={{
            position: 'fixed', bottom: 30, left: 0, zIndex: 89,
            width: 300, maxHeight: 80, overflowY: 'auto',
            background: 'rgba(4,13,28,0.85)', borderTop: '1px solid var(--border)', borderRight: '1px solid var(--border)',
            backdropFilter: 'blur(6px)', padding: '4px 10px',
            fontSize: 9, lineHeight: 1.7,
          }}
        >
          {log.slice(-4).map((e, i) => (
            <div key={i} style={{ color: LOG_COLOR[e.type], display: 'flex', gap: 8 }}>
              <span style={{ color: 'var(--text-dim)', flexShrink: 0 }}>
                {new Date(e.ts).toLocaleTimeString('en-US', { hour12: false })}
              </span>
              <span>{e.text}</span>
            </div>
          ))}
        </div>
      )}

      {/* Modal palette */}
      {open && (
        <div
          onClick={() => setOpen(false)}
          style={{
            position: 'fixed', inset: 0, zIndex: 200,
            background: 'rgba(2,7,16,0.75)', backdropFilter: 'blur(4px)',
            display: 'flex', alignItems: 'flex-start', justifyContent: 'center',
            paddingTop: '15vh',
          }}
        >
          <div
            onClick={e => e.stopPropagation()}
            style={{
              width: 560, background: 'var(--bg-deep)',
              border: '1px solid var(--border)', borderRadius: 6,
              boxShadow: '0 24px 64px rgba(0,0,0,0.8)',
              overflow: 'hidden',
            }}
          >
            {/* Search input */}
            <div style={{ display: 'flex', alignItems: 'center', padding: '12px 16px', borderBottom: '1px solid var(--bg-dark)', gap: 10 }}>
              <span style={{ color: 'var(--text-dim)', fontSize: 12 }}>›_</span>
              <input
                ref={inputRef}
                value={query}
                onChange={e => { setQuery(e.target.value); setSelected(0) }}
                onKeyDown={handleKeyDown}
                placeholder="Search commands..."
                style={{
                  flex: 1, background: 'transparent', border: 'none', outline: 'none',
                  color: 'var(--text-body)', fontSize: 13, fontFamily: 'var(--font-mono)',
                }}
              />
              <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>ESC TO CLOSE</span>
            </div>

            {/* Command list */}
            <div style={{ maxHeight: 360, overflowY: 'auto' }}>
              {filtered.length === 0 && (
                <div style={{ padding: '20px 16px', color: 'var(--text-dim)', fontSize: 10, textAlign: 'center' }}>
                  NO COMMANDS MATCH
                </div>
              )}
              {filtered.map((cmd, i) => (
                <button
                  key={cmd.id}
                  onClick={() => send(cmd)}
                  onMouseEnter={() => setSelected(i)}
                  aria-label={`Send command: ${cmd.label}`}
                  style={{
                    width: '100%', display: 'flex', alignItems: 'center', gap: 12,
                    padding: '10px 16px', border: 'none', cursor: 'pointer', textAlign: 'left',
                    background: i === selected ? 'rgba(0,200,240,0.07)' : 'transparent',
                    borderLeft: i === selected ? '2px solid var(--cyan)' : '2px solid transparent',
                  }}
                >
                  <span style={{
                    fontSize: 8, letterSpacing: 1, padding: '2px 5px',
                    border: `1px solid ${CATEGORY_COLOR[cmd.category]}`,
                    color: CATEGORY_COLOR[cmd.category], borderRadius: 2, flexShrink: 0, minWidth: 72,
                    textAlign: 'center',
                  }}>
                    {cmd.category}
                  </span>
                  <span style={{ flex: 1 }}>
                    <div style={{ color: 'var(--text-body)', fontSize: 11, fontWeight: 700, letterSpacing: 1 }}>{cmd.label}</div>
                    <div style={{ color: 'var(--text-dim)', fontSize: 9, marginTop: 2 }}>{cmd.description}</div>
                  </span>
                  <span style={{ color: 'var(--text-dim)', fontSize: 9 }}>↵</span>
                </button>
              ))}
            </div>

            {/* Footer */}
            <div style={{
              borderTop: '1px solid var(--bg-dark)', padding: '8px 16px',
              display: 'flex', gap: 16, alignItems: 'center',
            }}>
              <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>↑↓ NAVIGATE</span>
              <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>↵ TRANSMIT</span>
              {sending && <span style={{ color: 'var(--amber)', fontSize: 8, letterSpacing: 1, marginLeft: 'auto' }}>TRANSMITTING...</span>}
            </div>
          </div>
        </div>
      )}
    </>
  )
}
