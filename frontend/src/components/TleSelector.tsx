import { useState, useRef, useEffect } from 'react'
import { useTleImport } from '../hooks/useTleImport'
import type { TleGroup } from '../hooks/useTleImport'

interface Props {
  source: 'sim' | 'tle'
  group: string
  count: number
}

const GROUPS: { key: TleGroup; label: string; limit: number; note: string }[] = [
  { key: 'stations', label: 'ISS / STATIONS',  limit:    0, note: 'ISS, Tiangong, MIR debris' },
  { key: 'planet',   label: 'PLANET LABS',      limit:  100, note: 'Dove earth-observation sats' },
  { key: 'active',   label: 'ACTIVE LEO',        limit:  100, note: 'All active LEO, first 100' },
  { key: 'starlink', label: 'STARLINK',           limit:  100, note: 'SpaceX constellation, first 100' },
]

export function TleSelector({ source, group, count }: Props) {
  const [open, setOpen] = useState(false)
  const { importGroup, clearTle, loading, error } = useTleImport()
  const ref = useRef<HTMLDivElement>(null)

  // Close on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const label = source === 'tle'
    ? `TLE: ${group.toUpperCase()} (${count})`
    : `SIM: ${count} SAT`

  const labelColor = source === 'tle' ? 'var(--green)' : 'var(--text-mid)'

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button
        onClick={() => setOpen(o => !o)}
        aria-label="Switch constellation data source"
        style={{
          padding: '4px 10px',
          background: 'none', border: '1px solid var(--border)',
          color: labelColor, fontSize: 9, letterSpacing: 1, cursor: 'pointer',
          borderRadius: 2, display: 'flex', alignItems: 'center', gap: 5,
          opacity: loading ? 0.6 : 1,
        }}
      >
        {loading ? '...' : label}
        <span style={{ color: 'var(--text-dim)', fontSize: 8 }}>▾</span>
      </button>

      {error && (
        <div style={{
          position: 'absolute', top: '100%', left: 0, zIndex: 300, whiteSpace: 'nowrap',
          background: 'var(--bg-deep)', border: '1px solid var(--red)',
          color: 'var(--red)', fontSize: 9, padding: '4px 8px', borderRadius: 2, marginTop: 2,
        }}>
          {error}
        </div>
      )}

      {open && !loading && (
        <div style={{
          position: 'absolute', top: 'calc(100% + 4px)', left: 0, zIndex: 300,
          background: 'var(--bg-deep)', border: '1px solid var(--border)',
          borderRadius: 4, boxShadow: '0 8px 24px rgba(0,0,0,0.6)',
          minWidth: 220, overflow: 'hidden',
        }}>
          {/* Simulated option */}
          <button
            onClick={async () => { await clearTle(); setOpen(false) }}
            style={{
              width: '100%', padding: '8px 12px', border: 'none', cursor: 'pointer',
              background: source === 'sim' ? 'rgba(0,200,240,0.07)' : 'transparent',
              borderLeft: source === 'sim' ? '2px solid var(--cyan)' : '2px solid transparent',
              textAlign: 'left', display: 'flex', flexDirection: 'column', gap: 2,
            }}
          >
            <span style={{ color: 'var(--cyan)', fontSize: 10, fontWeight: 700, letterSpacing: 1 }}>
              SIMULATED
            </span>
            <span style={{ color: 'var(--text-dim)', fontSize: 8 }}>16 satellites — 4 orbital planes</span>
          </button>

          <div style={{ borderTop: '1px solid var(--bg-dark)', padding: '4px 12px 2px' }}>
            <span style={{ color: 'var(--text-dim)', fontSize: 8, letterSpacing: 2 }}>
              CELESTRAK · LIVE TLE DATA
            </span>
          </div>

          {GROUPS.map(g => (
            <button
              key={g.key}
              onClick={async () => {
                await importGroup(g.key, g.limit)
                setOpen(false)
              }}
              style={{
                width: '100%', padding: '8px 12px', border: 'none', cursor: 'pointer',
                background: (source === 'tle' && group === g.key)
                  ? 'rgba(0,232,120,0.07)' : 'transparent',
                borderLeft: (source === 'tle' && group === g.key)
                  ? '2px solid var(--green)' : '2px solid transparent',
                textAlign: 'left', display: 'flex', flexDirection: 'column', gap: 2,
              }}
            >
              <span style={{
                color: (source === 'tle' && group === g.key) ? 'var(--green)' : 'var(--text-body)',
                fontSize: 10, fontWeight: 700, letterSpacing: 1,
              }}>
                {g.label}
              </span>
              <span style={{ color: 'var(--text-dim)', fontSize: 8 }}>{g.note}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
