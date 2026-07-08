import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Search, ChevronLeft, ChevronRight } from 'lucide-react'
import { GlassCard } from '@/components/ui/GlassCard'
import { Badge } from '@/components/ui/Badge'
import { Select } from '@/components/ui/Select'
import { listRequestLogs } from '@/api/client'
import { useUserTimezone } from '@/hooks/useUserTimezone'
import { useRequestLogsStream } from '@/hooks/useRequestLogsStream'
import { formatCompact, formatMs, formatPercent } from '@/lib/format'
import type { RequestLog } from '@/types'

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const

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

function Cell({
  main,
  sub,
  align = 'left',
}: {
  main: React.ReactNode
  sub?: React.ReactNode
  align?: 'left' | 'right'
}) {
  return (
    <div className={`flex flex-col ${align === 'right' ? 'items-end' : 'items-start'}`}>
      <span className="text-text-primary">{main}</span>
      {sub != null && <span className="text-xs text-text-secondary mt-0.5">{sub}</span>}
    </div>
  )
}

export function RequestsPage() {
  const { t, i18n } = useTranslation()
  const [pageSize, setPageSize] = useState<number>(20)
  const [offset, setOffset] = useState(0)
  const [search, setSearch] = useState('')
  const { tz } = useUserTimezone()

  const { data: logs, isLoading, refetch } = useQuery<RequestLog[]>({
    queryKey: ['request-logs', pageSize, offset],
    queryFn: () => listRequestLogs(pageSize, offset),
  })

  // Live updates: the backend publishes a lightweight event whenever a
  // request log row is inserted (pending) or updated (success/error).
  useRequestLogsStream(() => {
    refetch()
  })

  const filteredLogs = logs?.filter((log) => {
    if (!search) return true
    const q = search.toLowerCase()
    return (
      log.requested_model.toLowerCase().includes(q) ||
      log.actual_model.toLowerCase().includes(q) ||
      log.provider_name.toLowerCase().includes(q) ||
      log.request_id.toLowerCase().includes(q) ||
      (log.client_ip ?? '').toLowerCase().includes(q) ||
      (log.gateway_api_key_name ?? '').toLowerCase().includes(q)
    )
  })

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

  const handlePageSizeChange = (n: number) => {
    setPageSize(n)
    setOffset(0)
  }

  return (
    <div className="flex flex-col h-[calc(100dvh-5rem)] md:h-[calc(100dvh-6rem)] lg:h-[calc(100dvh-4rem)]">
      <div className="flex items-center justify-between mb-4 shrink-0">
        <h1 className="text-2xl font-bold text-text-primary">{t('requests.title')}</h1>
      </div>

      <GlassCard className="flex-1 flex flex-col min-h-0 overflow-hidden">
        {/* Toolbar */}
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 p-4 border-b border-card-border shrink-0">
          <div className="relative flex-1 max-w-sm w-full">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
            <input
              type="text"
              placeholder={t('requests.searchPlaceholder')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full pl-10 pr-4 py-2 rounded-xl bg-surface-light border border-surface-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent transition-all"
            />
          </div>

          <div className="flex items-center gap-3 text-sm text-text-secondary">
            {/* Page size */}
            <div className="flex items-center gap-2">
              <span className="text-text-muted">{t('requests.pageSize')}</span>
              <Select
                size="sm"
                value={String(pageSize)}
                onChange={(e) => handlePageSizeChange(Number(e.target.value))}
                options={PAGE_SIZE_OPTIONS.map((n) => ({ value: String(n), label: String(n) }))}
                className="w-20"
              />
            </div>

            {/* Pagination */}
            <div className="flex items-center gap-1">
              <button
                onClick={() => setOffset(Math.max(0, offset - pageSize))}
                disabled={offset === 0}
                className="p-1.5 rounded-lg hover:bg-card disabled:opacity-30 transition-colors"
              >
                <ChevronLeft className="w-4 h-4" />
              </button>
              <span>{t('requests.pageInfo', { page: offset / pageSize + 1 })}</span>
              <button
                onClick={() => setOffset(offset + pageSize)}
                disabled={!logs || logs.length < pageSize}
                className="p-1.5 rounded-lg hover:bg-card disabled:opacity-30 transition-colors"
              >
                <ChevronRight className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>

        {/* Scrollable table area */}
        <div className="flex-1 min-h-0 overflow-auto">
          {isLoading ? (
            <div className="p-8 text-center text-text-muted animate-pulse">
              {t('requests.loading')}
            </div>
          ) : filteredLogs && filteredLogs.length > 0 ? (
            <table className="w-full text-sm whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-surface-light">
                <tr className="border-b border-card-border text-text-secondary">
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thTime')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thId')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thModel')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thApiFormat')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thStream')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thClientIp')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thProvider')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thApiKey')}</th>
                  <th className="text-left px-3 py-3 font-medium">{t('requests.thStatus')}</th>
                  <th className="text-right px-3 py-3 font-medium">{t('requests.thTokens')}</th>
                  <th className="text-right px-3 py-3 font-medium">{t('requests.thCacheRead')}</th>
                  <th className="text-right px-3 py-3 font-medium">{t('requests.thCacheWrite')}</th>
                  <th className="text-right px-3 py-3 font-medium">{t('requests.thDuration')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredLogs.map((log) => {
                  const hitRate =
                    log.input_tokens > 0 ? log.cached_tokens / log.input_tokens : null
                  const dur = log.total_duration_ms
                  const ttft = log.time_to_first_token_ms
                  return (
                    <tr
                      key={log.id}
                      className="border-b border-card-border hover:bg-card transition-colors"
                    >
                      <td className="px-3 py-3 text-text-secondary">{formatTime(log.created_at)}</td>
                      <td className="px-3 py-3 text-text-muted">{log.id}</td>
                      <td className="px-3 py-3">
                        <Cell
                          main={log.requested_model}
                          sub={
                            log.actual_model !== log.requested_model
                              ? `→ ${log.actual_model}`
                              : undefined
                          }
                        />
                      </td>
                      <td className="px-3 py-3 text-text-secondary">{log.provider_type}</td>
                      <td className="px-3 py-3 text-text-secondary">
                        {log.stream ? t('requests.streamYes') : t('requests.streamNo')}
                      </td>
                      <td className="px-3 py-3 text-text-secondary">
                        {log.client_ip || '—'}
                      </td>
                      <td className="px-3 py-3 text-text-primary">{log.provider_name}</td>
                      <td className="px-3 py-3 text-text-secondary">
                        {log.gateway_api_key_name || '—'}
                      </td>
                      <td className="px-3 py-3">
                        <StatusBadge status={log.status} />
                      </td>
                      <td className="px-3 py-3 text-right">
                        <Cell
                          align="right"
                          main={formatCompact(log.total_tokens)}
                          sub={`${t('requests.thInput')}: ${formatCompact(log.input_tokens)} / ${t('requests.thOutput')}: ${formatCompact(log.output_tokens)}`}
                        />
                      </td>
                      <td className="px-3 py-3 text-right">
                        <Cell
                          align="right"
                          main={formatCompact(log.cached_tokens)}
                          sub={
                            hitRate == null
                              ? '—'
                              : `${t('requests.hitRate')}: ${formatPercent(hitRate)}`
                          }
                        />
                      </td>
                      <td className="px-3 py-3 text-right text-text-primary">
                        {formatCompact(log.cache_write_tokens)}
                      </td>
                      <td className="px-3 py-3 text-right">
                        <Cell
                          align="right"
                          main={dur != null ? formatMs(dur) : '—'}
                          sub={
                            ttft != null
                              ? `${t('requests.subTtft')} ${formatMs(ttft)}`
                              : undefined
                          }
                        />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          ) : (
            <div className="p-8 text-center text-text-muted">
              <p>{t('requests.noLogs')}</p>
            </div>
          )}
        </div>
      </GlassCard>
    </div>
  )
}
