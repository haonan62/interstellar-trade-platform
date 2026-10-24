const numberFmt = new Intl.NumberFormat(undefined)
const dateFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
const relFmt = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

export function formatAmount(value) {
  if (value === null || value === undefined || value === '') return '—'
  const n = Number(value)
  return Number.isFinite(n) ? numberFmt.format(n) : String(value)
}

export function formatDateTime(value) {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? String(value) : dateFmt.format(d)
}

const UNITS = [
  ['year', 365 * 24 * 3600],
  ['month', 30 * 24 * 3600],
  ['day', 24 * 3600],
  ['hour', 3600],
  ['minute', 60],
  ['second', 1],
]

export function formatRelative(value, now = Date.now()) {
  if (!value) return '—'
  const t = new Date(value).getTime()
  if (Number.isNaN(t)) return String(value)
  const diff = Math.round((t - now) / 1000)
  for (const [unit, secs] of UNITS) {
    if (Math.abs(diff) >= secs || unit === 'second') {
      return relFmt.format(Math.round(diff / secs), unit)
    }
  }
  return ''
}

// humanize turns snake_case identifiers into "Sentence case".
export function humanize(value) {
  if (!value) return ''
  const s = String(value).replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function shortId(value, n = 8) {
  if (!value) return '—'
  const s = String(value)
  const i = s.indexOf('_')
  const body = i >= 0 ? s.slice(i + 1) : s
  return body.length > n ? `${s.slice(0, i + 1)}${body.slice(0, n)}…` : s
}

export function initials(name) {
  const parts = String(name || '?').trim().split(/\s+/)
  return ((parts[0]?.[0] ?? '?') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase()
}
