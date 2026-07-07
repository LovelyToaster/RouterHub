import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Activity,
  XCircle,
  Zap,
  ArrowUp,
  ArrowDown,
  Minus,
} from 'lucide-react'
import { GlassCard } from '@/components/ui/GlassCard'
import { RequestsTrendChart, TokensTrendChart } from '@/components/dashboard/TrendCharts'
import { useStatsSummaryStream } from '@/hooks/useStatsSummaryStream'
import { fmt2, formatCompact, formatMs, formatPercent } from '@/lib/format'
import type { StatsSummary, RangeKey } from '@/types'

// deltaPercent returns a Δ ratio: (cur - prev) / prev, or null when undefined.
function deltaPercent(cur: number, prev: number): number | null {
  if (!Number.isFinite(cur) || !Number.isFinite(prev)) return null
  if (prev <= 0) return null
  return (cur - prev) / prev
}

const RANGE_ORDER: RangeKey[] = ['all', 'month', 'week', 'day']

function DashboardSkeleton() {
  return (
    <div className="space-y-6 animate-pulse">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {[...Array(2)].map((_, i) => (
          <div key={i} className="h-40 rounded-2xl bg-card" />
        ))}
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="h-80 rounded-2xl bg-card" />
        <div className="h-80 rounded-2xl bg-card" />
        <div className="h-80 rounded-2xl bg-card" />
        <div className="h-80 rounded-2xl bg-card" />
      </div>
    </div>
  )
}

// ─── Shared UI ───────────────────────────────────────────────────────────────

function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
}) {
  return (
    <div className="inline-flex items-center rounded-lg bg-card p-0.5 border border-card-border">
      {options.map((opt) => {
        const active = opt.value === value
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => onChange(opt.value)}
            className={
              active
                ? 'px-2.5 py-1 text-xs rounded-md bg-accent text-white transition-colors'
                : 'px-2.5 py-1 text-xs rounded-md text-text-muted hover:text-text-primary transition-colors'
            }
          >
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}

// ─── Info cards ──────────────────────────────────────────────────────────────

function Metric({
  label,
  value,
  valueColor,
  icon: Icon,
  align = 'left',
}: {
  label: string
  value: string
  valueColor?: string
  icon?: any
  align?: 'left' | 'right'
}) {
  const alignClass = align === 'right' ? 'text-right' : 'text-left'
  const rowJustify = align === 'right' ? 'justify-end' : 'justify-start'
  return (
    <div className={`min-w-0 ${alignClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide">{label}</p>
      <div className={`flex items-baseline gap-1.5 mt-1 ${rowJustify}`}>
        {Icon && <Icon className="w-5 h-5 shrink-0 self-center" style={valueColor ? { color: valueColor } : undefined} />}
        <p
          className="text-2xl font-bold tabular-nums truncate"
          style={{ color: valueColor || 'var(--color-text-primary)' }}
        >
          {value}
        </p>
      </div>
    </div>
  )
}

function CardHeader({
  title,
  icon: Icon,
  color,
}: {
  title: string
  icon: any
  color: string
}) {
  return (
    <div className="flex items-center justify-between mb-4">
      <h3 className="text-base font-semibold text-text-primary">{title}</h3>
      <div className="p-2 rounded-xl" style={{ backgroundColor: `${color}20` }}>
        <Icon className="w-5 h-5" style={{ color }} />
      </div>
    </div>
  )
}

// Build "副标"：全部模式显示日均，其它模式显示环比 Δ%。

// deltaLabelForRange returns the i18n key used to label the range's delta metric.
// Only meaningful when range is day/week/month (all is handled separately via daily average).
function deltaLabelForRange(range: RangeKey): string {
  switch (range) {
    case 'day':
      return 'dashboard.deltaVsYesterday'
    case 'week':
      return 'dashboard.deltaVsLastWeek'
    case 'month':
      return 'dashboard.deltaVsLastMonth'
    default:
      return 'dashboard.deltaVsYesterday' // unreachable
  }
}

// Build side metric data for range-aware comparison.
// - all: daily average (raw value / active_days)
// - other: Δ% vs previous window (colored, with arrow)
function sideMetric(
  stats: StatsSummary,
  cur: number,
  prev: number,
  t: any,
): { label: string; value: string; color?: string; icon?: any } | null {
  if (stats.range === 'all') {
    const days = Math.max(stats.active_days, 1)
    return {
      label: t('dashboard.dailyAverage'),
      value: formatCompact(cur / days),
    }
  }
  if (!stats.has_previous_window) return null
  const deltaLabelKey = deltaLabelForRange(stats.range)
  const delta = deltaPercent(cur, prev)
  if (delta === null) {
    return {
      label: t(deltaLabelKey),
      value: '—',
    }
  }
  const pct = Math.abs(delta) * 100
  if (pct < 0.005) {
    return {
      label: t(deltaLabelKey),
      value: `${fmt2(0)}%`,
      icon: Minus,
    }
  }
  const up = delta > 0
  return {
    label: t(deltaLabelKey),
    value: `${fmt2(pct)}%`,
    // Chinese stock-market convention: up = red, down = green.
    color: up ? '#ef4444' : '#10b981',
    icon: up ? ArrowUp : ArrowDown,
  }
}

// successCountMetric returns the "success/total" summary displayed in the
// requests-overview card.
function healthSideMetric(
  stats: StatsSummary,
  t: any,
): { label: string; value: string } {
  return {
    label: t('dashboard.successCountLabel'),
    value: `${formatCompact(stats.current.successful_requests)} / ${formatCompact(stats.current.requests)}`,
  }
}

// RequestsOverviewCard merges the old Requests + Health cards into a single
// wider card, laying out four metrics in a row on md+ (2×2 on small screens):
//   [total requests] [Δ vs previous] [success rate] [success/total]
function RequestsOverviewCard({ stats, t }: { stats: StatsSummary; t: any }) {
  const cur = stats.current.requests
  const prev = stats.previous.requests
  const side = sideMetric(stats, cur, prev, t)
  const successRate =
    stats.current.requests > 0
      ? stats.current.successful_requests / stats.current.requests
      : null
  const successCount = healthSideMetric(stats, t)
  return (
    <GlassCard className="p-5">
      <CardHeader
        title={t('dashboard.requestsOverviewCard')}
        icon={Activity}
        color="#6366f1"
      />
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Metric label={t(`dashboard.range_${stats.range}`)} value={formatCompact(cur)} />
        <Metric
          label={side ? side.label : t(deltaLabelForRange(stats.range))}
          value={side ? side.value : '—'}
          valueColor={side?.color}
          icon={side?.icon}
        />
        <Metric
          label={t('dashboard.successRate')}
          value={successRate === null ? '—' : formatPercent(successRate)}
        />
        <Metric label={successCount.label} value={successCount.value} />
      </div>
    </GlassCard>
  )
}

function TokensCard({ stats, t }: { stats: StatsSummary; t: any }) {
  const hitRate =
    stats.current.input_tokens > 0
      ? stats.current.cached_tokens / stats.current.input_tokens
      : 0
  return (
    <GlassCard className="p-5">
      <CardHeader title={t('dashboard.tokensCard')} icon={Zap} color="#f59e0b" />
      <div className="grid grid-cols-4 gap-4">
        <Metric label={t('dashboard.inputTokens')} value={formatCompact(stats.current.input_tokens)} />
        <Metric label={t('dashboard.outputTokens')} value={formatCompact(stats.current.output_tokens)} />
        <Metric label={t('dashboard.cachedTokens')} value={formatCompact(stats.current.cached_tokens)} />
        <Metric label={t('dashboard.cacheHitRate')} value={formatPercent(hitRate)} />
      </div>
    </GlassCard>
  )
}

// ─── Distribution card ───────────────────────────────────────────────────────

function DistributionCard({ stats, t }: { stats: StatsSummary; t: any }) {
  const [dim, setDim] = useState<'model' | 'provider'>('model')
  const [metric, setMetric] = useState<'requests' | 'tokens'>('requests')

  const source =
    dim === 'model'
      ? metric === 'requests'
        ? stats.requests_by_model
        : stats.tokens_by_model
      : metric === 'requests'
        ? stats.requests_by_provider
        : stats.tokens_by_provider

  const entries = source
    ? Object.entries(source)
        .map(([name, count]) => ({ name, count }))
        .sort((a, b) => b.count - a.count)
    : []
  const totalAll = entries.reduce((sum, e) => sum + e.count, 0)
  const data = entries.slice(0, 10)

  const title =
    dim === 'model'
      ? metric === 'requests'
        ? t('dashboard.modelRequestsDistribution')
        : t('dashboard.modelTokensDistribution')
      : metric === 'requests'
        ? t('dashboard.providerRequestsDistribution')
        : t('dashboard.providerTokensDistribution')

  return (
    <GlassCard className="p-6">
      <div className="flex items-center justify-between mb-4 gap-3 flex-wrap">
        <h3 className="text-lg font-semibold text-text-primary">{title}</h3>
        <div className="flex items-center gap-2">
          <Segmented
            value={metric}
            onChange={setMetric}
            options={[
              { value: 'requests', label: t('dashboard.metricRequests') },
              { value: 'tokens', label: t('dashboard.metricTokens') },
            ]}
          />
          <Segmented
            value={dim}
            onChange={setDim}
            options={[
              { value: 'model', label: t('dashboard.dimModel') },
              { value: 'provider', label: t('dashboard.dimProvider') },
            ]}
          />
        </div>
      </div>
      <div className="h-72 overflow-y-auto pr-1">
        {data.length === 0 ? (
          <div className="h-full flex items-center justify-center text-text-muted text-sm">
            {t('dashboard.noData')}
          </div>
        ) : (
          <div className="space-y-3">
            {data.map((item, i) => {
              const pct = totalAll > 0 ? (item.count / totalAll) * 100 : 0
              return (
                <div key={item.name} className="flex items-center gap-3">
                  <span className="text-xs text-text-muted w-5 tabular-nums">{i + 1}</span>
                  <div className="flex-1 min-w-0">
                    <div className="flex justify-between mb-1 gap-3">
                      <span className="text-sm text-text-secondary truncate">{item.name}</span>
                      <span className="text-sm text-text-muted tabular-nums whitespace-nowrap">
                        {formatCompact(item.count)}{' '}
                        <span className="text-xs">({fmt2(pct)}%)</span>
                      </span>
                    </div>
                    <div className="h-1.5 rounded-full bg-surface-lighter overflow-hidden">
                      <div
                        style={{ width: `${pct}%` }}
                        className="h-full rounded-full bg-accent"
                      />
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </GlassCard>
  )
}

// ─── Performance card ────────────────────────────────────────────────────────

function PerformanceCard({ stats, t }: { stats: StatsSummary; t: any }) {
  const [dim, setDim] = useState<'model' | 'provider'>('model')
  const items = dim === 'model' ? stats.model_performance ?? [] : stats.provider_performance ?? []
  const title =
    dim === 'model' ? t('dashboard.modelPerformance') : t('dashboard.providerPerformance')

  return (
    <GlassCard className="p-6">
      <div className="flex items-center justify-between mb-4 gap-3 flex-wrap">
        <h3 className="text-lg font-semibold text-text-primary">{title}</h3>
        <Segmented
          value={dim}
          onChange={setDim}
          options={[
            { value: 'model', label: t('dashboard.dimModel') },
            { value: 'provider', label: t('dashboard.dimProvider') },
          ]}
        />
      </div>
      <div className="h-72">
        {items.length === 0 ? (
          <div className="h-full flex items-center justify-center text-text-muted text-sm">
            {t('dashboard.noData')}
          </div>
        ) : (
          <ul className="space-y-3">
            {items.map((item, i) => (
              <li key={item.model} className="flex items-start gap-3">
                <span className="text-xs text-text-muted w-5 mt-1 tabular-nums">{i + 1}</span>
                <div className="flex-1 min-w-0">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="text-sm text-text-primary truncate">{item.model}</span>
                    <span className="text-base font-semibold text-text-primary tabular-nums whitespace-nowrap">
                      {fmt2(item.tokens_per_second)}{' '}
                      <span className="text-xs font-normal text-text-muted">
                        {t('dashboard.tokensPerSec')}
                      </span>
                    </span>
                  </div>
                  <div className="flex items-baseline justify-between gap-3 mt-0.5">
                    <span className="text-xs text-text-muted">
                      {t('dashboard.ttft')} {formatMs(item.avg_ttft_ms)}
                    </span>
                    <span className="text-xs text-text-muted tabular-nums">
                      {formatCompact(item.sample_count)} {t('dashboard.samples')}
                    </span>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </GlassCard>
  )
}

// ─── Page ────────────────────────────────────────────────────────────────────

export function DashboardPage() {
  const { t } = useTranslation()
  const [range, setRange] = useState<RangeKey>('day')
  const { data: stats, error, pending } = useStatsSummaryStream(range)

  const rangeOptions = useMemo(
    () =>
      RANGE_ORDER.map((k) => ({
        value: k,
        label: t(`dashboard.range_${k}`),
      })),
    [t],
  )

  const bucketKind = stats?.bucket_kind ?? 'day'

  const header = (
    <div className="flex items-center justify-between gap-3 flex-wrap">
      <div>
        <h1 className="text-2xl font-bold text-text-primary">{t('dashboard.title')}</h1>
        <p className="text-text-secondary mt-1">{t('dashboard.subtitle')}</p>
      </div>
      <Segmented value={range} onChange={setRange} options={rangeOptions} />
    </div>
  )

  if (!stats && !error) {
    return (
      <div className="space-y-6">
        {header}
        <DashboardSkeleton />
      </div>
    )
  }

  if (error || !stats) {
    return (
      <div className="space-y-6">
        {header}
        <div className="flex items-center justify-center min-h-[300px]">
          <GlassCard className="p-8 text-center max-w-md">
            <XCircle className="w-12 h-12 mx-auto mb-3 text-red-400" />
            <h2 className="text-lg font-semibold text-text-primary mb-2">{t('dashboard.loadError')}</h2>
            <p className="text-text-secondary text-sm">
              {(error as any)?.message || t('dashboard.loadErrorDesc')}
            </p>
          </GlassCard>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {header}

      <div
        className={`space-y-6 transition-opacity duration-150 ${pending ? 'opacity-60' : 'opacity-100'}`}
      >
      {/* Info cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <RequestsOverviewCard stats={stats} t={t} />
        <TokensCard stats={stats} t={t} />
      </div>

      {/* Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Requests trend */}
        <GlassCard className="p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4">
            {t('dashboard.requestsTrend')}
          </h3>
          <div className="h-72">
            <RequestsTrendChart
              series={stats.series ?? []}
              bucketKind={bucketKind}
              label={t('dashboard.requestsCount')}
            />
          </div>
        </GlassCard>

        {/* Distribution (model/provider + requests/tokens toggle) */}
        <DistributionCard stats={stats} t={t} />

        {/* Token trend */}
        <GlassCard className="p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4">
            {t('dashboard.tokenTrend')}
          </h3>
          <div className="h-72">
            <TokensTrendChart
              series={stats.token_series ?? []}
              bucketKind={bucketKind}
              label={t('dashboard.tokenCount')}
            />
          </div>
        </GlassCard>

        {/* Performance TOP 5 */}
        <PerformanceCard stats={stats} t={t} />
      </div>
      </div>
    </div>
  )
}
