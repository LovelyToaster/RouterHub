import { useEffect, useRef } from 'react'
import { getToken } from '@/api/client'

/**
 * useRequestLogsStream subscribes to /api/request-logs/stream via
 * Server-Sent Events. The backend emits a lightweight "event: update"
 * notification (with no payload) whenever a request log row is inserted or
 * updated; the caller is expected to refetch its list on each notification.
 *
 * The browser's EventSource automatically reconnects on disconnect, so we do
 * not implement any retry logic here.
 */
export function useRequestLogsStream(onUpdate: () => void) {
  const esRef = useRef<EventSource | null>(null)
  // Keep the latest callback in a ref so re-renders do not tear down the
  // connection just because the caller wraps `refetch` inline.
  const cbRef = useRef(onUpdate)
  cbRef.current = onUpdate

  useEffect(() => {
    const token = getToken()
    if (!token) {
      return
    }

    const url = `/api/request-logs/stream?token=${encodeURIComponent(token)}`
    const es = new EventSource(url)
    esRef.current = es

    const handle = () => {
      cbRef.current()
    }

    // Backend uses a named `update` event; browsers also fire onmessage for
    // unnamed events, but we listen explicitly for safety.
    es.addEventListener('update', handle)

    return () => {
      es.removeEventListener('update', handle)
      es.close()
      esRef.current = null
    }
  }, [])
}
