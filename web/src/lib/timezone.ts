// Timezone utilities.
//
// Provides browser detection, offset formatting, and the master list of
// selectable timezones for the settings dropdown. The dropdown label is
// always "(UTC±HH:MM) IANA/Name".

/**
 * getBrowserTimezone returns the IANA timezone reported by the browser,
 * falling back to "Asia/Shanghai" only if the platform cannot provide one
 * (extremely rare on modern browsers).
 */
export function getBrowserTimezone(): string {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone
    if (tz) return tz
  } catch {
    // ignore
  }
  return 'Asia/Shanghai'
}

/**
 * formatOffset returns the current UTC offset for an IANA timezone as
 * "UTC+08:00" / "UTC-05:00" / "UTC+00:00".
 */
export function formatOffset(timeZone: string, at: Date = new Date()): string {
  try {
    // "shortOffset" gives strings like "GMT+8" or "GMT-05:30".
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone,
      timeZoneName: 'shortOffset',
    }).formatToParts(at)
    const raw = parts.find((p) => p.type === 'timeZoneName')?.value ?? ''
    // raw is one of: "GMT", "GMT+8", "GMT-5", "GMT+05:30", "UTC" ...
    let s = raw.replace(/^GMT/, '').replace(/^UTC/, '')
    if (s === '') return 'UTC+00:00'
    const sign = s[0] === '-' ? '-' : '+'
    s = s.replace(/^[+-]/, '')
    let hours = '00'
    let minutes = '00'
    if (s.includes(':')) {
      const [h, m] = s.split(':')
      hours = h.padStart(2, '0')
      minutes = (m ?? '00').padStart(2, '0')
    } else {
      hours = s.padStart(2, '0')
    }
    return `UTC${sign}${hours}:${minutes}`
  } catch {
    return 'UTC+00:00'
  }
}

/** formatTimezoneOption produces "(UTC+08:00) Asia/Shanghai". */
export function formatTimezoneOption(timeZone: string): string {
  return `(${formatOffset(timeZone)}) ${timeZone}`
}

/**
 * TIMEZONE_OPTIONS is the curated list of IANA zones shown in the settings
 * dropdown. It intentionally excludes the bare "UTC" because we always want
 * users to pick a real region.
 */
export const TIMEZONE_OPTIONS: string[] = [
  'Pacific/Auckland',
  'Australia/Sydney',
  'Asia/Tokyo',
  'Asia/Shanghai',
  'Asia/Singapore',
  'Asia/Bangkok',
  'Asia/Kolkata',
  'Asia/Dubai',
  'Europe/Moscow',
  'Europe/Berlin',
  'Europe/Paris',
  'Europe/London',
  'Atlantic/Reykjavik',
  'America/Sao_Paulo',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'Pacific/Honolulu',
]
