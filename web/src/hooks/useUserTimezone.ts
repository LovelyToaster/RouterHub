import { useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getMe, updateMe } from '@/api/client'
import { getBrowserTimezone } from '@/lib/timezone'

/**
 * useUserTimezone reads the current user's stored timezone. If the value is
 * empty (e.g. first login on a fresh install), it writes the browser's
 * detected IANA name back to the server exactly once. Consumers get a
 * guaranteed non-empty IANA string via `tz`.
 */
export function useUserTimezone(): { tz: string; ready: boolean } {
  const queryClient = useQueryClient()
  const { data: me } = useQuery({ queryKey: ['me'], queryFn: getMe })
  const writeAttemptedRef = useRef(false)

  const writeMut = useMutation({
    mutationFn: (tz: string) => updateMe({ timezone: tz }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
  })

  useEffect(() => {
    if (!me) return
    if (me.timezone) return
    if (writeAttemptedRef.current) return
    writeAttemptedRef.current = true
    writeMut.mutate(getBrowserTimezone())
    // writeMut identity is stable enough for this one-shot effect; deps are intentional.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me])

  const tz = me?.timezone || getBrowserTimezone()
  return { tz, ready: !!me }
}
