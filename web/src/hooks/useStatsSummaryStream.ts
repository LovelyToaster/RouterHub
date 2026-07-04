import { useEffect, useRef, useState } from 'react'
import type { StatsSummary, RangeKey } from '@/types'
import { getToken } from '@/api/client'

interface StreamState {
  data: StatsSummary | null
  error: Error | null
  connected: boolean
  /** True while a new range is requested but no matching payload has arrived yet. */
  pending: boolean
}

/**
 * useStatsSummaryStream subscribes to /api/stats/summary/stream via Server-Sent
 * Events. The server pushes an initial payload immediately, then re-pushes
 * whenever a new request log is inserted. When `range` changes, the current
 * connection is closed and a new one is opened with the updated parameter.
 *
 * Data from the previous range is kept until the first payload of the new
 * range arrives, so the UI does not flash a skeleton on every switch.
 */
export function useStatsSummaryStream(range: RangeKey = 'all'): StreamState {
  const [data, setData] = useState<StatsSummary | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [connected, setConnected] = useState(false)
  const [pending, setPending] = useState(false)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    const token = getToken()
    if (!token) {
      setError(new Error('missing session token'))
      return
    }

    // Keep previous data visible; only mark the panel as pending until the
    // first payload matching the new range arrives.
    setPending(true)
    setError(null)

    const url = `/api/stats/summary/stream?token=${encodeURIComponent(token)}&range=${encodeURIComponent(range)}`
    const es = new EventSource(url)
    esRef.current = es

    es.onopen = () => {
      setConnected(true)
      setError(null)
    }

    es.onmessage = (ev) => {
      try {
        const parsed = JSON.parse(ev.data) as StatsSummary
        setData(parsed)
        if (parsed.range === range) {
          setPending(false)
        }
      } catch (err) {
        // Ignore malformed frames.
      }
    }

    es.addEventListener('error', () => {
      setConnected(false)
      // EventSource will attempt to reconnect automatically.
    })

    return () => {
      es.close()
      esRef.current = null
    }
  }, [range])

  return { data, error, connected, pending }
}
