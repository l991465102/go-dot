export const DISPLAY_TIMEZONE = 'Asia/Shanghai'

const formatter = new Intl.DateTimeFormat('zh-CN', {
  timeZone: DISPLAY_TIMEZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
})

export function formatShanghaiDateTime(value: string | Date | null | undefined): string {
  if (!value) return '-'
  const parsed = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(parsed.getTime())) return '-'
  const parts = Object.fromEntries(formatter.formatToParts(parsed).map((part) => [part.type, part.value]))
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}
