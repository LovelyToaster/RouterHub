import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import uPlot from 'uplot'
import UplotReact from 'uplot-react'
import { useTranslation } from 'react-i18next'
import { useAppearance } from '@/hooks/useAppearance'
import { formatCompact } from '@/lib/format'

export type BucketKind = 'hour' | 'day' | 'week' | 'month'

export interface TrendSeriesPoint {
  date: string
  count: number
}

interface TrendChartProps {
  series: TrendSeriesPoint[]
  bucketKind: BucketKind
  label: string
}

type ChartType = 'line' | 'bar'

interface TooltipState {
  x: number
  y: number
  label: string
  value: number
}

interface Palette {
  accent: string
  accentLight: string
  cardBorder: string
  textMuted: string
  surfaceLight: string
  textPrimary: string
}

// seriesTickFormatter is copied verbatim from DashboardPage so the chart owns
// its tick formatting without a cross-file refactor. Keep behaviour identical.
function seriesTickFormatter(bucket: BucketKind) {
  return (v: string) => {
    if (!v) return ''
    if (bucket === 'hour') {
      // "2026-07-04 15:04" -> "15:00"
      const parts = v.split(' ')
      if (parts.length === 2) {
        const [h] = parts[1].split(':')
        return `${h}:00`
      }
      return v
    }
    if (bucket === 'month') {
      // "2026-07" -> "2026-07"
      return v
    }
    // day / week: "2026-07-04" -> "07-04"
    return v.length >= 10 ? v.slice(5, 10) : v
  }
}

// PALETTES is a constant lookup table so charts read the correct theme colors
// immediately, without relying on DOM CSS variable reads (which lag behind the
// theme class being applied by useAppearance's effect).
const PALETTES = {
  dark: {
    accent: '#6366f1',
    accentLight: '#818cf8',
    cardBorder: 'rgba(255, 255, 255, 0.1)',
    textMuted: '#64748b',
    surfaceLight: '#1a1a24',
    textPrimary: '#f1f5f9',
  },
  light: {
    accent: '#6366f1',
    accentLight: '#818cf8',
    cardBorder: 'rgba(0, 0, 0, 0.1)',
    textMuted: '#94a3b8',
    surfaceLight: '#ffffff',
    textPrimary: '#1e293b',
  },
} as const

function TrendChartBase({ series, bucketKind, label, chartType }: TrendChartProps & { chartType: ChartType }) {
  const { resolvedTheme } = useAppearance()
  const { t } = useTranslation()

  // Palette is read directly from the constant table, indexed by the resolved
  // theme. This is a stable reference, so no useMemo is needed.
  const palette = PALETTES[resolvedTheme]

  // Columnar data: x is the bucket index 0..N-1, y is the count.
  // For bar charts we map zero to null so uPlot skips drawing a stub bar
  // (its 1px stroke otherwise appears as a tiny sliver at the baseline).
  // Line charts keep zero as-is so the line stays continuous.
  const { xs, ys, labels, hasAnyValue } = useMemo(() => {
    const n = series.length
    const xsArr: number[] = new Array(n)
    const ysArr: (number | null)[] = new Array(n)
    const labelsArr: string[] = new Array(n)
    let hasAny = false
    for (let i = 0; i < n; i++) {
      xsArr[i] = i
      const c = series[i].count
      if (c > 0) hasAny = true
      ysArr[i] = chartType === 'bar' && c === 0 ? null : c
      labelsArr[i] = series[i].date
    }
    return { xs: xsArr, ys: ysArr, labels: labelsArr, hasAnyValue: hasAny }
  }, [series, chartType])

  const data = useMemo<uPlot.AlignedData>(() => [xs, ys], [xs, ys])

  // Keep a ref to the latest labels so the cursor hook (stable) always reads
  // the current series without forcing options to be recreated.
  const labelsRef = useRef<string[]>(labels)
  labelsRef.current = labels

  const tickFormatter = useMemo(() => seriesTickFormatter(bucketKind), [bucketKind])

  // ── Responsive sizing via ResizeObserver ──────────────────────────────────
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [size, setSize] = useState<{ width: number; height: number }>({ width: 0, height: 0 })

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const apply = (width: number, height: number) =>
      setSize({ width: Math.floor(width), height: Math.floor(height) })
    const ro = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect
      if (rect) apply(rect.width, rect.height)
    })
    ro.observe(el)
    apply(el.clientWidth, el.clientHeight)
    return () => ro.disconnect()
  }, [])

  // ── Custom tooltip state ──────────────────────────────────────────────────
  const [tooltip, setTooltip] = useState<TooltipState | null>(null)

  const setCursorHook = useCallback((u: uPlot) => {
    const idx = u.cursor.idx
    if (idx == null || idx < 0) {
      setTooltip(null)
      return
    }
    const yValues = u.data[1] as (number | null)[]
    const rawY = yValues[idx]
    // For bar charts we substitute null for zero; treat null as 0 for display.
    const yv = rawY ?? 0
    const plotX = u.valToPos(idx, 'x')
    const plotY = u.valToPos(yv, 'y')
    const container = containerRef.current
    if (!container) return
    const rect = container.getBoundingClientRect()
    // Use u.rect (CSS pixels) instead of u.bbox (canvas pixels) so tooltip
    // stays aligned on HiDPI screens where devicePixelRatio > 1.
    const plotRect = u.rect
    const rawLeft = plotRect.left + plotX - rect.left + 12
    const rawTop = plotRect.top + plotY - rect.top
    const estWidth = 180
    const estHeight = 56
    const clampedLeft = Math.max(4, Math.min(rawLeft, rect.width - estWidth - 4))
    const clampedTop = Math.max(4, Math.min(rawTop, rect.height - estHeight - 4))
    setTooltip({ x: clampedLeft, y: clampedTop, label: labelsRef.current[idx] ?? '', value: yv })
  }, [])

  const seriesOpts: uPlot.Series[] = useMemo(
    () => [
      { label: 'x' },
      chartType === 'line'
        ? {
            label,
            stroke: palette.accent,
            width: 2,
            points: { show: false },
          }
        : {
            label,
            fill: '#8b5cf6',
            stroke: '#8b5cf6',
            paths: uPlot.paths.bars!({ size: [0.8, 40] }),
            points: { show: false },
          },
    ],
    [chartType, label, palette.accent],
  )

  const axesOpts: uPlot.Axis[] = useMemo(
    () => [
      {
        scale: 'x',
        stroke: palette.textMuted,
        grid: { stroke: palette.cardBorder, width: 1 },
        ticks: { stroke: palette.cardBorder, width: 1 },
        // Constrain splits to integer bucket indices; sparsify to ~8 ticks max
        // so labels don't overlap when the series is long.
        splits: ((_u: uPlot, _axisIdx: number, scaleMin: number, scaleMax: number) => {
          const maxIdx = labelsRef.current.length - 1
          const start = Math.max(0, Math.ceil(scaleMin))
          const end = Math.min(maxIdx, Math.floor(scaleMax))
          const total = end - start + 1
          if (total <= 0) return []
          const step = Math.max(1, Math.ceil(total / 8))
          const out: number[] = []
          for (let i = start; i <= end; i += step) out.push(i)
          return out
        }) as uPlot.Axis.Splits,
        values: (_u, splits) =>
          splits.map((s) => tickFormatter(labelsRef.current[Math.round(s)] ?? '')),
      },
      {
        scale: 'y',
        stroke: palette.textMuted,
        grid: { stroke: palette.cardBorder, width: 1 },
        ticks: { stroke: palette.cardBorder, width: 1 },
        values: (_u, splits) => splits.map((v) => formatCompact(v)),
      },
    ],
    [palette.textMuted, palette.cardBorder, tickFormatter],
  )

  const options = useMemo<uPlot.Options>(
    () => ({
      width: size.width,
      height: size.height,
      // Force y axis to start at 0 so line and bar charts share the same
      // baseline. hasAnyValue guards against the all-zero case at the parent
      // level (noData placeholder), so max is always > 0 here.
      scales: {
        x: { time: false },
        y: {
          range: (_u, _min, max) => [0, max],
        },
      },
      series: seriesOpts,
      axes: axesOpts,
      legend: { show: false },
      // Bar charts don't need a hover marker (the bar itself is the indicator);
      // line charts keep the default cursor point for easier value pinpointing.
      cursor: {
        drag: { x: false, y: false },
        points: chartType === 'bar' ? { show: false } : { size: 8 },
      },
      hooks: { setCursor: [setCursorHook] },
    }),
    // seriesOpts/axesOpts are derived from these primitives, so excluding them
    // here keeps the chart stable across pure data updates (SSE pushes).
    // resolvedTheme covers palette changes (palette = PALETTES[resolvedTheme]).
    [resolvedTheme, bucketKind, series.length, size.width, size.height, label, chartType, setCursorHook],
  )

  return (
    <div ref={containerRef} className="relative h-full w-full">
      {!hasAnyValue ? (
        <div className="flex h-full w-full items-center justify-center text-text-muted text-sm">
          {t('dashboard.noData')}
        </div>
      ) : (
        <>
          {size.width > 0 && size.height > 0 && <UplotReact options={options} data={data} />}
          {tooltip && (
            <div
              className="pointer-events-none absolute z-10 rounded-xl border border-card-border bg-surface-light px-3 py-2 text-sm shadow-lg"
              style={{ left: tooltip.x, top: tooltip.y }}
            >
              <div className="text-text-muted">{tooltip.label}</div>
              <div className="font-medium text-text-primary">
                {label}: {formatCompact(tooltip.value)}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export function RequestsTrendChart(props: TrendChartProps) {
  return <TrendChartBase {...props} chartType="line" />
}

export function TokensTrendChart(props: TrendChartProps) {
  return <TrendChartBase {...props} chartType="bar" />
}
