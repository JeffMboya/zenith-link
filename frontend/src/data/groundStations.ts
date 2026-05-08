export interface GroundStationConfig {
  name: string
  lat: number
  lon: number
  color: string
  satId: string // for GS entity click routing
}

export const GROUND_STATIONS: GroundStationConfig[] = [
  { name: 'Nairobi',      lat: -1.2864,   lon:  36.8172,  color: '#00e878', satId: 'gs-nairobi'   },
  { name: 'Svalbard',     lat:  78.2232,  lon:  15.6267,  color: '#00c8f0', satId: 'gs-svalbard'  },
  { name: 'Punta Arenas', lat: -53.1638,  lon: -70.9171,  color: '#c060f0', satId: 'gs-punta'     },
  { name: 'Fairbanks',    lat:  64.8201,  lon: -147.7200, color: '#f0a800', satId: 'gs-fairbanks' },
  { name: 'Bangalore',    lat:  12.9716,  lon:  77.5946,  color: '#00e8c0', satId: 'gs-bangalore' },
  { name: 'Perth',        lat: -31.9505,  lon: 115.8605,  color: '#5878a0', satId: 'gs-perth'     },
]

export const GS_BY_NAME = Object.fromEntries(GROUND_STATIONS.map(gs => [gs.name, gs]))
