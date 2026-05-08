import { useEffect, useRef } from 'react'
import {
  Ion,
  Cartesian3,
  Cartesian2,
  Color,
  LabelStyle,
  TileMapServiceImageryProvider,
  buildModuleUrl,
  ScreenSpaceEventHandler,
  ScreenSpaceEventType,
  defined,
  NearFarScalar,
  IonImageryProvider,
  Math as CesiumMath,
} from 'cesium'
import type { Viewer as CesiumViewer } from 'cesium'
import { Viewer, Entity, PointGraphics, LabelGraphics, PolylineGraphics, EllipseGraphics } from 'resium'
import type { StateUpdate } from '../types'
import type { OrbitalPos } from '../hooks/useOrbitalPosition'
import type { ConstellationSat } from '../hooks/useConstellation'
import type { GroundStationConfig } from '../data/groundStations'
import { useForwardTrack } from '../hooks/useForwardTrack'

Ion.defaultAccessToken = (import.meta as unknown as { env: Record<string, string> }).env.VITE_CESIUM_TOKEN ?? ''

const PLANE_COLOR: Record<string, Color> = {
  A:       Color.CYAN.withAlpha(0.9),
  B:       Color.fromCssColorString('#00e878').withAlpha(0.9),
  C:       Color.fromCssColorString('#f0a800').withAlpha(0.9),
  D:       Color.fromCssColorString('#c060f0').withAlpha(0.9),
  ISL:     Color.fromCssColorString('#f06040').withAlpha(0.9),
  stations: Color.WHITE.withAlpha(0.95),
  starlink: Color.fromCssColorString('#00c8f0').withAlpha(0.8),
  planet:   Color.fromCssColorString('#00e8c0').withAlpha(0.8),
  active:   Color.fromCssColorString('#f0a800').withAlpha(0.7),
}

export interface RelayHealth {
  online:        boolean
  bufferHasData: boolean
  lastFetchAt:   string | null
  upstreamNode?: string
}

function islBeamColor(health: RelayHealth | null | undefined): Color {
  if (!health || !health.online) return Color.WHITE.withAlpha(0.08)
  if (health.bufferHasData) return Color.fromCssColorString('#00e878').withAlpha(0.5)
  return Color.fromCssColorString('#f0a800').withAlpha(0.3)
}

/** Compute elevation angle (degrees) from ground station to satellite. */
function elevationDeg(
  gsLat: number, gsLon: number,
  satLat: number, satLon: number, satAltM: number,
): number {
  const R = 6_371_000
  const gsR = R
  const satR = R + satAltM

  const glat = CesiumMath.toRadians(gsLat)
  const glon = CesiumMath.toRadians(gsLon)
  const slat = CesiumMath.toRadians(satLat)
  const slon = CesiumMath.toRadians(satLon)

  // Central angle
  const cosC = Math.sin(glat) * Math.sin(slat) + Math.cos(glat) * Math.cos(slat) * Math.cos(slon - glon)
  const c = Math.acos(Math.max(-1, Math.min(1, cosC)))

  // Elevation
  const elev = Math.atan2(satR * Math.cos(c) - gsR, satR * Math.sin(c))
  return CesiumMath.toDegrees(elev)
}

function elevationRingColor(elevDeg: number): Color {
  if (elevDeg > 30) return Color.fromCssColorString('#00e878').withAlpha(0.35)   // green
  if (elevDeg > 10) return Color.fromCssColorString('#f0a800').withAlpha(0.3)    // amber
  return Color.fromCssColorString('#5878a0').withAlpha(0.2)                       // gray
}

interface Props {
  data:          StateUpdate | null
  orbitalPos:    OrbitalPos | null
  constellation: ConstellationSat[]
  groundStations: GroundStationConfig[]
  selectedSatId: string
  onSelectSat:   (id: string) => void
  relay1Health?: RelayHealth | null
  // relay2Health reserved for future beam coloring
  tleSource:     'sim' | 'tle'
  primarySatId:  string        // first primary (ISS) — kept for backwards compat
  primarySatIds: string[]      // all primary TLE names
}

export function Globe3D({
  data, orbitalPos, constellation, groundStations,
  selectedSatId, onSelectSat,
  relay1Health,
  tleSource, primarySatId, primarySatIds,
}: Props) {
  const viewerRef     = useRef<{ cesiumElement: CesiumViewer } | null>(null)
  const flew          = useRef(false)
  const imageryInit   = useRef(false)
  const clickHandlerRef = useRef<ScreenSpaceEventHandler | null>(null)
  const allEntityIds  = useRef<Set<string>>(new Set())
  const forwardTrack  = useForwardTrack()

  // Track all entity IDs for click detection
  useEffect(() => {
    const ids = new Set(constellation.map(s => s.id))
    primarySatIds.forEach(id => ids.add(id))
    groundStations.forEach(gs => ids.add(gs.satId))
    allEntityIds.current = ids
  }, [constellation, primarySatIds, groundStations])

  // Imagery + initial camera
  useEffect(() => {
    if (imageryInit.current) return
    const viewer = viewerRef.current?.cesiumElement
    if (!viewer) return
    imageryInit.current = true

    const cesiumToken = (import.meta as unknown as { env: Record<string, string> }).env.VITE_CESIUM_TOKEN
    if (cesiumToken) {
      // Use Cesium ion world imagery for terminator line support
      viewer.imageryLayers.removeAll()
      IonImageryProvider.fromAssetId(3).then(p => {
        viewer.imageryLayers.addImageryProvider(p)
        // Enable sun lighting for day/night terminator
        viewer.scene.globe.enableLighting = true
      }).catch(() => {
        // Fall back to bundled NaturalEarth if ion asset unavailable
        const layers = viewer.imageryLayers
        layers.removeAll()
        TileMapServiceImageryProvider.fromUrl(buildModuleUrl('Assets/Textures/NaturalEarthII'))
          .then(p => layers.addImageryProvider(p)).catch(() => {})
      })
    } else {
      const layers = viewer.imageryLayers
      layers.removeAll()
      TileMapServiceImageryProvider.fromUrl(buildModuleUrl('Assets/Textures/NaturalEarthII'))
        .then(p => layers.addImageryProvider(p)).catch(() => {})
    }

    viewer.camera.setView({ destination: Cartesian3.fromDegrees(0, 20, 22_000_000) })
  })

  // Click handler — satellites AND ground stations
  useEffect(() => {
    if (clickHandlerRef.current) {
      clickHandlerRef.current.destroy()
      clickHandlerRef.current = null
    }
    const viewer = viewerRef.current?.cesiumElement
    if (!viewer) return

    const handler = new ScreenSpaceEventHandler(viewer.scene.canvas)
    handler.setInputAction((e: { position: Cartesian2 }) => {
      const picked = viewer.scene.pick(e.position)
      if (!defined(picked) || !picked.id) return

      const name: string | undefined = picked.id.name
      if (!name) return

      // Ground station click → flyTo GS position
      const gs = groundStations.find(g => g.satId === name)
      if (gs) {
        viewer.camera.flyTo({
          destination: Cartesian3.fromDegrees(gs.lon, gs.lat, 8_000_000),
          duration: 1.5,
        })
        window.dispatchEvent(new CustomEvent('select-sat', { detail: gs.satId }))
        onSelectSat(gs.satId)
        return
      }

      // Satellite click → flyTo satellite + select
      if (allEntityIds.current.has(name)) {
        const sat = constellation.find(s => s.id === name)
        if (sat) {
          viewer.camera.flyTo({
            destination: Cartesian3.fromDegrees(sat.lon, sat.lat, 12_000_000),
            duration: 1.5,
          })
        } else {
          // Primary not in constellation array — use current position
          viewer.camera.flyTo({
            destination: Cartesian3.fromDegrees(satLon, satLat, 12_000_000),
            duration: 1.5,
          })
        }
        onSelectSat(name)
      }
    }, ScreenSpaceEventType.LEFT_CLICK)

    clickHandlerRef.current = handler
    return () => {
      handler.destroy()
      clickHandlerRef.current = null
    }
  }, [onSelectSat, constellation, groundStations])

  // Primary satellite position (ISS / first primary)
  const primaryConstellationSat = tleSource === 'tle'
    ? constellation.find(s => s.id === primarySatId)
    : null

  const firstPos = primaryConstellationSat
    ? { lat: primaryConstellationSat.lat, lon: primaryConstellationSat.lon, alt: primaryConstellationSat.alt_m }
    : orbitalPos ?? (data?.satellite
        ? { lat: data.satellite.latitude, lon: data.satellite.longitude, alt: data.satellite.altitude }
        : null)

  useEffect(() => {
    if (!firstPos || flew.current) return
    const viewer = viewerRef.current?.cesiumElement
    if (!viewer) return
    flew.current = true
    viewer.camera.flyTo({
      destination: Cartesian3.fromDegrees(firstPos.lon, firstPos.lat, 20_000_000),
      duration: 2,
    })
  }, [firstPos])

  const satLat = primaryConstellationSat?.lat ?? orbitalPos?.lat ?? data?.satellite.latitude ?? 0
  const satLon = primaryConstellationSat?.lon ?? orbitalPos?.lon ?? data?.satellite.longitude ?? 0
  const satAlt = primaryConstellationSat?.alt_m ?? orbitalPos?.alt ?? data?.satellite.altitude ?? 550_000
  const satPos = Cartesian3.fromDegrees(satLon, satLat, satAlt)
  const primaryLabel = primarySatId.split(' ')[0].slice(0, 10)

  const pastTrack = (data?.ground_track ?? []).map(p => Cartesian3.fromDegrees(p.lon, p.lat, p.alt))
  const futureTrack = forwardTrack.map(p => Cartesian3.fromDegrees(p.lon, p.lat, p.alt_m))

  // All non-ISS primary positions for ISL beams
  const otherPrimaries = primarySatIds
    .filter(id => id !== primarySatId)
    .map(id => constellation.find(s => s.id === id))
    .filter((s): s is ConstellationSat => !!s)

  return (
    <Viewer
      ref={viewerRef as never}
      full
      baseLayerPicker={false}
      navigationHelpButton={false}
      homeButton={false}
      geocoder={false}
      timeline={false}
      animation={false}
      sceneModePicker={false}
      selectionIndicator={false}
      infoBox={false}
      style={{ position: 'fixed', top: 0, left: 0, width: '95%', height: '95%' }}
    >
      {/* Forward ground track */}
      {futureTrack.length >= 2 && (
        <Entity name="forward-track">
          <PolylineGraphics positions={futureTrack} width={1.2} material={Color.WHITE.withAlpha(0.25)} />
        </Entity>
      )}

      {/* Past ground track */}
      {pastTrack.length >= 2 && (
        <Entity name="past-track">
          <PolylineGraphics positions={pastTrack} width={1.8} material={Color.WHITE.withAlpha(0.6)} />
        </Entity>
      )}

      {/* ISL beams: primary ↔ all other primaries (triangle when 3 are up) */}
      {otherPrimaries.map(sat => (
        <Entity key={`isl-${sat.id}`} name={`isl-${primarySatId}-${sat.id}`}>
          <PolylineGraphics
            positions={[satPos, Cartesian3.fromDegrees(sat.lon, sat.lat, sat.alt_m)]}
            width={1.5}
            material={islBeamColor(relay1Health)}
          />
        </Entity>
      ))}
      {/* Cross-beams between non-ISS primaries */}
      {otherPrimaries.length >= 2 && (
        <Entity name={`isl-${otherPrimaries[0].id}-${otherPrimaries[1].id}`}>
          <PolylineGraphics
            positions={[
              Cartesian3.fromDegrees(otherPrimaries[0].lon, otherPrimaries[0].lat, otherPrimaries[0].alt_m),
              Cartesian3.fromDegrees(otherPrimaries[1].lon, otherPrimaries[1].lat, otherPrimaries[1].alt_m),
            ]}
            width={1}
            material={Color.fromCssColorString('#f06040').withAlpha(0.2)}
          />
        </Entity>
      )}

      {/* Primary satellite (ISS / first primary) */}
      <Entity position={satPos} name={primarySatId}
        description={`Primary spacecraft: ${primarySatId}`}
      >
        <PointGraphics
          color={Color.CYAN}
          pixelSize={selectedSatId === primarySatId ? 12 : 10}
          outlineColor={selectedSatId === primarySatId ? Color.WHITE.withAlpha(0.9) : Color.fromCssColorString('#004060')}
          outlineWidth={selectedSatId === primarySatId ? 2.5 : 2}
        />
        <LabelGraphics
          text={primaryLabel}
          fillColor={Color.CYAN}
          font="12px 'IBM Plex Mono', 'Courier New', monospace"
          pixelOffset={new Cartesian2(14, 0)}
          style={LabelStyle.FILL}
          showBackground={false}
          disableDepthTestDistance={Number.POSITIVE_INFINITY}
          scaleByDistance={new NearFarScalar(1e6, 1.0, 8e6, 0.8)}
        />
      </Entity>

      {/* All ground stations (all 6) with elevation rings */}
      {groundStations.map(gs => {
        const elev = elevationDeg(gs.lat, gs.lon, satLat, satLon, satAlt)
        const ringColor = elevationRingColor(elev)
        const isSelected = selectedSatId === gs.satId
        const gsColor = Color.fromCssColorString(gs.color).withAlpha(isSelected ? 1.0 : 0.5)

        return (
          <Entity
            key={gs.name}
            position={Cartesian3.fromDegrees(gs.lon, gs.lat, 0)}
            name={gs.satId}
            description={`Ground Station: ${gs.name}, Elevation: ${elev.toFixed(1)}°`}
          >
            <PointGraphics
              color={gsColor}
              pixelSize={isSelected ? 9 : 7}
              outlineColor={Color.fromCssColorString('#071830')}
              outlineWidth={1}
            />
            <LabelGraphics
              text={gs.name.toUpperCase()}
              fillColor={gsColor}
              font="10px 'IBM Plex Mono', 'Courier New', monospace"
              pixelOffset={new Cartesian2(12, 0)}
              style={LabelStyle.FILL}
              disableDepthTestDistance={Number.POSITIVE_INFINITY}
            />
            {/* Elevation quality ring */}
            <EllipseGraphics
              semiMajorAxis={80_000}
              semiMinorAxis={80_000}
              material={ringColor}
              outline={false}
              height={0}
            />
          </Entity>
        )
      })}

      {/* GS→satellite link lines when in view */}
      {(data?.ground_stations ?? groundStations.map(gs => ({ ...gs, inView: false }))).filter(gs => gs.inView).map(gs => {
        const gsConf = groundStations.find(g => g.name === gs.name)
        if (!gsConf) return null
        return (
          <Entity key={`link-${gs.name}`} name={`link-${gs.name}`}>
            <PolylineGraphics
              positions={[Cartesian3.fromDegrees(gs.lon, gs.lat, 0), satPos]}
              width={1}
              material={Color.fromCssColorString(gsConf.color).withAlpha(0.5)}
            />
          </Entity>
        )
      })}

      {/* All constellation satellites (relay + other primaries) */}
      {constellation.filter(s => s.id !== primarySatId).map(s => {
        const isPrimary    = primarySatIds.includes(s.id)
        const color        = isPrimary ? Color.fromCssColorString('#00e878').withAlpha(0.9) : (PLANE_COLOR[s.plane] ?? Color.WHITE.withAlpha(0.7))
        const isSelected   = s.id === selectedSatId
        const shortName    = s.id.split(' ')[0].replace(/[()]/g, '').slice(0, 8)
        const satDescr     = isPrimary ? `Primary spacecraft: ${s.id}` : `ISL Relay: ${s.id}`
        return (
          <Entity
            key={s.id}
            position={Cartesian3.fromDegrees(s.lon, s.lat, s.alt_m)}
            name={s.id}
            description={satDescr}
          >
            <PointGraphics
              color={color}
              pixelSize={isSelected ? 10 : (isPrimary ? 8 : 5)}
              outlineColor={isSelected ? Color.WHITE.withAlpha(0.9) : Color.BLACK.withAlpha(0.3)}
              outlineWidth={isSelected ? 2 : 1}
            />
            <LabelGraphics
              text={shortName}
              fillColor={color}
              font="12px 'IBM Plex Mono', 'Courier New', monospace"
              pixelOffset={new Cartesian2(12, 0)}
              style={LabelStyle.FILL}
              showBackground={false}
              disableDepthTestDistance={Number.POSITIVE_INFINITY}
              scaleByDistance={new NearFarScalar(1e6, 1.0, 8e6, 0.8)}
            />
          </Entity>
        )
      })}
    </Viewer>
  )
}
