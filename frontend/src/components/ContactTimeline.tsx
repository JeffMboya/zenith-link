import { useState, useMemo } from 'react'
import type { ContactWindowsState } from '../hooks/useContactWindows'
import { GROUND_STATIONS } from '../data/groundStations'

const WINDOW_H = 6  // hours to show

interface Props {
  windowsState: ContactWindowsState
}

function formatCountdown(aosMs: number): string {
  const diff = Math.round((aosMs - Date.now()) / 1000)
  if (diff <= 0) return 'NOW'
  const m = Math.floor(diff / 60), s = diff % 60
  return m > 0 ? `${m}m ${String(s).padStart(2,'0')}s` : `${s}s`
}

export function ContactTimeline({ windowsState }: Props) {
  const [expanded, setExpanded] = useState(false)

  const nowMs   = Date.now()
  const endMs   = nowMs + WINDOW_H * 3600_000
  const spanMs  = endMs - nowMs

  // Build GS summary dots (● = has window, ○ = no window in 6h)
  const gsDots = GROUND_STATIONS.map(gs => {
    const entries = windowsState.byGS[gs.name] ?? []
    const hasWindow = entries.some(e =>
      e.inContact || e.windows.some(w => new Date(w.aos).getTime() < endMs)
    )
    return { gs, hasWindow }
  })

  // Find next AOS for header
  const nextAOS = useMemo(() => {
    const d = windowsState.delivery
    if (!d) return null
    if (d.flowing) return { label: 'IN PASS', sat: d.relay, gs: d.gs, urgent: false }
    if (d.aosMs === null) return null
    const diff = d.aosMs - nowMs
    if (diff < 0) return { label: 'IN PASS', sat: d.relay, gs: d.gs, urgent: false }
    return { label: formatCountdown(d.aosMs), sat: d.relay, gs: d.gs, urgent: diff < 300_000 }
  }, [windowsState.delivery, nowMs])

  return (
    <div style={{
      position: 'fixed',
      bottom: 28,  // sits above MissionFooter
      left: 0, right: 0,
      zIndex: 115,
      background: 'rgba(7,24,48,0.95)',
      borderTop: '1px solid var(--border)',
      backdropFilter: 'blur(8px)',
    }}>
      {/* Collapsed header — always visible */}
      <button
        onClick={() => setExpanded(e => !e)}
        aria-expanded={expanded}
        aria-label={expanded ? 'Collapse contact window timeline' : 'Expand contact window timeline'}
        style={{
          width: '100%', height: 20,
          background: 'none', border: 'none', cursor: 'pointer',
          display: 'flex', alignItems: 'center', gap: 10,
          padding: '0 16px',
          color: 'var(--text-dim)',
          fontFamily: 'var(--font-mono)',
        }}
      >
        <span style={{ fontSize: 8, letterSpacing: 2, color: 'var(--text-dim)' }}>
          CONTACT WINDOWS {expanded ? '▾' : '▸'}
        </span>

        {/* GS health dots */}
        <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
          {gsDots.map(({ gs, hasWindow }) => (
            <div
              key={gs.name}
              title={`${gs.name}: ${hasWindow ? 'windows in 6h' : 'no windows'}`}
              style={{
                width: 5, height: 5, borderRadius: '50%',
                background: hasWindow ? gs.color : 'var(--bg-dark)',
                opacity: hasWindow ? 0.8 : 0.4,
              }}
            />
          ))}
        </div>

        {nextAOS && (
          <span style={{
            fontSize: 8, letterSpacing: 0.5,
            color: nextAOS.urgent ? 'var(--amber)' : nextAOS.label === 'IN PASS' ? 'var(--green)' : 'var(--cyan)',
            marginLeft: 'auto',
          }}>
            {nextAOS.label !== 'IN PASS' ? `ISS next: ${nextAOS.label}` : `IN PASS · ${nextAOS.gs}`}
          </span>
        )}
      </button>

      {/* Expanded bar chart */}
      {expanded && (
        <div style={{ padding: '6px 16px 8px' }}>
          {/* Time axis */}
          <div style={{ display: 'flex', marginBottom: 4, paddingLeft: 76 }}>
            <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>NOW</span>
            <div style={{ flex: 1 }} />
            <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1 }}>+{WINDOW_H}h</span>
          </div>

          {GROUND_STATIONS.map(gs => {
            const entries = windowsState.byGS[gs.name] ?? []
            // Collect all windows across all relays for this GS
            const allWindows = entries.flatMap(e => e.windows)
            const inContact  = entries.some(e => e.inContact)
            const hasAny     = inContact || allWindows.length > 0

            return (
              <div
                key={gs.name}
                style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 3 }}
                title={`${gs.name}: ${hasAny ? `${allWindows.length} window(s)` : 'no contact in 6h'}`}
              >
                {/* GS label */}
                <span style={{
                  width: 72, fontSize: 8, letterSpacing: 0.3, textAlign: 'right',
                  color: hasAny ? 'var(--text-body)' : 'var(--text-dim)',
                  opacity: hasAny ? 1 : 0.5,
                  flexShrink: 0,
                  whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
                }}>
                  {gs.name}
                </span>

                {/* Timeline bar area */}
                <div style={{ flex: 1, height: 8, position: 'relative', background: 'var(--bg-dark)', borderRadius: 1 }}>
                  {/* AOS windows */}
                  {inContact && (
                    <div style={{
                      position: 'absolute', left: 0, top: 0, height: '100%',
                      width: '5%',
                      background: gs.color, opacity: 0.8, borderRadius: 1,
                    }} />
                  )}
                  {allWindows.map((w, i) => {
                    const wAos = new Date(w.aos).getTime()
                    const wLos = new Date(w.los).getTime()
                    if (wLos < nowMs || wAos > endMs) return null
                    const left  = Math.max(0, ((wAos - nowMs) / spanMs) * 100)
                    const right = Math.min(100, ((wLos - nowMs) / spanMs) * 100)
                    const width = right - left
                    if (width <= 0) return null
                    return (
                      <div
                        key={i}
                        title={`AOS: ${new Date(w.aos).toLocaleTimeString('en-US', { hour12: false })} · ${w.duration_sec}s · max ${w.max_elevation_deg.toFixed(0)}°`}
                        style={{
                          position: 'absolute',
                          left:   `${left}%`,
                          width:  `${width}%`,
                          top: 0, height: '100%',
                          background: gs.color,
                          opacity: 0.75,
                          borderRadius: 2,
                        }}
                      />
                    )
                  })}

                  {/* "Now" marker */}
                  <div style={{
                    position: 'absolute', left: 0, top: -2, bottom: -2,
                    width: 1,
                    background: 'rgba(255,255,255,0.35)',
                  }} />
                </div>
              </div>
            )
          })}

          <div style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1, marginTop: 4, textAlign: 'right' }}>
            Hover bars for window details
          </div>
        </div>
      )}
    </div>
  )
}
