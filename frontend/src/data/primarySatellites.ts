// Canonical primary satellite config keyed by NORAD ID.
// Use NORAD ID as the stable identifier — TLE display names drift over time.
export interface PrimarySat {
  noradId: number
  displayName: string
  tleName: string    // expected CelesTrak name (may vary — always prefer noradId for matching)
  altKm: number
  incDeg: number
  scTarget: 'sc1' | 'sc2' | 'sc3' // backend command routing target
  wsPath: string                    // WebSocket path via nginx
}

export const PRIMARY_SATELLITES: PrimarySat[] = [
  {
    noradId: 25544,
    displayName: 'ISS',
    tleName: 'ISS (ZARYA)',
    altKm: 408,
    incDeg: 51.6,
    scTarget: 'sc1',
    wsPath: '/ws',
  },
  {
    noradId: 48274,
    displayName: 'Tiangong',
    tleName: 'CSS (TIANHE)',
    altKm: 380,
    incDeg: 41.5,
    scTarget: 'sc2',
    wsPath: '/spacecraft2/ws',
  },
  {
    noradId: 20580,
    displayName: 'Hubble',
    tleName: 'HST',
    altKm: 540,
    incDeg: 28.5,
    scTarget: 'sc3',
    wsPath: '/spacecraft3/ws',
  },
]

export const PRIMARY_NORAD_IDS = new Set(PRIMARY_SATELLITES.map(s => s.noradId))
export const PRIMARY_BY_NORAD = Object.fromEntries(PRIMARY_SATELLITES.map(s => [s.noradId, s]))

// Map TLE display name → PrimarySat (normalised for common CelesTrak name variants)
const tleNameMap = new Map<string, PrimarySat>()
for (const sat of PRIMARY_SATELLITES) {
  tleNameMap.set(sat.tleName.toUpperCase(), sat)
  tleNameMap.set(sat.displayName.toUpperCase(), sat)
}
// Common name variants
tleNameMap.set('ISS', PRIMARY_SATELLITES[0])
tleNameMap.set('ZARYA', PRIMARY_SATELLITES[0])
tleNameMap.set('CSS', PRIMARY_SATELLITES[1])
tleNameMap.set('TIANGONG', PRIMARY_SATELLITES[1])
tleNameMap.set('HST', PRIMARY_SATELLITES[2])
tleNameMap.set('HUBBLE SPACE TELESCOPE', PRIMARY_SATELLITES[2])

export function primarySatByName(name: string): PrimarySat | undefined {
  return tleNameMap.get(name.toUpperCase())
}

export function isPrimaryById(satId: string): boolean {
  // satId may be TLE display name or NORAD-based
  return tleNameMap.has(satId.toUpperCase())
}
