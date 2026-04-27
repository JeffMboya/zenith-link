import type { LinkMetrics } from '../types'

const LINK_COST_PER_MB = 15

interface Props {
  metrics: LinkMetrics | null
  connected: boolean
}

interface KPI {
  label: string
  value: string
  color: string
}

function kpis(m: LinkMetrics): KPI[] {
  const zenithMB = m.bytes_received_zenith / 1e6
  const jsonMB = m.bytes_equivalent_json / 1e6
  const savedMB = Math.max(0, jsonMB - zenithMB)
  const eff = jsonMB > 0 ? ((savedMB / jsonMB) * 100).toFixed(1) : '0.0'
  const effNum = parseFloat(eff)

  return [
    { label: 'PACKETS ACK', value: m.packets_received.toLocaleString(), color: '#00c8f0' },
    { label: 'NACKs', value: m.nacks_issued.toLocaleString(), color: m.nacks_issued > 0 ? '#f0a800' : '#00e878' },
    { label: 'DROPPED', value: m.packets_dropped_simulated.toLocaleString(), color: '#f0a800' },
    { label: 'EFFICIENCY', value: `${eff}%`, color: effNum >= 80 ? '#00e878' : effNum >= 60 ? '#f0a800' : '#f03040' },
    { label: 'BYTES SAVED', value: `${(savedMB * 1000).toFixed(1)} KB`, color: '#00e8c0' },
    { label: 'COST SAVED', value: `$${(savedMB * LINK_COST_PER_MB).toFixed(4)}`, color: '#c060f0' },
  ]
}

export function KPIBar({ metrics, connected }: Props) {
  return (
    <div style={{
      position: 'fixed', top: 0, left: 0, right: 0, zIndex: 100,
      display: 'flex', alignItems: 'center', gap: 1,
      background: 'rgba(4,13,28,0.88)', borderBottom: '1px solid #1a4878',
      backdropFilter: 'blur(6px)',
    }}>
      {/* Logo / status */}
      <div style={{
        padding: '8px 20px', borderRight: '1px solid #1a4878',
        display: 'flex', flexDirection: 'column', minWidth: 180,
      }}>
        <span style={{ color: '#00c8f0', fontSize: 11, letterSpacing: 3, fontWeight: 700 }}>
          ZENITH-LINK
        </span>
        <span style={{ color: '#3a6898', fontSize: 9, letterSpacing: 2, marginTop: 2 }}>
          MISSION CONTROL
        </span>
      </div>

      {/* KPI tiles */}
      <div style={{ flex: 1, display: 'flex' }}>
        {metrics
          ? kpis(metrics).map(({ label, value, color }) => (
              <div key={label} style={{
                flex: 1, padding: '6px 12px', borderRight: '1px solid #0d2040',
                display: 'flex', flexDirection: 'column', alignItems: 'center',
              }}>
                <span style={{ color, fontSize: 16, fontWeight: 700, lineHeight: 1.2 }}>{value}</span>
                <span style={{ color: '#3a6898', fontSize: 8, letterSpacing: 2, marginTop: 2 }}>{label}</span>
              </div>
            ))
          : <div style={{ padding: '12px 20px', color: '#3a6898', fontSize: 10, letterSpacing: 2 }}>
              AWAITING LINK ACQUISITION...
            </div>
        }
      </div>

      {/* Link status */}
      <div style={{
        padding: '6px 16px', borderLeft: '1px solid #1a4878',
        display: 'flex', alignItems: 'center', gap: 6,
      }}>
        <div style={{
          width: 6, height: 6, borderRadius: '50%',
          background: connected ? '#00e878' : '#f03040',
          boxShadow: connected ? '0 0 6px #00e878' : '0 0 6px #f03040',
        }} />
        <span style={{ color: connected ? '#00e878' : '#f03040', fontSize: 9, letterSpacing: 2 }}>
          {connected ? 'LIVE' : 'OFFLINE'}
        </span>
      </div>
    </div>
  )
}
