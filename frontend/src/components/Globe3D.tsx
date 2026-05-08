import { useEffect, useRef, useState } from 'react'
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
import type { SatCommandStateMap } from '../hooks/useSatCommandState'
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
  tleSource:     'sim' | 'tle'
  primarySatId:  string
  primarySatIds: string[]
  cmdState:      SatCommandStateMap
}

export function Globe3D({
  data, orbitalPos, constellation, groundStations,
  selectedSatId, onSelectSat,
  relay1Health,
  tleSource, primarySatId, primarySatIds,
  cmdState,
}: Props) {
  const viewerRef     = useRef<{ cesiumElement: CesiumViewer } | null>(null)
  const flew          = useRef(false)
  const imageryInit   = useRef(false)
  const clickHandlerRef = useRef<ScreenSpaceEventHandler | null>(null)
  const allEntityIds  = useRef<Set<string>>(new Set())
  const forwardTrack  = useForwardTrack()

  // Sonar ring animation — local radius that expands from 0 → 600km then fades
  const [sonarRadii, setSonarRadii] = useState<Record<string, number>>({})

  useEffect(() => {
    if (cmdState.sonarSats.length === 0) return
    const id = setInterval(() => {
      setSonarRadii(prev => {
        const next = { ...prev }
        let changed = false
        for (const satId of cmdState.sonarSats) {
          const r = (prev[satId] ?? 0) + 30_000  // expand 30km per tick
          next[satId] = r
          changed = true
        }
        if (!changed) return prev
        return next
      })
    }, 80)
    return () => clearInterval(id)
  }, [cmdState.sonarSats])

  // Reset sonar radius when ring cleared
  useEffect(() => {
    setSonarRadii(prev => {
      const next = { ...prev }
      let changed = false
      for (const key of Object.keys(prev)) {
        if (!cmdState.sonarSats.includes(key)) {
          delete next[key]
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [cmdState.sonarSats])

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

  // All primary sat positions (for federated beam and badges)
  const allPrimaryPositions = primarySatIds.map(id => {
    if (id === primarySatId) return { id, pos: satPos, lat: satLat, lon: satLon, alt: satAlt }
    const sat = constellation.find(s => s.id === id)
    if (!sat) return null
    return { id, pos: Cartesian3.fromDegrees(sat.lon, sat.lat, sat.alt_m), lat: sat.lat, lon: sat.lon, alt: sat.alt_m }
  }).filter((p): p is { id: string; pos: Cartesian3; lat: number; lon: number; alt: number } => !!p)

  // Dot color for a primary satellite based on command state
  const primaryDotColor = (satId: string): Color => {
    const phase = cmdState.rebootPhase[satId]
    const mode  = cmdState.modes[satId]
    if (phase === 'red') return cmdState.blinkTick ? Color.RED : Color.RED.withAlpha(0.15)
    if (mode  === 'safe') return Color.fromCssColorString('#f0a800')  // amber
    return satId === primarySatId ? Color.CYAN : Color.fromCssColorString('#00e878')
  }

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

      {/* Primary satellite (ISS) — mode-aware color, reboot hide, sonar ring, badge */}
      {cmdState.rebootPhase[primarySatId] !== 'hidden' && (
        <Entity position={satPos} name={primarySatId}
          description={`Primary spacecraft: ${primarySatId}`}
        >
          <PointGraphics
            color={primaryDotColor(primarySatId)}
            pixelSize={selectedSatId === primarySatId ? 13 : 10}
            outlineColor={selectedSatId === primarySatId ? Color.WHITE.withAlpha(0.9) : Color.fromCssColorString('#004060')}
            outlineWidth={selectedSatId === primarySatId ? 2.5 : 2}
          />
          <LabelGraphics
            text={
              (cmdState.deployedAgents[primarySatId] ?? []).includes('edge-inference')
                ? `${primaryLabel} ⋆`
                : primaryLabel
            }
            fillColor={primaryDotColor(primarySatId)}
            font="12px 'IBM Plex Mono', 'Courier New', monospace"
            pixelOffset={new Cartesian2(14, 0)}
            style={LabelStyle.FILL}
            showBackground={false}
            disableDepthTestDistance={Number.POSITIVE_INFINITY}
            scaleByDistance={new NearFarScalar(1e6, 1.0, 8e6, 0.8)}
          />
        </Entity>
      )}

      {/* Sonar ring — expands from satellite position outward */}
      {cmdState.sonarSats.includes(primarySatId) && (sonarRadii[primarySatId] ?? 0) > 0 && (
        <Entity position={satPos} name={`sonar-${primarySatId}`}>
          <EllipseGraphics
            semiMajorAxis={sonarRadii[primarySatId] ?? 0}
            semiMinorAxis={sonarRadii[primarySatId] ?? 0}
            material={Color.fromCssColorString('#00c8f0').withAlpha(
              Math.max(0, 0.6 - (sonarRadii[primarySatId] ?? 0) / 700_000)
            )}
            outline={true}
            outlineColor={Color.fromCssColorString('#00c8f0').withAlpha(0.8)}
            height={satAlt}
          />
        </Entity>
      )}

      {/* Federated beam — teal pulsing lines between all primaries when active */}
      {cmdState.federatedBeam && allPrimaryPositions.length >= 2 && (
        <>
          {allPrimaryPositions.slice(0, -1).map((p, i) => (
            <Entity key={`fed-beam-${p.id}-${allPrimaryPositions[i+1].id}`} name={`fed-beam-${i}`}>
              <PolylineGraphics
                positions={[p.pos, allPrimaryPositions[i+1].pos]}
                width={2.5}
                material={Color.fromCssColorString('#00e8c0').withAlpha(0.55)}
              />
            </Entity>
          ))}
          {/* Close the triangle */}
          {allPrimaryPositions.length >= 3 && (
            <Entity name="fed-beam-close">
              <PolylineGraphics
                positions={[allPrimaryPositions[0].pos, allPrimaryPositions[2].pos]}
                width={2.5}
                material={Color.fromCssColorString('#00e8c0').withAlpha(0.55)}
              />
            </Entity>
          )}
        </>
      )}

      {/* Orbit predictor — extended forward track (dotted) when deployed */}
      {cmdState.orbitPredictors.has(primarySatId) && futureTrack.length >= 2 && (
        <Entity name="orbit-predictor-ext">
          <PolylineGraphics
            positions={futureTrack}
            width={1}
            material={Color.fromCssColorString('#c060f0').withAlpha(0.5)}
          />
        </Entity>
      )}

      {/* Shield overlay dot for anomaly detector */}
      {(cmdState.deployedAgents[primarySatId] ?? []).includes('anomaly-detector') && (
        <Entity position={satPos} name={`shield-${primarySatId}`}>
          <EllipseGraphics
            semiMajorAxis={35_000}
            semiMinorAxis={35_000}
            material={Color.fromCssColorString('#40d080').withAlpha(0.12)}
            outline={true}
            outlineColor={Color.fromCssColorString('#40d080').withAlpha(0.6)}
            height={satAlt}
          />
        </Entity>
      )}

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
        const isPrimary  = primarySatIds.includes(s.id)
        const isSelected = s.id === selectedSatId
        const isHidden   = cmdState.rebootPhase[s.id] === 'hidden'
        const baseColor  = isPrimary
          ? primaryDotColor(s.id)
          : (PLANE_COLOR[s.plane] ?? Color.WHITE.withAlpha(0.7))
        const shortName  = s.id.split(' ')[0].replace(/[()]/g, '').slice(0, 8)
        const badgeLabel = isPrimary && (cmdState.deployedAgents[s.id] ?? []).includes('edge-inference')
          ? `${shortName} ⋆` : shortName
        const satDescr   = isPrimary ? `Primary spacecraft: ${s.id}` : `ISL Relay: ${s.id}`
        if (isHidden) return null
        return (
          <Entity
            key={s.id}
            position={Cartesian3.fromDegrees(s.lon, s.lat, s.alt_m)}
            name={s.id}
            description={satDescr}
          >
            <PointGraphics
              color={baseColor}
              pixelSize={isSelected ? 10 : (isPrimary ? 8 : 5)}
              outlineColor={isSelected ? Color.WHITE.withAlpha(0.9) : Color.BLACK.withAlpha(0.3)}
              outlineWidth={isSelected ? 2 : 1}
            />
            <LabelGraphics
              text={badgeLabel}
              fillColor={baseColor}
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

      {/* Sonar rings for non-primary-1 satellites */}
      {cmdState.sonarSats.filter(id => id !== primarySatId).map(satId => {
        const sat = constellation.find(s => s.id === satId)
        if (!sat || !(sonarRadii[satId] > 0)) return null
        return (
          <Entity key={`sonar-${satId}`} position={Cartesian3.fromDegrees(sat.lon, sat.lat, sat.alt_m)} name={`sonar-${satId}`}>
            <EllipseGraphics
              semiMajorAxis={sonarRadii[satId]}
              semiMinorAxis={sonarRadii[satId]}
              material={Color.fromCssColorString('#00c8f0').withAlpha(Math.max(0, 0.6 - sonarRadii[satId] / 700_000))}
              outline={true}
              outlineColor={Color.fromCssColorString('#00c8f0').withAlpha(0.8)}
              height={sat.alt_m}
            />
          </Entity>
        )
      })}
    </Viewer>
  )
}
