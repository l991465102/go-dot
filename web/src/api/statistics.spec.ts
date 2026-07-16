import { afterEach, describe, expect, it, vi } from 'vitest'
import { completedDayRange } from './statistics'

describe('completedDayRange', () => {
  afterEach(() => vi.useRealTimers())

  it('subtracts a local calendar day across the spring DST transition', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-09T12:00:00Z'))

    expect(completedDayRange(1, 'America/Los_Angeles')).toEqual({
      start: '2026-03-08T08:00:00.000Z',
      end: '2026-03-09T07:00:00.000Z',
    })
  })

  it('subtracts a local calendar day across the fall DST transition', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-02T12:00:00Z'))

    expect(completedDayRange(1, 'America/Los_Angeles')).toEqual({
      start: '2026-11-01T07:00:00.000Z',
      end: '2026-11-02T08:00:00.000Z',
    })
  })
})
