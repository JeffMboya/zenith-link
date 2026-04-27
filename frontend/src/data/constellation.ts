// Static orbital element metadata for the 16-satellite constellation.
// Mirrors the element table in spacecraft/api/constellation.go.

export interface SatMeta {
  id: string
  plane: 'A' | 'B' | 'C' | 'D'
  inclinationDeg: number
  raanDeg: number
  smaM: number       // semi-major axis [m]
  altitudeKm: number // nominal altitude [km]
  periodMin: number  // orbital period [min]
  description: string
}

const MU = 3.986004418e14  // Earth gravitational parameter [m³/s²]
const TWO_PI = 2 * Math.PI

function periodMin(smaM: number): number {
  return (TWO_PI * Math.sqrt(smaM ** 3 / MU)) / 60
}

export const CONSTELLATION: SatMeta[] = [
  // ── Plane A: 51.6° — ISS-like ──────────────────────────────────────────
  { id: 'AT-1',  plane: 'A', inclinationDeg: 51.6, raanDeg:   0, smaM: 6_788_000, altitudeKm: 410, periodMin: periodMin(6_788_000), description: 'Primary — full CCSDS telemetry' },
  { id: 'AT-2',  plane: 'A', inclinationDeg: 51.6, raanDeg:   0, smaM: 6_788_000, altitudeKm: 410, periodMin: periodMin(6_788_000), description: 'ISS-family, 90° phase offset' },
  { id: 'AT-3',  plane: 'A', inclinationDeg: 51.6, raanDeg:   0, smaM: 6_788_000, altitudeKm: 410, periodMin: periodMin(6_788_000), description: 'ISS-family, 180° phase offset' },
  { id: 'AT-4',  plane: 'A', inclinationDeg: 51.6, raanDeg:   0, smaM: 6_788_000, altitudeKm: 410, periodMin: periodMin(6_788_000), description: 'ISS-family, 270° phase offset' },
  // ── Plane B: 97.5° — Sun-synchronous ───────────────────────────────────
  { id: 'AT-5',  plane: 'B', inclinationDeg: 97.5, raanDeg:  45, smaM: 6_878_000, altitudeKm: 500, periodMin: periodMin(6_878_000), description: 'Sun-synchronous, dawn-dusk orbit' },
  { id: 'AT-6',  plane: 'B', inclinationDeg: 97.5, raanDeg:  45, smaM: 6_878_000, altitudeKm: 500, periodMin: periodMin(6_878_000), description: 'Sun-synchronous, 90° offset' },
  { id: 'AT-7',  plane: 'B', inclinationDeg: 97.5, raanDeg:  45, smaM: 6_878_000, altitudeKm: 500, periodMin: periodMin(6_878_000), description: 'Sun-synchronous, 180° offset' },
  { id: 'AT-8',  plane: 'B', inclinationDeg: 97.5, raanDeg:  45, smaM: 6_878_000, altitudeKm: 500, periodMin: periodMin(6_878_000), description: 'Sun-synchronous, 270° offset' },
  // ── Plane C: 28.5° — Low inclination ───────────────────────────────────
  { id: 'AT-9',  plane: 'C', inclinationDeg: 28.5, raanDeg:  90, smaM: 6_828_000, altitudeKm: 450, periodMin: periodMin(6_828_000), description: 'Low inclination, equatorial coverage' },
  { id: 'AT-10', plane: 'C', inclinationDeg: 28.5, raanDeg:  90, smaM: 6_828_000, altitudeKm: 450, periodMin: periodMin(6_828_000), description: 'Low inclination, 90° offset' },
  { id: 'AT-11', plane: 'C', inclinationDeg: 28.5, raanDeg:  90, smaM: 6_828_000, altitudeKm: 450, periodMin: periodMin(6_828_000), description: 'Low inclination, 180° offset' },
  { id: 'AT-12', plane: 'C', inclinationDeg: 28.5, raanDeg:  90, smaM: 6_828_000, altitudeKm: 450, periodMin: periodMin(6_828_000), description: 'Low inclination, 270° offset' },
  // ── Plane D: 70.0° — High inclination ──────────────────────────────────
  { id: 'AT-13', plane: 'D', inclinationDeg: 70.0, raanDeg: 135, smaM: 6_858_000, altitudeKm: 480, periodMin: periodMin(6_858_000), description: 'High inclination, polar coverage' },
  { id: 'AT-14', plane: 'D', inclinationDeg: 70.0, raanDeg: 135, smaM: 6_858_000, altitudeKm: 480, periodMin: periodMin(6_858_000), description: 'High inclination, 90° offset' },
  { id: 'AT-15', plane: 'D', inclinationDeg: 70.0, raanDeg: 135, smaM: 6_858_000, altitudeKm: 480, periodMin: periodMin(6_858_000), description: 'High inclination, 180° offset' },
  { id: 'AT-16', plane: 'D', inclinationDeg: 70.0, raanDeg: 135, smaM: 6_858_000, altitudeKm: 480, periodMin: periodMin(6_858_000), description: 'High inclination, 270° offset' },
]

export const SAT_META = Object.fromEntries(CONSTELLATION.map(s => [s.id, s]))

export const PLANE_LABEL: Record<string, string> = {
  A: 'PLANE A · 51.6° ISS-LIKE',
  B: 'PLANE B · 97.5° SSO',
  C: 'PLANE C · 28.5° LOW-INCL',
  D: 'PLANE D · 70.0° HIGH-INCL',
}
