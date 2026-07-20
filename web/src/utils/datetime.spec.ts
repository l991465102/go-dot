import { describe, expect, it } from 'vitest'
import { DISPLAY_TIMEZONE, formatShanghaiDateTime } from './datetime'

describe('formatShanghaiDateTime', () => {
  it('formats UTC in fixed Asia/Shanghai time', () => {
    expect(DISPLAY_TIMEZONE).toBe('Asia/Shanghai')
    expect(formatShanghaiDateTime('2026-07-20T02:51:03Z')).toBe('2026-07-20 10:51:03')
  })

  it('returns a dash for missing or invalid values', () => {
    expect(formatShanghaiDateTime('')).toBe('-')
    expect(formatShanghaiDateTime('not-a-date')).toBe('-')
    expect(formatShanghaiDateTime(undefined)).toBe('-')
  })
})
