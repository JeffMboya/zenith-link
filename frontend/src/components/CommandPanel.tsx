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
  payload: string
  destructive?: boolean   // requires CONFIRM gate before transmitting
}

const COMMANDS: Command[] = [
  { id: 'safe_mode',  label: 'SAFE MODE',          description: 'Reduce power, halt non-essential subsystems', category: 'MODE',       payload: 'SAFE_MODE',                destructive: true },
  { id: 'nominal',    label: 'NOMINAL',             description: 'Return to nominal operations',                category: 'MODE',       payload: 'NOMINAL' },
  { id: 'reboot_obc', label: 'REBOOT OBC',          description: 'Soft-reset onboard computer (seq loss)',      category: 'SYSTEM',     payload: 'REBOOT_OBC',               destructive: true },
  { id: 'dump_tlm',   label: 'DUMP TELEMETRY',      description: 'Force full telemetry frame downlink',         category: 'TELEMETRY',  payload: 'DUMP_TELEMETRY' },
  { id: 'set_pitch',  label: 'SET PITCH THRESHOLD', description: 'Set attitude pitch deadband (0.02 default)',  category: 'DIAGNOSTIC', payload: 'SET_THRESHOLD_PITCH 0.02' },
  { id: 'inference',  label: 'RUN INFERENCE',       description: 'Trigger onboard AI earth observation',        category: 'DIAGNOSTIC', payload: 'INFERENCE_RUN' },
]

const CATEGORY_COLOR: Record<Command['category'], string> = {
  MODE:       'var(--cyan)',
  SYSTEM:     'var(--amber)',
  TELEMETRY:  'var(--green)',
  DIAGNOSTIC: 'var(--purple)',
}

const LOG_COLOR: Record<LogEntry['type'], string> = {
  sent:  'var(--cyan)',
  ack:   'var(--green)',
  error: 'var(--red)',
}

export function CommandPanel() {
  const [open, setOpen]           = useState(false)
  const [query, setQuery]         = useState('')
  const [selected, setSelected]   = useState(0)
  const [log, setLog]             = useState<LogEntry[]>([])
  const [sending, setSending]     = useState(false)
  // Confirm gate: holds the command awaiting CONFIRM; null when idle
  const [confirming, setConfirming] = useState<Command | null>(null)
  const [confirmInput, setConfirmInput] = useState('')
  const [showLog, setShowLog]     = useState(false)

  const inputRef   = useRef<HTMLInputElement>(null)
  const confirmRef = useRef<HTMLInputElement>(null)
  const logRef     = useRef<HTMLDivElement>(null)

  // Global keyboard: Ctrl+K toggle, Escape closes
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

  // Focus: search input on open, confirm input when confirming
  useEffect(() => {
    if (open && !confirming) {
      setQuery(''); setSelected(0)
      setTimeout(() => inputRef.current?.focus(), 50)
    }
  }, [open, confirming])

  useEffect(() => {
    if (confirming) setTimeout(() => confirmRef.current?.focus(), 50)
  }, [confirming])

  // Scroll log to bottom
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [log])

  // Show log section automatically when a new entry arrives
  useEffect(() => {
    if (log.length > 0) setShowLog(true)
  }, [log.length])

  const filtered = COMMANDS.filter(c =>
    !query ||
    c.label.toLowerCase().includes(query.toLowerCase()) ||
    c.description.toLowerCase().includes(query.toLowerCase()) ||
    c.category.toLowerCase().includes(query.toLowerCase())
  )

  const transmit = useCallback(async (cmd: Command) => {
    if (sending) return
    setSending(true)
    setConfirming(null)
    setConfirmInput('')
    setOpen(false)
    setLog(l => [...l, { ts: Date.now(), text: `TX  ${cmd.label}`, type: 'sent' }])

    try {
      const res = await fetch('/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: cmd.payload }),
      })
      const body = await res.json() as { status: string; seq?: number; message?: string; reason?: string }
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

  const selectCommand = useCallback((cmd: Command) => {
    if (cmd.destructive) {
      setConfirming(cmd)
      setConfirmInput('')
    } else {
      transmit(cmd)
    }
  }, [transmit])

  const handleSearchKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSelected(s => Math.min(s + 1, filtered.length - 1)) }
    if (e.key === 'ArrowUp')   { e.preventDefault(); setSelected(s => Math.max(s - 1, 0)) }
    if (e.key === 'Enter' && filtered[selected]) selectCommand(filtered[selected])
  }

  const handleConfirmKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && confirmInput.trim().toUpperCase() === 'CONFIRM' && confirming) {
      transmit(confirming)
    }
    if (e.key === 'Escape') {
      setConfirming(null)
      setConfirmInput('')
    }
  }

  const closeModal = () => {
    setOpen(false)
    setConfirming(null)
    setConfirmInput('')
  }

  return (
    <>
      {/* Trigger button — bottom-left, shows TX status when sending */}
      <button
        onClick={() => setOpen(true)}
        aria-label="Open command palette (Ctrl+K)"
        style={{
          position: 'fixed', bottom: 0, left: 0, zIndex: 90,
          padding: '6px 14px',
          background: 'rgba(4,13,28,0.90)',
          borderTop: `1px solid ${sending ? 'var(--amber)' : 'var(--border)'}`,
          borderRight: '1px solid var(--border)',
          backdropFilter: 'blur(6px)', borderRadius: '0 4px 0 0',
          display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer',
          transition: 'border-color 0.2s',
        }}
      >
        <span style={{ color: sending ? 'var(--amber)' : 'var(--amber)', fontSize: 9, letterSpacing: 2 }}>
          {sending ? '⟳ TRANSMITTING' : '⌨ UPLINK CMD'}
        </span>
        {!sending && (
          <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1, border: '1px solid var(--border)', padding: '1px 4px', borderRadius: 2 }}>
            CTRL K
          </span>
        )}
      </button>

      {/* Modal */}
      {open && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="Uplink command palette"
          onClick={closeModal}
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
              border: `1px solid ${confirming ? 'var(--red)' : 'var(--border)'}`,
              borderRadius: 6,
              boxShadow: confirming
                ? '0 24px 64px rgba(240,48,64,0.25)'
                : '0 24px 64px rgba(0,0,0,0.8)',
              overflow: 'hidden',
              transition: 'border-color 0.15s, box-shadow 0.15s',
            }}
          >
            {/* ── CONFIRM GATE ── */}
            {confirming ? (
              <>
                <div style={{
                  padding: '12px 16px',
                  background: 'rgba(240,48,64,0.08)',
                  borderBottom: '1px solid rgba(240,48,64,0.3)',
                }}>
                  <div style={{ color: 'var(--red)', fontSize: 9, letterSpacing: 2, marginBottom: 4 }}>
                    ⚠ DESTRUCTIVE COMMAND — CONFIRMATION REQUIRED
                  </div>
                  <div style={{ color: 'var(--text-body)', fontSize: 12, fontWeight: 700, letterSpacing: 1 }}>
                    {confirming.label}
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 9, marginTop: 2 }}>
                    {confirming.description}
                  </div>
                  <div style={{ color: 'var(--text-dim)', fontSize: 9, marginTop: 2, fontFamily: 'var(--font-mono)' }}>
                    payload: <span style={{ color: 'var(--amber)' }}>{confirming.payload}</span>
                  </div>
                </div>
                <div style={{ padding: '12px 16px', borderBottom: '1px solid var(--bg-dark)' }}>
                  <div style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1, marginBottom: 8 }}>
                    TYPE <span style={{ color: 'var(--red)', fontWeight: 700 }}>CONFIRM</span> AND PRESS ENTER TO TRANSMIT — ESC TO CANCEL
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <span style={{ color: 'var(--red)', fontSize: 12 }}>›_</span>
                    <input
                      ref={confirmRef}
                      value={confirmInput}
                      onChange={e => setConfirmInput(e.target.value)}
                      onKeyDown={handleConfirmKey}
                      placeholder="CONFIRM"
                      style={{
                        flex: 1, background: 'transparent', border: 'none', outline: 'none',
                        color: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'var(--red)' : 'var(--text-body)',
                        fontSize: 13, fontFamily: 'var(--font-mono)', letterSpacing: 2,
                      }}
                    />
                    <button
                      onClick={() => { if (confirmInput.trim().toUpperCase() === 'CONFIRM') transmit(confirming) }}
                      disabled={confirmInput.trim().toUpperCase() !== 'CONFIRM'}
                      aria-label="Confirm and transmit destructive command"
                      style={{
                        padding: '4px 12px', fontSize: 9, letterSpacing: 1,
                        background: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'rgba(240,48,64,0.15)' : 'transparent',
                        border: `1px solid ${confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'var(--red)' : 'var(--border)'}`,
                        color: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'var(--red)' : 'var(--text-dim)',
                        cursor: confirmInput.trim().toUpperCase() === 'CONFIRM' ? 'pointer' : 'not-allowed',
                        borderRadius: 2,
                      }}
                    >
                      TRANSMIT
                    </button>
                  </div>
                </div>
                <div style={{ padding: '8px 16px' }}>
                  <button
                    onClick={() => { setConfirming(null); setConfirmInput('') }}
                    style={{
                      background: 'none', border: 'none', color: 'var(--text-dim)',
                      fontSize: 9, letterSpacing: 1, cursor: 'pointer',
                    }}
                  >
                    ← CANCEL
                  </button>
                </div>
              </>
            ) : (
              <>
                {/* ── SEARCH ── */}
                <div style={{ display: 'flex', alignItems: 'center', padding: '12px 16px', borderBottom: '1px solid var(--bg-dark)', gap: 10 }}>
                  <span style={{ color: 'var(--text-dim)', fontSize: 12 }}>›_</span>
                  <input
                    ref={inputRef}
                    value={query}
                    onChange={e => { setQuery(e.target.value); setSelected(0) }}
                    onKeyDown={handleSearchKey}
                    placeholder="ENTER COMMAND OR SEARCH..."
                    style={{
                      flex: 1, background: 'transparent', border: 'none', outline: 'none',
                      color: 'var(--text-body)', fontSize: 13, fontFamily: 'var(--font-mono)',
                    }}
                  />
                  <span style={{ color: 'var(--text-dim)', fontSize: 9, letterSpacing: 1 }}>ESC TO CLOSE</span>
                </div>

                {/* ── COMMAND LIST ── */}
                <div style={{ maxHeight: 300, overflowY: 'auto' }}>
                  {filtered.length === 0 && (
                    <div style={{ padding: '20px 16px', color: 'var(--text-dim)', fontSize: 10, textAlign: 'center' }}>
                      NO COMMANDS MATCH
                    </div>
                  )}
                  {filtered.map((cmd, i) => (
                    <button
                      key={cmd.id}
                      onClick={() => selectCommand(cmd)}
                      onMouseEnter={() => setSelected(i)}
                      aria-label={`${cmd.destructive ? 'Destructive: ' : ''}Send command: ${cmd.label}`}
                      style={{
                        width: '100%', display: 'flex', alignItems: 'flex-start', gap: 12,
                        padding: '10px 16px', border: 'none', cursor: 'pointer', textAlign: 'left',
                        background: i === selected
                          ? cmd.destructive ? 'rgba(240,48,64,0.06)' : 'rgba(0,200,240,0.07)'
                          : 'transparent',
                        borderLeft: i === selected
                          ? `2px solid ${cmd.destructive ? 'var(--red)' : 'var(--cyan)'}`
                          : '2px solid transparent',
                      }}
                    >
                      <span style={{
                        fontSize: 8, letterSpacing: 1, padding: '2px 5px', marginTop: 2,
                        border: `1px solid ${cmd.destructive ? 'var(--red)' : CATEGORY_COLOR[cmd.category]}`,
                        color: cmd.destructive ? 'var(--red)' : CATEGORY_COLOR[cmd.category],
                        borderRadius: 2, flexShrink: 0, minWidth: 72, textAlign: 'center',
                      }}>
                        {cmd.destructive ? '⚠ ' : ''}{cmd.category}
                      </span>
                      <span style={{ flex: 1 }}>
                        <div style={{
                          color: cmd.destructive ? 'var(--red)' : 'var(--text-body)',
                          fontSize: 11, fontWeight: 700, letterSpacing: 1,
                        }}>
                          {cmd.label}
                        </div>
                        <div style={{ color: 'var(--text-dim)', fontSize: 9, marginTop: 1 }}>
                          {cmd.description}
                        </div>
                        {/* Payload preview */}
                        <div style={{ color: 'var(--text-dim)', fontSize: 8, marginTop: 2, opacity: 0.7 }}>
                          payload: <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-mid)' }}>{cmd.payload}</span>
                        </div>
                      </span>
                      <span style={{ color: 'var(--text-dim)', fontSize: 9, paddingTop: 2 }}>
                        {cmd.destructive ? '⚠↵' : '↵'}
                      </span>
                    </button>
                  ))}
                </div>

                {/* ── KEYBOARD HINTS + LOG TOGGLE ── */}
                <div style={{
                  borderTop: '1px solid var(--bg-dark)', padding: '6px 16px',
                  display: 'flex', gap: 16, alignItems: 'center',
                }}>
                  <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>↑↓ NAVIGATE</span>
                  <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>↵ TRANSMIT</span>
                  <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1 }}>⚠ = CONFIRM REQUIRED</span>
                  {log.length > 0 && (
                    <button
                      onClick={() => setShowLog(s => !s)}
                      style={{
                        marginLeft: 'auto', background: 'none', border: 'none',
                        color: 'var(--text-dim)', fontSize: 8, letterSpacing: 1,
                        cursor: 'pointer',
                      }}
                    >
                      {showLog ? '▼ HIDE LOG' : `▲ TX LOG (${log.length})`}
                    </button>
                  )}
                  {sending && (
                    <span style={{ color: 'var(--amber)', fontSize: 8, letterSpacing: 1, marginLeft: 'auto' }}>
                      TRANSMITTING...
                    </span>
                  )}
                </div>

                {/* ── EMBEDDED TX LOG ── */}
                {showLog && log.length > 0 && (
                  <div
                    ref={logRef}
                    style={{
                      borderTop: '1px solid var(--bg-dark)',
                      maxHeight: 120, overflowY: 'auto',
                      padding: '6px 16px',
                      background: 'rgba(2,7,16,0.4)',
                    }}
                  >
                    {log.slice(-20).map((e, i) => (
                      <div key={i} style={{ display: 'flex', gap: 10, fontSize: 9, lineHeight: 1.7 }}>
                        <span style={{ color: 'var(--text-dim)', flexShrink: 0 }}>
                          {new Date(e.ts).toLocaleTimeString('en-US', { hour12: false })}
                        </span>
                        <span style={{ color: LOG_COLOR[e.type] }}>{e.text}</span>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      )}
    </>
  )
}
