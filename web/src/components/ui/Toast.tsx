import { useEffect } from 'react'
import { CheckCircle2, XCircle, X } from 'lucide-react'

export type ToastType = 'success' | 'error'

export function Toast({
  type,
  message,
  onClose,
  duration = 3000,
}: {
  type: ToastType
  message: string
  onClose: () => void
  duration?: number
}) {
  useEffect(() => {
    const timer = setTimeout(onClose, duration)
    return () => clearTimeout(timer)
  }, [onClose, duration])

  const isSuccess = type === 'success'
  return (
    <div className="fixed top-6 right-6 z-[200] flex items-center gap-3 rounded-xl border border-card-border bg-surface-light px-4 py-3 shadow-xl max-w-sm">
      {isSuccess ? (
        <CheckCircle2 className="w-5 h-5 text-emerald-400 shrink-0" />
      ) : (
        <XCircle className="w-5 h-5 text-red-400 shrink-0" />
      )}
      <span className="text-sm text-text-primary break-all">{message}</span>
      <button
        type="button"
        onClick={onClose}
        className="ml-2 text-text-muted hover:text-text-primary transition-colors"
        aria-label="close"
      >
        <X className="w-4 h-4" />
      </button>
    </div>
  )
}
