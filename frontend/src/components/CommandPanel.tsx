import { useState, useRef, useEffect } from 'react'

interface LogEntry {
  ts: number
  text: string
  type: 'sent' | 'ack' | 'error'
}

const PRESETS = ['SAFE_MODE', 'NOMINAL', 'REBOOT_OBC', 'SET_THRESHOLD_PITCH 0.02', 'DUMP_TELEMETRY']

export function CommandPanel() {
  const [open, setOpen] = useState(false)
  const [input, setInput] = useState('')
  const [log, setLog] = useState<LogEntry[]>([])
  const [sending, setSending] = useState(false)
  const logRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [log])

  const send = async (cmd: string) => {
    const text = cmd.trim()
    if (!text || sending) return
    setSending(true)
    setLog(l => [...l, { ts: Date.now(), text, type: 'sent' }])
    setInput('')

    try {
      const res = await fetch('/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: text }),
      })
      const body = await res.json()
      setLog(l => [...l, {
        ts: Date.now(),
        text: body.status === 'queued' ? `ACK seq=${body.seq}` : `ERROR: ${body.reason ?? 'unknown'}`,
        type: body.status === 'queued' ? 'ack' : 'error',
      }])
    } catch {
      setLog(l => [...l, { ts: Date.now(), text: 'SEND FAILED — link down?', type: 'error' }])
    } finally {
      setSending(false)
    }
  }

  const typeColor: Record<LogEntry['type'], string> = {
    sent: 'var(--cyan)',
    ack: 'var(--green)',
    error: 'var(--red)',
  }

  return (
    <div style={{
      position: 'fixed', bottom: 0, left: 0, zIndex: 90,
      width: open ? 320 : 120,
      background: 'rgba(4,13,28,0.90)', borderTop: '1px solid var(--border)', borderRight: '1px solid var(--border)',
      backdropFilter: 'blur(6px)',
      transition: 'width 0.2s ease', borderRadius: '0 4px 0 0',
    }}>
      {/* Header */}
      <button
        onClick={() => setOpen(o => !o)}
        aria-label={open ? 'Collapse uplink command panel' : 'Expand uplink command panel'}
        style={{
          width: '100%', padding: '6px 12px', background: 'none', border: 'none',
          display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer',
          borderBottom: open ? '1px solid var(--border)' : 'none',
        }}
      >
        <span style={{ color: 'var(--amber)', fontSize: 9, letterSpacing: 2 }}>⌨ UPLINK CMD</span>
        {!open && <span style={{ color: 'var(--text-dim)', fontSize: 9 }}>▲</span>}
      </button>

      {open && (
        <div style={{ padding: 10 }}>
          {/* Log */}
          <div ref={logRef} style={{
            height: 100, overflowY: 'auto', marginBottom: 8,
            fontSize: 10, lineHeight: 1.6,
          }}>
            {log.length === 0 && (
              <div style={{ color: 'var(--text-dim)' }}>No commands sent yet.</div>
            )}
            {log.map((e, i) => (
              <div key={i} style={{ color: typeColor[e.type] }}>
                <span style={{ color: 'var(--text-dim)', marginRight: 6 }}>
                  {new Date(e.ts).toLocaleTimeString('en-US', { hour12: false })}
                </span>
                {e.text}
              </div>
            ))}
          </div>

          {/* Presets */}
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginBottom: 8 }}>
            {PRESETS.map(p => (
              <button key={p} onClick={() => send(p)} aria-label={`Send preset command: ${p}`} style={{
                background: 'var(--bg-deep)', border: '1px solid var(--border)', color: 'var(--text-mid)',
                fontSize: 9, padding: '2px 6px', cursor: 'pointer', borderRadius: 2,
                letterSpacing: 1,
              }}>
                {p}
              </button>
            ))}
          </div>

          {/* Input */}
          <div style={{ display: 'flex', gap: 4 }}>
            <input
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && send(input)}
              placeholder="COMMAND..."
              aria-label="Uplink command input"
              style={{
                flex: 1, background: 'var(--bg-deep)', border: '1px solid var(--border)',
                color: 'var(--text-body)', fontSize: 10, padding: '4px 8px',
                fontFamily: 'var(--font-mono)', outline: 'none',
              }}
            />
            <button
              onClick={() => send(input)}
              disabled={sending}
              aria-label="Transmit command"
              style={{
                background: sending ? 'var(--bg-deep)' : 'var(--bg-dark)',
                border: '1px solid var(--border)', color: sending ? 'var(--text-dim)' : 'var(--cyan)',
                fontSize: 9, padding: '4px 10px', cursor: sending ? 'not-allowed' : 'pointer', letterSpacing: 1,
              }}
            >
              TX
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
