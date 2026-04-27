import { useState } from 'react'
import type { SatelliteState } from '../types'
import { ArcGauge } from './ArcGauge'

interface Props {
  state: SatelliteState | null
}

function Row({ label, value, unit, color = '#b8cfe8' }: { label: string; value: string; unit: string; color?: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', padding: '3px 0', borderBottom: '1px solid #0d2040' }}>
      <span style={{ color: '#3a6898', fontSize: 9, letterSpacing: 1 }}>{label}</span>
      <span style={{ color, fontSize: 12, fontWeight: 700 }}>{value}<span style={{ color: '#3a6898', fontSize: 9, marginLeft: 3 }}>{unit}</span></span>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <div style={{ color: '#00a8d0', fontSize: 8, letterSpacing: 3, marginBottom: 6, paddingBottom: 4, borderBottom: '1px solid #1a4878' }}>
        {title}
      </div>
      {children}
    </div>
  )
}

export function TelemetryPanel({ state }: Props) {
  const [open, setOpen] = useState(true)

  const battColor = state && state.battery_voltage > 7.8 ? '#00e878' : state && state.battery_voltage > 7.0 ? '#f0a800' : '#f03040'
  const rssiColor = state && state.rssi > -90 ? '#00e878' : state && state.rssi > -110 ? '#f0a800' : '#f03040'

  return (
    <div style={{
      position: 'fixed', right: 0, top: 48, bottom: 0, width: open ? 220 : 32,
      background: 'rgba(4,13,28,0.90)', borderLeft: '1px solid #1a4878',
      backdropFilter: 'blur(6px)', zIndex: 90,
      transition: 'width 0.2s ease', overflow: 'hidden',
      display: 'flex', flexDirection: 'column',
    }}>
      {/* Toggle tab */}
      <button
        onClick={() => setOpen(o => !o)}
        style={{
          position: 'absolute', left: -20, top: 60, width: 20, height: 48,
          background: '#071830', border: '1px solid #1a4878', borderRight: 'none',
          color: '#5878a0', cursor: 'pointer', fontSize: 10,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          borderRadius: '4px 0 0 4px',
        }}
      >
        {open ? '›' : '‹'}
      </button>

      {open && (
        <div style={{ padding: '12px 12px', flex: 1, overflowY: 'auto' }}>
          <div style={{ color: '#00c8f0', fontSize: 9, letterSpacing: 3, marginBottom: 12 }}>
            TELEMETRY · GROUND MIRROR
          </div>

          {!state ? (
            <div style={{ color: '#3a6898', fontSize: 10, marginTop: 20, textAlign: 'center' }}>
              AWAITING FIRST FRAME...
            </div>
          ) : (
            <>
              <Section title="ATTITUDE">
                <div style={{ display: 'flex', justifyContent: 'space-between', flexWrap: 'wrap', gap: 4 }}>
                  <ArcGauge value={state.pitch}   min={-30} max={30}   label="PITCH"  unit="°"   color="#00c8f0" size={68} />
                  <ArcGauge value={state.yaw}     min={-180} max={180} label="YAW"    unit="°"   color="#c060f0" size={68} />
                  <ArcGauge value={state.roll}    min={-15} max={15}   label="ROLL"   unit="°"   color="#00e8c0" size={68} />
                </div>
              </Section>

              <Section title="ANGULAR VELOCITY">
                <Row label="ω_X" value={state.omega_x.toFixed(4)} unit="rad/s" />
                <Row label="ω_Y" value={state.omega_y.toFixed(4)} unit="rad/s" />
                <Row label="ω_Z" value={state.omega_z.toFixed(4)} unit="rad/s" />
              </Section>

              <Section title="POWER">
                <div style={{ display: 'flex', justifyContent: 'space-around', marginBottom: 4 }}>
                  <ArcGauge value={state.battery_voltage} min={6.0} max={8.4} label="BATTERY"   unit="V" color={battColor} size={80} />
                  <ArcGauge value={state.solar_voltage}   min={0}  max={35}  label="SOLAR"     unit="V" color={state.solar_voltage > 5 ? '#00e878' : '#f0a800'} size={80} />
                </div>
              </Section>

              <Section title="RF / THERMAL">
                <ArcGauge value={state.rssi} min={-130} max={-60} label="RSSI" unit="dBm" color={rssiColor} size={80} />
                <div style={{ marginTop: 8 }}>
                  <Row label="CHASSIS" value={state.chassis_temp.toFixed(1)} unit="°C" color={state.chassis_temp > 50 ? '#f0a800' : '#b8cfe8'} />
                  <Row label="CPU"     value={state.cpu_temp.toFixed(1)}     unit="°C" color={state.cpu_temp > 70 ? '#f03040' : '#b8cfe8'} />
                </div>
              </Section>

              <Section title="POSITION">
                <Row label="LAT"  value={state.latitude.toFixed(4)}           unit="°" />
                <Row label="LON"  value={state.longitude.toFixed(4)}          unit="°" />
                <Row label="ALT"  value={(state.altitude / 1000).toFixed(1)}  unit="km" color="#00c8f0" />
              </Section>
            </>
          )}
        </div>
      )}
    </div>
  )
}
