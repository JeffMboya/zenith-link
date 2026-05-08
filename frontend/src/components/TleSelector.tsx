import { useEffect } from 'react'
import { useTleImport } from '../hooks/useTleImport'

interface Props {
  source: 'sim' | 'tle'
  group: string
  count: number
}

export function TleSelector({ source, group, count }: Props) {
  const { importGroup, loading } = useTleImport()

  useEffect(() => {
    importGroup('stations', 0)
  }, [])

  const label = loading
    ? 'LOADING...'
    : source === 'tle'
      ? `TLE: ${group.toUpperCase()} (${count})`
      : 'AWAITING TLE...'

  const color = source === 'tle' ? 'var(--green)' : 'var(--text-dim)'

  return (
    <div style={{
      padding: '4px 10px',
      border: '1px solid var(--border)',
      color, fontSize: 9, letterSpacing: 1,
      borderRadius: 2,
    }}>
      {label}
    </div>
  )
}
