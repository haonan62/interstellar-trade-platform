import { describe, expect, it } from 'vitest'
import { formatAmount, formatRelative, humanize, initials, shortId } from './format'

describe('format', () => {
  it('formats amounts and placeholders', () => {
    expect(formatAmount(null)).toBe('—')
    expect(formatAmount(1234)).toMatch(/1.?234/)
    expect(formatAmount('abc')).toBe('abc')
  })

  it('humanizes identifiers', () => {
    expect(humanize('offer_received')).toBe('Offer received')
    expect(humanize('')).toBe('')
  })

  it('shortens prefixed ids', () => {
    expect(shortId('col_0123456789abcdef')).toBe('col_01234567…')
    expect(shortId('abc')).toBe('abc')
    expect(shortId('')).toBe('—')
  })

  it('builds initials', () => {
    expect(initials('Jane Doe')).toBe('JD')
    expect(initials('root')).toBe('R')
    expect(initials('')).toBe('?')
  })

  it('formats relative times', () => {
    const now = Date.parse('2026-01-01T12:00:00Z')
    expect(formatRelative('2026-01-01T11:00:00Z', now)).toMatch(/hour/)
    expect(formatRelative('', now)).toBe('—')
  })
})
