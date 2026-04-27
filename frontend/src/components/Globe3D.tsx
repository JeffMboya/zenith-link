import { useEffect, useRef } from 'react'
import { Ion, Cartesian3, Cartesian2, Color, LabelStyle } from 'cesium'
import type { Viewer as CesiumViewer } from 'cesium'
import { Viewer, Entity, PointGraphics, LabelGraphics, PolylineGraphics } from 'resium'
import type { StateUpdate, GroundStation } from '../types'

// Set Cesium Ion token from env (Vite exposes VITE_* vars)
Ion.defaultAccessToken = (import.meta as unknown as { env: Record<string, string> }).env.VITE_CESIUM_TOKEN ?? ''

const GS_COLORS: Record<string, string> = {
  Nairobi: '#00e878',
  Svalbard: '#00c8f0',
  'Punta Arenas': '#c060f0',
}

interface Props {
  data: StateUpdate | null
}

function gsColor(gs: GroundStation): Color {
  const hex = GS_COLORS[gs.name] ?? '#5878a0'
  return Color.fromCssColorString(hex).withAlpha(gs.inView ? 1.0 : 0.4)
}

export function Globe3D({ data }: Props) {
  const viewerRef = useRef<{ cesiumElement: CesiumViewer } | null>(null)

  // Fly to satellite on first position received
  const flew = useRef(false)
  useEffect(() => {
    if (!data || flew.current) return
    const viewer = viewerRef.current?.cesiumElement
    if (!viewer) return
    flew.current = true
    viewer.camera.flyTo({
      destination: Cartesian3.fromDegrees(
        data.satellite.longitude,
        data.satellite.latitude,
        data.satellite.altitude + 3_000_000,
      ),
      duration: 2,
    })
  }, [data])

  const sat = data?.satellite
  const satPos = sat
    ? Cartesian3.fromDegrees(sat.longitude, sat.latitude, sat.altitude)
    : Cartesian3.fromDegrees(0, 0, 550_000)

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
      {/* Satellite */}
      <Entity position={satPos} name="SAT-1">
        <PointGraphics
          color={Color.CYAN}
          pixelSize={10}
          outlineColor={Color.fromCssColorString('#004060')}
          outlineWidth={2}
        />
        <LabelGraphics
          text="SAT-1"
          fillColor={Color.CYAN}
          font="11px 'Courier New'"
          pixelOffset={new Cartesian2(14, 0)}
          style={LabelStyle.FILL}
          showBackground={false}
        />
      </Entity>

      {/* Ground track */}
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
