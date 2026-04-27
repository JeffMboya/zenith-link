import { useEffect, useRef } from 'react'
import { Ion, Cartesian3, Cartesian2, Color, LabelStyle } from 'cesium'
import type { Viewer as CesiumViewer } from 'cesium'
import { Viewer, Entity, PointGraphics, LabelGraphics, PolylineGraphics } from 'resium'
import type { StateUpdate, GroundStation } from '../types'
import type { OrbitalPos } from '../hooks/useOrbitalPosition'

// Set Cesium Ion token from env (Vite exposes VITE_* vars)
Ion.defaultAccessToken = (import.meta as unknown as { env: Record<string, string> }).env.VITE_CESIUM_TOKEN ?? ''

const GS_COLORS: Record<string, string> = {
  Nairobi: '#00e878',
  Svalbard: '#00c8f0',
  'Punta Arenas': '#c060f0',
}

interface Props {
  data: StateUpdate | null
  orbitalPos: OrbitalPos | null   // 1Hz position feed independent of telemetry frames
}

function gsColor(gs: GroundStation): Color {
  const hex = GS_COLORS[gs.name] ?? '#5878a0'
  return Color.fromCssColorString(hex).withAlpha(gs.inView ? 1.0 : 0.4)
}

export function Globe3D({ data, orbitalPos }: Props) {
  const viewerRef = useRef<{ cesiumElement: CesiumViewer } | null>(null)

  // Fly to satellite on first position received
  const flew = useRef(false)
  const firstPos = orbitalPos ?? (data?.satellite ? { lat: data.satellite.latitude, lon: data.satellite.longitude, alt: data.satellite.altitude } : null)
  useEffect(() => {
    if (!firstPos || flew.current) return
    const viewer = viewerRef.current?.cesiumElement
    if (!viewer) return
    flew.current = true
    viewer.camera.flyTo({
      destination: Cartesian3.fromDegrees(firstPos.lon, firstPos.lat, firstPos.alt + 3_000_000),
      duration: 2,
    })
  }, [firstPos])

  // Satellite position: prefer 1Hz orbital feed, fall back to last WS telemetry
  const satLat = orbitalPos?.lat ?? data?.satellite.latitude ?? 0
  const satLon = orbitalPos?.lon ?? data?.satellite.longitude ?? 0
  const satAlt = orbitalPos?.alt ?? data?.satellite.altitude ?? 550_000
  const satPos = Cartesian3.fromDegrees(satLon, satLat, satAlt)

  const trackPositions = data?.ground_track.map(p =>
    Cartesian3.fromDegrees(p.lon, p.lat, p.alt)
  ) ?? []

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
      {/* Satellite — updates at 1Hz from orbital feed */}
      <Entity position={satPos} name="SAT-1">
        <PointGraphics
          color={Color.CYAN}
          pixelSize={10}
          outlineColor={Color.fromCssColorString('#004060')}
          outlineWidth={2}
        />
        <LabelGraphics
          text="AT-1"
          fillColor={Color.CYAN}
          font="11px 'Courier New'"
          pixelOffset={new Cartesian2(14, 0)}
          style={LabelStyle.FILL}
          showBackground={false}
        />
      </Entity>

      {/* Ground track from telemetry history */}
      {trackPositions.length >= 2 && (
        <Entity name="ground-track">
          <PolylineGraphics
            positions={trackPositions}
            width={1.5}
            material={Color.CYAN.withAlpha(0.35)}
          />
        </Entity>
      )}

      {/* Ground stations */}
      {data?.ground_stations.map(gs => (
        <Entity
          key={gs.name}
          position={Cartesian3.fromDegrees(gs.lon, gs.lat, 0)}
          name={gs.name}
        >
          <PointGraphics
            color={gsColor(gs)}
            pixelSize={8}
            outlineColor={Color.fromCssColorString('#071830')}
            outlineWidth={1}
          />
          <LabelGraphics
            text={gs.name.toUpperCase()}
            fillColor={gsColor(gs)}
            font="9px 'Courier New'"
            pixelOffset={new Cartesian2(12, 0)}
            style={LabelStyle.FILL}
          />
        </Entity>
      ))}

      {/* Contact lines: ground station → satellite when in view */}
      {data?.ground_stations.filter(gs => gs.inView).map(gs => (
        <Entity key={`link-${gs.name}`} name={`link-${gs.name}`}>
          <PolylineGraphics
            positions={[
              Cartesian3.fromDegrees(gs.lon, gs.lat, 0),
              satPos,
            ]}
            width={1}
            material={Color.fromCssColorString(GS_COLORS[gs.name] ?? '#00c8f0').withAlpha(0.5)}
          />
        </Entity>
      ))}
    </Viewer>
  )
}
