export interface SatMeta {
  id: string
  inclinationDeg: number
  raanDeg: number
  smaM: number
  altitudeKm: number
  periodMin: number
  description: string
}

const MU = 3.986004418e14
const TWO_PI = 2 * Math.PI

function periodMin(smaM: number): number {
  return (TWO_PI * Math.sqrt(smaM ** 3 / MU)) / 60
}

export const CONSTELLATION: SatMeta[] = [
  { id: 'Satellite-1', inclinationDeg: 51.6, raanDeg: 0, smaM: 6_788_000, altitudeKm: 410, periodMin: periodMin(6_788_000), description: 'Primary — full CCSDS telemetry' },
]

export const SAT_META = Object.fromEntries(CONSTELLATION.map(s => [s.id, s]))
