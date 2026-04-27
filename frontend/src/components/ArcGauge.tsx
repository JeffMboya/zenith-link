interface Props {
  value: number
  min: number
  max: number
  label: string
  unit: string
  color?: string
  size?: number
}

export function ArcGauge({ value, min, max, label, unit, color = '#00c8f0', size = 90 }: Props) {
  const pct = Math.max(0, Math.min(1, (value - min) / (max - min)))
  const r = 34
  const cx = 50
  const cy = 52
  const startAngle = -220
  const sweepAngle = 260
  const toRad = (d: number) => (d * Math.PI) / 180

  const arcPath = (pct: number) => {
    const end = startAngle + sweepAngle * pct
    const x1 = cx + r * Math.cos(toRad(startAngle))
    const y1 = cy + r * Math.sin(toRad(startAngle))
    const x2 = cx + r * Math.cos(toRad(end))
    const y2 = cy + r * Math.sin(toRad(end))
    const large = sweepAngle * pct > 180 ? 1 : 0
    return `M ${x1} ${y1} A ${r} ${r} 0 ${large} 1 ${x2} ${y2}`
  }

  const displayVal = Math.abs(value) < 10 ? value.toFixed(2) : value.toFixed(1)

  return (
    <svg width={size} height={size} viewBox="0 0 100 100">
      {/* Track */}
      <path d={arcPath(1)} fill="none" stroke="#1a4878" strokeWidth={5} strokeLinecap="round" />
      {/* Value arc */}
      <path d={arcPath(pct)} fill="none" stroke={color} strokeWidth={5} strokeLinecap="round" />
      {/* Value text */}
      <text x={cx} y={cy - 4} textAnchor="middle" fill={color} fontSize={13} fontFamily="Courier New" fontWeight="bold">
        {displayVal}
      </text>
      <text x={cx} y={cy + 8} textAnchor="middle" fill="#5878a0" fontSize={7} fontFamily="Courier New">
        {unit}
      </text>
      {/* Label */}
      <text x={cx} y={cy + 22} textAnchor="middle" fill="#3a6898" fontSize={7} fontFamily="Courier New" letterSpacing={1}>
        {label}
      </text>
    </svg>
  )
}
