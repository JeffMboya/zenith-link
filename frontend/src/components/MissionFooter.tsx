import { useState, useEffect } from 'react'

interface Props {
  connected:     boolean
  selectedSatId: string
  missionStartMs: number
}

function pad(n: number) { return String(n).padStart(2, '0') }

function utcString() {
  const d = new Date()
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`
}

function metString(startMs: number) {
  const elapsed = Math.floor((Date.now() - startMs) / 1000)
  const h = Math.floor(elapsed / 3600)
  const m = Math.floor((elapsed % 3600) / 60)
  const s = elapsed % 60
  return `${pad(h)}:${pad(m)}:${pad(s)}`
}

function localString() {
  const d = new Date()
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function useClock() {
  const [tick, setTick] = useState(0)
  useEffect(() => {
    const id = setInterval(() => setTick(t => t + 1), 1000)
    return () => clearInterval(id)
  }, [])
  return tick
}

// AOS countdown sourced from KPIBar's relay windows — read from shared state
// For Phase 2 we expose a custom event 'delivery-state' that KPIBar fires.
interface DeliverySnapshot { aosMs: number | null; relay: string; gs: string; flowing: boolean }

function useDeliverySnapshot(): DeliverySnapshot | null {
  const [snap, setSnap] = useState<DeliverySnapshot | null>(null)
  useEffect(() => {
    const h = (e: Event) => setSnap((e as CustomEvent<DeliverySnapshot>).detail)
    window.addEventListener('delivery-state', h)
    return () => window.removeEventListener('delivery-state', h)
  }, [])
  return snap
}

// Alert counts from the events feed
function useAlertCounts() {
  const [counts, setCounts] = useState({ error: 0, warning: 0, info: 0 })
  useEffect(() => {
    const h = (e: Event) => setCounts((e as CustomEvent<{ error: number; warning: number; info: number }>).detail)
    window.addEventListener('alert-counts', h)
    return () => window.removeEventListener('alert-counts', h)
  }, [])
  return counts
}

// Heartbeat blink — toggles at 1Hz
function useHeartbeat() {
  const [lit, setLit] = useState(true)
  useEffect(() => {
    const id = setInterval(() => setLit(l => !l), 500)
    return () => clearInterval(id)
  }, [])
  return lit
}

const DIVIDER = (
  <div style={{ width: 1, height: 16, background: 'var(--bg-dark)', flexShrink: 0, margin: '0 8px' }} />
)

export function MissionFooter({ connected, selectedSatId, missionStartMs }: Props) {
  useClock()
  const snap    = useDeliverySnapshot()
  const alerts  = useAlertCounts()
  const hbLit   = useHeartbeat()

  // AOS countdown label
  let aosLabel = '—'
  let aosColor = 'var(--text-dim)'
  if (snap) {
    if (snap.flowing) {
      aosLabel = 'IN PASS'
      aosColor = 'var(--green)'
    } else if (snap.aosMs !== null) {
      const diff = Math.round((snap.aosMs - Date.now()) / 1000)
      if (diff > 0) {
        const m = Math.floor(diff / 60), s = diff % 60
        aosLabel = m > 0 ? `AOS ${m}m ${pad(s)}s` : `AOS ${s}s`
        aosColor = diff < 300 ? 'var(--amber)' : 'var(--cyan)'
      } else {
        aosLabel = 'IN PASS'
        aosColor = 'var(--green)'
      }
    }
  }

  const satLabel = selectedSatId
    ? selectedSatId.split(' ')[0].slice(0, 12)
    : '—'

  return (
    <div
      aria-label="Mission health and time standards"
      style={{
        position: 'fixed',
        bottom: 0, left: 0, right: 0,
        zIndex: 116,
        height: 28,
        background: 'rgba(4,13,28,0.96)',
        borderTop: '1px solid var(--border)',
        display: 'flex',
        alignItems: 'center',
        padding: '0 14px',
        gap: 0,
        backdropFilter: 'blur(8px)',
      }}
    >
      {/* ── LEFT: Time Standards ── */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 0, flexShrink: 0 }}>
        {/* UTC */}
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', padding: '0 10px' }}>
          <span
            aria-label={`UTC time ${utcString()}`}
            style={{
              color: 'var(--green)',
              fontSize: 13,
              fontWeight: 700,
              letterSpacing: 0.5,
              fontVariantNumeric: 'tabular-nums',
              lineHeight: 1,
            }}
          >
            {utcString()}
          </span>
          <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2, marginTop: 1 }}>UTC</span>
        </div>

        {DIVIDER}

        {/* MET */}
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', padding: '0 10px' }}>
          <span
            aria-label={`Mission elapsed time ${metString(missionStartMs)}`}
            style={{
              color: 'var(--cyan)',
              fontSize: 11,
              fontWeight: 700,
              letterSpacing: 0.5,
              fontVariantNumeric: 'tabular-nums',
              lineHeight: 1,
            }}
          >
            {metString(missionStartMs)}
          </span>
          <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2, marginTop: 1 }}>MET</span>
        </div>

        {DIVIDER}

        {/* Local */}
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', padding: '0 8px' }}>
          <span
            style={{
              color: 'var(--text-mid)',
              fontSize: 10,
              letterSpacing: 0.3,
              fontVariantNumeric: 'tabular-nums',
              lineHeight: 1,
            }}
          >
            {localString()}
          </span>
          <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2, marginTop: 1 }}>LOCAL</span>
        </div>
      </div>

      {DIVIDER}

      {/* ── CENTER: Network Health ── */}
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 0 }}>
        {/* AOS countdown */}
        <div
          style={{
            padding: '0 12px',
            borderLeft: snap && !snap.flowing && snap.aosMs !== null && (snap.aosMs - Date.now()) < 300_000
              ? '2px solid var(--amber)' : '2px solid transparent',
          }}
        >
          <span
            aria-label={`Next acquisition of signal: ${aosLabel}`}
            style={{ color: aosColor, fontSize: 10, fontWeight: 700, letterSpacing: 0.5, fontVariantNumeric: 'tabular-nums' }}
          >
            {aosLabel}
          </span>
          {snap && (snap.relay || snap.gs) && (
            <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1, marginLeft: 5 }}>
              {snap.relay} → {snap.gs}
            </span>
          )}
        </div>
      </div>

      {DIVIDER}

      {/* ── RIGHT: System Context ── */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 0, flexShrink: 0 }}>
        {/* Target */}
        <div style={{ padding: '0 10px', display: 'flex', flexDirection: 'column', alignItems: 'flex-start' }}>
          <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 2 }}>TARGET</span>
          <span
            aria-label={`Active target: ${satLabel}`}
            style={{ color: selectedSatId ? 'var(--cyan)' : 'var(--text-dim)', fontSize: 10, fontWeight: 700, letterSpacing: 0.5, lineHeight: 1 }}
          >
            {satLabel}
          </span>
        </div>

        {DIVIDER}

        {/* Alert badges */}
        <div
          style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '0 10px', cursor: 'pointer' }}
          onClick={() => window.dispatchEvent(new CustomEvent('switch-tab', { detail: 'EVENTS' }))}
          title="Open events log"
          aria-label={`Alerts: ${alerts.error} errors, ${alerts.warning} warnings, ${alerts.info} info`}
          role="button"
          tabIndex={0}
          onKeyDown={e => e.key === 'Enter' && window.dispatchEvent(new CustomEvent('switch-tab', { detail: 'EVENTS' }))}
        >
          {alerts.error > 0 && (
            <span style={{ color: 'var(--red)', fontSize: 9, fontWeight: 700 }}>
              ✕{alerts.error}
            </span>
          )}
          {alerts.warning > 0 && (
            <span style={{ color: 'var(--amber)', fontSize: 9, fontWeight: 700 }}>
              ⚠{alerts.warning}
            </span>
          )}
          {alerts.info > 0 && (
            <span style={{ color: 'var(--cyan)', fontSize: 9 }}>
              ℹ{alerts.info}
            </span>
          )}
          {alerts.error === 0 && alerts.warning === 0 && alerts.info === 0 && (
            <span style={{ color: 'var(--text-dim)', fontSize: 9 }}>—</span>
          )}
        </div>

        {DIVIDER}

        {/* GCS heartbeat dot */}
        <div
          style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', padding: '0 8px' }}
          aria-label={`Ground control station link: ${connected ? 'connected' : 'offline'}`}
        >
          <div style={{
            width: 7,
            height: 7,
            borderRadius: '50%',
            background: connected
              ? (hbLit ? 'var(--green)' : 'rgba(0,232,120,0.25)')
              : 'var(--red)',
            boxShadow: connected && hbLit ? '0 0 6px var(--green)' : 'none',
            transition: 'background 0.1s, box-shadow 0.1s',
          }} />
          <span style={{ color: 'var(--text-dim)', fontSize: 7, letterSpacing: 1.5, marginTop: 1 }}>GCS</span>
        </div>
      </div>
    </div>
  )
}
