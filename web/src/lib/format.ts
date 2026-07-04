// Number & duration formatting helpers shared across pages.
//
// Keep behaviour identical to the original DashboardPage helpers.

function stripZeros(s: string): string {
  return s.replace(/\.?0+$/, '')
}

function fmt2(n: number): string {
  return stripZeros(n.toFixed(2))
}

/**
 * formatCompact renders a number using human-friendly K/M/B/T suffixes.
 * < 1000 keeps integer rendering; larger values are compressed to two decimals
 * with trailing zeros stripped.
 */
export function formatCompact(n: number): string {
  if (!Number.isFinite(n)) return '0'
  const abs = Math.abs(n)
  if (abs < 1000) return String(Math.round(n))
  if (abs < 1e6) return `${fmt2(n / 1e3)}K`
  if (abs < 1e9) return `${fmt2(n / 1e6)}M`
  if (abs < 1e12) return `${fmt2(n / 1e9)}B`
  return `${fmt2(n / 1e12)}T`
}

/** formatPercent multiplies by 100 and appends %. */
export function formatPercent(v: number): string {
  return `${fmt2(v * 100)}%`
}

/**
 * formatMs auto-scales a millisecond value to ms / s / m / h.
 */
export function formatMs(v: number): string {
  if (!Number.isFinite(v)) return '0ms'
  const abs = Math.abs(v)
  if (abs < 1000) return `${fmt2(v)}ms`
  if (abs < 60_000) return `${fmt2(v / 1000)}s`
  if (abs < 3_600_000) return `${fmt2(v / 60_000)}m`
  return `${fmt2(v / 3_600_000)}h`
}
