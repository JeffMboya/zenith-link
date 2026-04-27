import { useState } from 'react'

export type TleGroup = 'stations' | 'starlink' | 'planet' | 'active'

export interface TleImportResult {
  group: string
  imported: number
  loaded_at: string
}

export function useTleImport() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function importGroup(group: TleGroup, limit = 100): Promise<TleImportResult | null> {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams({ group })
      if (group !== 'stations') params.set('limit', String(limit))
      const res = await fetch(`/tle/import?${params}`, { method: 'POST' })
      const body = await res.json() as TleImportResult & { error?: string }
      if (!res.ok) { setError(body.error ?? 'Import failed'); return null }
      return body
    } catch (e) {
      setError(String(e))
      return null
    } finally {
      setLoading(false)
    }
  }

  async function clearTle(): Promise<void> {
    setLoading(true)
    setError(null)
    try { await fetch('/tle/clear', { method: 'POST' }) }
    finally { setLoading(false) }
  }

  return { importGroup, clearTle, loading, error }
}
