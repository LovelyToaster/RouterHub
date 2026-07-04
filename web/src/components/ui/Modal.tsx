import { type ReactNode } from 'react'
import { X } from 'lucide-react'
import { ModalPortal } from '@/components/ui/ModalPortal'

interface ModalProps {
  children: ReactNode
  title?: ReactNode
  onClose: () => void
  maxWidth?: 'sm' | 'md' | 'lg' | 'xl'
  zIndex?: string
  showClose?: boolean
}

const maxWidthClasses = {
  sm: 'max-w-lg',
  md: 'max-w-2xl',
  lg: 'max-w-3xl',
  xl: 'max-w-5xl',
}

export function Modal({
  children,
  title,
  onClose,
  maxWidth = 'md',
  zIndex = 'z-[100]',
  showClose = true,
}: ModalProps) {
  return (
    <ModalPortal>
      <div
        className={`fixed inset-0 ${zIndex} flex items-center justify-center p-4 bg-black/20 dark:bg-black/60`}
        onClick={(e) => e.target === e.currentTarget && onClose()}
      >
      <div className={`w-full ${maxWidthClasses[maxWidth]} max-h-[90vh] flex flex-col overflow-hidden rounded-2xl border border-card-border bg-surface-light shadow-xl`}>
        {(title || showClose) && (
          <div className="shrink-0 flex items-center justify-between px-6 py-4 border-b border-card-border">
            <div className="text-xl font-bold text-text-primary">{title}</div>
            {showClose && (
              <button
                type="button"
                onClick={onClose}
                className="p-1.5 rounded-lg text-text-muted hover:text-text-primary hover:bg-card transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            )}
          </div>
        )}
        <div className="flex-1 min-h-0 overflow-y-auto p-6">{children}</div>
      </div>
      </div>
    </ModalPortal>
  )
}
