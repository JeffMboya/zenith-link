import { useEffect, useState } from 'react'
import type { AutonomousEvent } from '../types'

const CLASS_COLOR: Record<string, string> = {
  NOMINAL: 'var(--green)',
  ECLIPSE_ENTRY: 'var(--cyan)',
  ECLIPSE_COMPUTE: 'var(--purple)',
  POWER_ANOMALY: 'var(--red)',
  THERMAL_EVENT: 'var(--amber)',
  ATTITUDE_INSTABILITY: 'var(--amber)',
  RF_DEGRADATION: 'var(--amber)',
}

export function AutonomyFeed() {
  const [events, setEvents] = useState<AutonomousEvent[]>([])
  const [open, setOpen] = useState(true)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const res = await fetch('/events')
        if (res.ok) {
          const data = await res.json() as AutonomousEvent[] | null
          if (Array.isArray(data)) setEvents(data.slice(0, 8))
        }
      } catch {  }
      timer = setTimeout(poll, 3000)
    }
    poll()
    return () => clearTimeout(timer)
  }, [])

  if (events.length === 0) return null

  return (
    <div style={{
      position: 'fixed', bottom: 48, left: 0, width: open ? 340 : 32,
      background: 'rgba(4,13,28,0.90)', borderRight: '1px solid var(--border)',
      borderTop: '1px solid var(--border)', backdropFilter: 'blur(6px)',
      zIndex: 85, transition: 'width 0.2s ease', overflow: 'hidden',
    }}>
      
      <button
        onClick={() => setOpen(o => !o)}
        aria-label={open ? 'Collapse autonomy feed' : 'Expand autonomy feed'}
        style={{
          position: 'absolute', right: -20, top: 0, width: 20, height: 32,
          background: 'var(--bg-deep)', border: '1px solid var(--border)', borderLeft: 'none',
          color: 'var(--text-mid)', cursor: 'pointer', fontSize: 10,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          borderRadius: '0 4px 4px 0',
        }}
      >
        {open ? '‹' : '›'}
      </button>

      {open && (
        <div>
          <div style={{
            padding: '6px 10px', borderBottom: '1px solid var(--border)',
            color: 'var(--purple)', fontSize: 8, letterSpacing: 3,
            display: 'flex', alignItems: 'center', gap: 6,
          }}>
            <div style={{ width: 5, height: 5, borderRadius: '50%', background: 'var(--purple)', boxShadow: '0 0 4px var(--purple)' }} />
            AUTONOMOUS EVENTS
          </div>
          <div style={{ maxHeight: 240, overflowY: 'auto' }}>
            {events.map((ev, i) => {
              const color = CLASS_COLOR[ev.class] ?? 'var(--text-dim)'
              const ts = new Date(ev.at).toISOString().slice(11, 19) + 'Z'
              return (
                <div key={i} style={{
                  padding: '5px 10px',
                  borderBottom: '1px solid var(--bg-dark)',
                  opacity: 1 - i * 0.1,
                }}>
                  <div style={{ display: 'flex', gap: 6, alignItems: 'baseline', marginBottom: 2 }}>
                    <span style={{ color: 'var(--text-dim)', fontSize: 7, fontFamily: 'var(--font-mono)', flexShrink: 0 }}>{ts}</span>
                    <span style={{ color, fontSize: 8, fontWeight: 700, letterSpacing: 1 }}>{ev.class}</span>
                  </div>
                  <div style={{ color: 'var(--text-mid)', fontSize: 8, lineHeight: 1.4 }}>{ev.action}</div>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
