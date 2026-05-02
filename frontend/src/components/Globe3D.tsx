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
} from 'cesium'
import type { Viewer as CesiumViewer } from 'cesium'
import { Viewer, Entity, PointGraphics, LabelGraphics, PolylineGraphics } from 'resium'
import type { StateUpdate, GroundStation } from '../types'
import type { OrbitalPos } from '../hooks/useOrbitalPosition'
import type { ConstellationSat } from '../hooks/useConstellation'
import { useForwardTrack } from '../hooks/useForwardTrack'

Ion.defaultAccessToken = (import.meta as unknown as { env: Record<string, string> }).env.VITE_CESIUM_TOKEN ?? ''

const GS_COLORS: Record<string, string> = {
  Nairobi:        '#00e878',
  Svalbard:       '#00c8f0',
  'Punta Arenas': '#c060f0',
}

const PLANE_COLOR: Record<string, Color> = {
  
  A:   Color.CYAN.withAlpha(0.9),
  B:   Color.fromCssColorString('#00e878').withAlpha(0.9),
  C:   Color.fromCssColorString('#f0a800').withAlpha(0.9),
  D:   Color.fromCssColorString('#c060f0').withAlpha(0.9),
  ISL: Color.fromCssColorString('#f06040').withAlpha(0.9), 
  
  stations: Color.WHITE.withAlpha(0.95),
  starlink: Color.fromCssColorString('#00c8f0').withAlpha(0.8),
  planet:   Color.fromCssColorString('#00e8c0').withAlpha(0.8),
  active:   Color.fromCssColorString('#f0a800').withAlpha(0.7),
}

export interface RelayHealth {
  online: boolean
  bufferHasData: boolean
  lastFetchAt: string | null
  upstreamNode?: string
}

function islBeamColor(health: RelayHealth | null | undefined): Color {
  if (!health || !health.online) return Color.WHITE.withAlpha(0.08)
  if (health.bufferHasData) return Color.fromCssColorString('#00e878').withAlpha(0.5)
  return Color.fromCssColorString('#f0a800').withAlpha(0.3)
}

interface Props {
  data: StateUpdate | null
  orbitalPos: OrbitalPos | null
  constellation: ConstellationSat[]
  selectedSatId: string
  onSelectSat: (id: string) => void
  relay1Health?: RelayHealth | null
  relay2Health?: RelayHealth | null
  tleSource: 'sim' | 'tle'
  primarySatId: string
}

function gsColor(gs: GroundStation): Color {
  const hex = GS_COLORS[gs.name] ?? '#5878a0'
  return Color.fromCssColorString(hex).withAlpha(gs.inView ? 1.0 : 0.4)
}

export function Globe3D({ data, orbitalPos, constellation, selectedSatId, onSelectSat, relay1Health, relay2Health, tleSource, primarySatId }: Props) {
  const viewerRef = useRef<{ cesiumElement: CesiumViewer } | null>(null)
  const flew = useRef(false)
  const imageryInit = useRef(false)
  const clickHandlerRef = useRef<ScreenSpaceEventHandler | null>(null)
  const satelliteIdsRef = useRef<Set<string>>(new Set())
  const forwardTrack = useForwardTrack()

  
  
  
  useEffect(() => {
    if (imageryInit.current) return
    const viewer = viewerRef.current?.cesiumElement
    if (!viewer) return
    imageryInit.current = true

    const layers = viewer.imageryLayers
    layers.removeAll()
    TileMapServiceImageryProvider.fromUrl(
      buildModuleUrl('Assets/Textures/NaturalEarthII'),
    ).then(p => layers.addImageryProvider(p)).catch(() => {})

    viewer.camera.setView({
      destination: Cartesian3.fromDegrees(0, 20, 22_000_000),
    })
  })

  
  useEffect(() => {
    const ids = new Set(constellation.map(s => s.id))
    ids.add(primarySatId)
    satelliteIdsRef.current = ids
  }, [constellation, primarySatId])

  
  
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
      if (defined(picked) && picked.id) {
        const name: string | undefined = picked.id.name
        if (name && satelliteIdsRef.current.has(name)) onSelectSat(name)
      }
    }, ScreenSpaceEventType.LEFT_CLICK)

    clickHandlerRef.current = handler
    return () => {
      handler.destroy()
      clickHandlerRef.current = null
    }
  }, [onSelectSat])

  
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

  const pastTrack = (data?.ground_track ?? []).map(p =>
    Cartesian3.fromDegrees(p.lon, p.lat, p.alt)
  )
  const futureTrack = forwardTrack.map(p =>
    Cartesian3.fromDegrees(p.lon, p.lat, p.alt_m)
  )

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
      style={{ position: 'fixed', top: 0, left: 0, width: '100%', height: '100%' }}
    >
      
      {futureTrack.length >= 2 && (
        <Entity name="forward-track">
          <PolylineGraphics
            positions={futureTrack}
            width={1.2}
            material={Color.WHITE.withAlpha(0.25)}
          />
        </Entity>
      )}

      
      {pastTrack.length >= 2 && (
        <Entity name="past-track">
          <PolylineGraphics
            positions={pastTrack}
            width={1.8}
            material={Color.WHITE.withAlpha(0.6)}
          />
        </Entity>
      )}

      
      <Entity position={satPos} name={primarySatId}>
        <PointGraphics
          color={Color.CYAN}
          pixelSize={selectedSatId === primarySatId ? 12 : 10}
          outlineColor={selectedSatId === primarySatId
            ? Color.WHITE.withAlpha(0.9)
            : Color.fromCssColorString('#004060')}
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

      
      {data?.ground_stations.map(gs => (
        <Entity
          key={gs.name}
          position={Cartesian3.fromDegrees(gs.lon, gs.lat, 0)}
          name={gs.name}
        >
          <PointGraphics
            color={gsColor(gs)}
            pixelSize={7}
            outlineColor={Color.fromCssColorString('#071830')}
            outlineWidth={1}
          />
          <LabelGraphics
            text={gs.name.toUpperCase()}
            fillColor={gsColor(gs)}
            font="10px 'IBM Plex Mono', 'Courier New', monospace"
            pixelOffset={new Cartesian2(12, 0)}
            style={LabelStyle.FILL}
            disableDepthTestDistance={Number.POSITIVE_INFINITY}
          />
        </Entity>
      ))}

      
      {data?.ground_stations.filter(gs => gs.inView).map(gs => (
        <Entity key={`link-${gs.name}`} name={`link-${gs.name}`}>
          <PolylineGraphics
            positions={[Cartesian3.fromDegrees(gs.lon, gs.lat, 0), satPos]}
            width={1}
            material={Color.fromCssColorString(GS_COLORS[gs.name] ?? '#00c8f0').withAlpha(0.5)}
          />
        </Entity>
      ))}

      
      {(() => {
        const sc2 = constellation.find(s => s.id === 'SC-2')
        const sc3 = constellation.find(s => s.id === 'SC-3')
        return (
          <>
            {sc2 && (
              <Entity name="isl-at1-sc2">
                <PolylineGraphics
                  positions={[satPos, Cartesian3.fromDegrees(sc2.lon, sc2.lat, sc2.alt_m)]}
                  width={1.5}
                  material={islBeamColor(relay1Health)}
                />
              </Entity>
            )}
            {sc3 && (
              <Entity name="isl-at1-sc3">
                <PolylineGraphics
                  positions={[satPos, Cartesian3.fromDegrees(sc3.lon, sc3.lat, sc3.alt_m)]}
                  width={1.5}
                  material={islBeamColor(relay2Health)}
                />
              </Entity>
            )}
            {sc2 && sc3 && (
              <Entity name="isl-sc2-sc3">
                <PolylineGraphics
                  positions={[
                    Cartesian3.fromDegrees(sc2.lon, sc2.lat, sc2.alt_m),
                    Cartesian3.fromDegrees(sc3.lon, sc3.lat, sc3.alt_m),
                  ]}
                  width={1}
                  material={Color.fromCssColorString('#f06040').withAlpha(0.2)}
                />
              </Entity>
            )}
          </>
        )
      })()}

      
      {constellation.filter(s => s.id !== primarySatId).map(s => {
        const color = PLANE_COLOR[s.plane] ?? Color.WHITE.withAlpha(0.7)
        const isSelected = s.id === selectedSatId
        
        const shortName = s.id.split(' ')[0].replace(/[()]/g, '').slice(0, 8)
        return (
          <Entity
            key={s.id}
            position={Cartesian3.fromDegrees(s.lon, s.lat, s.alt_m)}
            name={s.id}
          >
            <PointGraphics
              color={color}
              pixelSize={isSelected ? 10 : 5}
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
