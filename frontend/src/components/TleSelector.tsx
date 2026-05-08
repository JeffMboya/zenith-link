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

  if (loading || source !== 'tle') return null

  return (
    <div style={{
      padding: '4px 10px',
      border: '1px solid var(--border)',
      color: 'var(--green)', fontSize: 9, letterSpacing: 1,
      borderRadius: 2,
    }}>
      {`TLE: ${group.toUpperCase()} (${count})`}
    </div>
  )
}
