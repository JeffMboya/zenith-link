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

// Plane colours for constellation members
const PLANE_COLOR: Record<string, Color> = {
  A: Color.CYAN.withAlpha(0.9),
  B: Color.fromCssColorString('#00e878').withAlpha(0.9),
  C: Color.fromCssColorString('#f0a800').withAlpha(0.9),
  D: Color.fromCssColorString('#c060f0').withAlpha(0.9),
}

interface Props {
  data: StateUpdate | null
  orbitalPos: OrbitalPos | null
  constellation: ConstellationSat[]
  selectedSatId: string
  onSelectSat: (id: string) => void
}

function gsColor(gs: GroundStation): Color {
  const hex = GS_COLORS[gs.name] ?? '#5878a0'
  return Color.fromCssColorString(hex).withAlpha(gs.inView ? 1.0 : 0.4)
}

export function Globe3D({ data, orbitalPos, constellation, selectedSatId, onSelectSat }: Props) {
  const viewerRef = useRef<{ cesiumElement: CesiumViewer } | null>(null)
  const flew = useRef(false)
  const imageryInit = useRef(false)
  const clickHandlerRef = useRef<ScreenSpaceEventHandler | null>(null)
  const forwardTrack = useForwardTrack()

  // Imagery + camera — no dependency array so it retries every render until the
  // Cesium viewer is actually initialised (viewerRef.current may be null on first paint).
  // imageryInit.current prevents it from running more than once.
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

  // Click handler — re-registers when onSelectSat changes so the closure is always fresh.
  // Also no-ops until the viewer is ready.
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
        if (name && /^AT-\d+$/.test(name)) onSelectSat(name)
      }
    }, ScreenSpaceEventType.LEFT_CLICK)

    clickHandlerRef.current = handler
    return () => {
      handler.destroy()
      clickHandlerRef.current = null
    }
  }, [onSelectSat])

  // Fly to satellite once we have the first position — full globe view
  const firstPos = orbitalPos ?? (data?.satellite
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

  // AT-1 position (primary, 1Hz feed)
  const satLat = orbitalPos?.lat ?? data?.satellite.latitude ?? 0
  const satLon = orbitalPos?.lon ?? data?.satellite.longitude ?? 0
  const satAlt = orbitalPos?.alt ?? data?.satellite.altitude ?? 550_000
  const satPos = Cartesian3.fromDegrees(satLon, satLat, satAlt)

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
      {/* Forward track — white, low alpha; stands out against both ocean and land */}
      {futureTrack.length >= 2 && (
        <Entity name="forward-track">
          <PolylineGraphics
            positions={futureTrack}
            width={1.2}
            material={Color.WHITE.withAlpha(0.25)}
          />
        </Entity>
      )}

      {/* Past ground track — white, brighter */}
      {pastTrack.length >= 2 && (
        <Entity name="past-track">
          <PolylineGraphics
            positions={pastTrack}
            width={1.8}
            material={Color.WHITE.withAlpha(0.6)}
          />
        </Entity>
      )}

      {/* AT-1 — primary satellite, cyan, larger, always labelled */}
      <Entity position={satPos} name="AT-1">
        <PointGraphics
          color={Color.CYAN}
          pixelSize={selectedSatId === 'AT-1' ? 12 : 10}
          outlineColor={selectedSatId === 'AT-1'
            ? Color.WHITE.withAlpha(0.9)
            : Color.fromCssColorString('#004060')}
          outlineWidth={selectedSatId === 'AT-1' ? 2.5 : 2}
        />
        <LabelGraphics
          text="AT-1"
          fillColor={Color.CYAN}
          font="11px 'IBM Plex Mono', 'Courier New', monospace"
          pixelOffset={new Cartesian2(14, 0)}
          style={LabelStyle.FILL}
          showBackground={false}
        />
      </Entity>

      {/* Ground stations */}
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
            font="9px 'IBM Plex Mono', 'Courier New', monospace"
            pixelOffset={new Cartesian2(12, 0)}
            style={LabelStyle.FILL}
          />
        </Entity>
      ))}

      {/* Contact lines */}
      {data?.ground_stations.filter(gs => gs.inView).map(gs => (
        <Entity key={`link-${gs.name}`} name={`link-${gs.name}`}>
          <PolylineGraphics
            positions={[Cartesian3.fromDegrees(gs.lon, gs.lat, 0), satPos]}
            width={1}
            material={Color.fromCssColorString(GS_COLORS[gs.name] ?? '#00c8f0').withAlpha(0.5)}
          />
        </Entity>
      ))}

      {/* Constellation members AT-2 through AT-16 */}
      {constellation.filter(s => s.id !== 'AT-1').map(s => {
        const color = PLANE_COLOR[s.plane] ?? Color.WHITE.withAlpha(0.7)
        const isSelected = s.id === selectedSatId
        return (
          <Entity
            key={s.id}
            position={Cartesian3.fromDegrees(s.lon, s.lat, s.alt_m)}
            name={s.id}
          >
            <PointGraphics
              color={color}
              pixelSize={isSelected ? 10 : 6}
              outlineColor={isSelected ? Color.WHITE.withAlpha(0.9) : Color.BLACK.withAlpha(0.4)}
              outlineWidth={isSelected ? 2 : 1}
            />
            <LabelGraphics
              text={s.id}
              fillColor={color}
              font="9px 'IBM Plex Mono', 'Courier New', monospace"
              pixelOffset={new Cartesian2(10, 0)}
              style={LabelStyle.FILL}
              showBackground={false}
            />
          </Entity>
        )
      })}
    </Viewer>
  )
}
