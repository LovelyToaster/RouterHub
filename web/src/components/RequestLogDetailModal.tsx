import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Copy } from 'lucide-react'
import { Modal } from '@/components/ui/Modal'
import { Badge } from '@/components/ui/Badge'
import { useUserTimezone } from '@/hooks/useUserTimezone'
import { formatCompact, formatMs, prettyJson } from '@/lib/format'
import type { RequestLog } from '@/types'

function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation()
  switch (status) {
    case 'success':
      return <Badge variant="success">{t('requests.statusSuccess')}</Badge>
    case 'failed':
    case 'error':
      return <Badge variant="danger">{t('requests.statusFailed')}</Badge>
    case 'pending':
      return <Badge variant="warning">{t('requests.statusPending')}</Badge>
    default:
      return <Badge>{status}</Badge>
  }
}

function MetaRow({ label, value }: { label: string; value: React.ReactNode }) {
  if (value == null || value === '') return null
  return (
    <div className="flex justify-between gap-4 py-1">
      <span className="text-text-secondary shrink-0">{label}</span>
      <span className="text-text-primary text-right break-all">{value}</span>
    </div>
  )
}

function BodyCard({
  title,
  body,
}: {
  title: string
  body: string | undefined
}) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  if (!body) return null

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(prettyJson(body))
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="rounded-xl border border-card-border bg-surface p-4">
      <div className="flex items-center justify-between mb-2">
        <h4 className="text-sm font-semibold text-text-primary">{title}</h4>
        <button
          type="button"
          onClick={handleCopy}
          className="inline-flex items-center gap-1.5 text-xs text-text-secondary hover:text-text-primary transition-colors"
        >
          <Copy className="w-3.5 h-3.5" />
          {copied ? t('requests.copied') : t('requests.copy')}
        </button>
      </div>
      <pre className="overflow-auto max-h-80 text-xs text-text-primary bg-surface-light rounded-lg p-3 border border-card-border">
        {prettyJson(body)}
      </pre>
    </div>
  )
}

export default function RequestLogDetailModal({
  log,
  onClose,
}: {
  log: RequestLog
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const { tz } = useUserTimezone()

  const dateLocale = i18n.language === 'zh-CN' ? 'zh-CN' : 'en-US'
  const formatTime = (iso: string) =>
    new Date(iso).toLocaleString(dateLocale, {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      timeZone: tz,
    })

  return (
    <Modal title={t('requests.detailTitle')} onClose={onClose} maxWidth="xl">
      <div className="space-y-4">
        {/* Overview */}
        <div className="rounded-xl border border-card-border bg-surface p-4">
          <h4 className="text-sm font-semibold text-text-primary mb-2">
            {t('requests.overview')}
          </h4>
          <div className="space-y-1 text-sm">
            <MetaRow label={t('requests.status')} value={<StatusBadge status={log.status} />} />
            <MetaRow
              label={t('requests.httpStatus')}
              value={log.http_status != null ? String(log.http_status) : undefined}
            />
            <MetaRow label={t('requests.reason')} value={log.error_message} />
            <MetaRow label={t('requests.thModel')} value={log.requested_model} />
            <MetaRow label={t('requests.thProvider')} value={log.provider_name} />
            <MetaRow label={t('requests.thApiFormat')} value={log.inbound_protocol} />
            <MetaRow label={t('requests.thClientIp')} value={log.client_ip} />
            <MetaRow label={t('requests.thApiKey')} value={log.gateway_api_key_name} />
            <MetaRow label={t('requests.thTime')} value={formatTime(log.created_at)} />
          </div>
        </div>

        {/* Tokens */}
        <div className="rounded-xl border border-card-border bg-surface p-4">
          <h4 className="text-sm font-semibold text-text-primary mb-2">
            {t('requests.tokens')}
          </h4>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm">
            <div>
              <div className="text-text-secondary">{t('requests.inputTokens')}</div>
              <div className="text-text-primary">{formatCompact(log.input_tokens)}</div>
            </div>
            <div>
              <div className="text-text-secondary">{t('requests.outputTokens')}</div>
              <div className="text-text-primary">{formatCompact(log.output_tokens)}</div>
            </div>
            <div>
              <div className="text-text-secondary">{t('requests.cacheRead')}</div>
              <div className="text-text-primary">{formatCompact(log.cached_tokens)}</div>
            </div>
            <div>
              <div className="text-text-secondary">{t('requests.cacheWrite')}</div>
              <div className="text-text-primary">{formatCompact(log.cache_write_tokens)}</div>
            </div>
            <div>
              <div className="text-text-secondary">{t('requests.totalTokens')}</div>
              <div className="text-text-primary">{formatCompact(log.total_tokens)}</div>
            </div>
          </div>
        </div>

        {/* Timing */}
        <div className="rounded-xl border border-card-border bg-surface p-4">
          <h4 className="text-sm font-semibold text-text-primary mb-2">
            {t('requests.timing')}
          </h4>
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <div className="text-text-secondary">{t('requests.ttft')}</div>
              <div className="text-text-primary">
                {log.time_to_first_token_ms != null
                  ? formatMs(log.time_to_first_token_ms)
                  : t('requests.notRecorded')}
              </div>
            </div>
            <div>
              <div className="text-text-secondary">{t('requests.totalDuration')}</div>
              <div className="text-text-primary">
                {log.total_duration_ms != null
                  ? formatMs(log.total_duration_ms)
                  : t('requests.notRecorded')}
              </div>
            </div>
          </div>
        </div>

        {/* Request body */}
        <BodyCard title={t('requests.requestBody')} body={log.request_body} />

        {/* Response body */}
        <BodyCard title={t('requests.responseBody')} body={log.response_body} />
      </div>
    </Modal>
  )
}
