export interface SatelliteState {
  pitch: number
  yaw: number
  roll: number
  omega_x: number
  omega_y: number
  omega_z: number
  latitude: number
  longitude: number
  altitude: number
  solar_voltage: number
  battery_voltage: number
  chassis_temp: number
  cpu_temp: number
  rssi: number
  inference_class?: number
  inference_label?: string
  inference_conf?: number
  flags?: number
}

export interface LinkMetrics {
  packets_received: number
  packets_dropped_simulated: number
  nacks_issued: number
  acks_issued: number
  full_syncs_received: number
  bytes_received_satlyt: number
  bytes_equivalent_json: number
}

export interface TrackPoint {
  lat: number
  lon: number
  alt: number
}

export interface GroundStation {
  name: string
  lat: number
  lon: number
  inView: boolean
}

export interface StateUpdate {
  type: 'state_update'
  ts: number
  satellite: SatelliteState
  metrics: LinkMetrics
  ground_track: TrackPoint[]
  ground_stations: GroundStation[]
}

export interface CommandResult {
  status: 'queued' | 'error'
  seq: number
  command: string
}

export interface AutonomousEvent {
  at: string
  class: string
  action: string
}

export interface DeployedPayload {
  Name: string
  SizeBytes: number
  Status: string
  DeployedAt: string
  ExecOutput: string
}
