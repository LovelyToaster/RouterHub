import { type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { Modal } from '@/components/ui/Modal'
import { Button } from '@/components/ui/Button'

interface ConfirmDialogProps {
  title: string
  message: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  onConfirm: () => void
  onCancel: () => void
  loading?: boolean
  danger?: boolean
}

export function ConfirmDialog({
  title,
  message,
  confirmLabel,
  cancelLabel,
  onConfirm,
  onCancel,
  loading = false,
  danger = false,
}: ConfirmDialogProps) {
  const { t } = useTranslation()

  return (
    <Modal title={title} onClose={onCancel} maxWidth="sm">
      <div className="space-y-5">
        <div className="flex items-start gap-3">
          {danger && (
            <div className="p-2 rounded-xl bg-red-500/15 shrink-0">
              <AlertTriangle className="w-5 h-5 text-red-500" />
            </div>
          )}
          <p className="text-sm text-text-secondary leading-relaxed">{message}</p>
        </div>

        <div className="flex justify-end gap-3 pt-2">
          <Button variant="secondary" onClick={onCancel} type="button">
            {cancelLabel ?? t('common.cancel')}
          </Button>
          <Button
            variant={danger ? 'danger' : 'primary'}
            onClick={onConfirm}
            loading={loading}
          >
            {confirmLabel ?? (danger ? t('common.delete') : t('common.confirm'))}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
