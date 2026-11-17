/** 为新事件生成 UUIDv7；依赖浏览器安全随机数。 */
export function uuidv7() {
  const b = crypto.getRandomValues(new Uint8Array(16)); let ms = Date.now()
  for (let i = 5; i >= 0; i--) { b[i] = ms % 256; ms = Math.floor(ms / 256) }
  b[6] = (b[6] & 15) | 0x70; b[8] = (b[8] & 63) | 0x80
  const h = Array.from(b, x => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

/** 批量上报客户端；失败的批次保留在内存队列中。 */
export class Client {
  constructor({ baseURL, writeKey, test = false, maxRetries = 2, retryBackoff = 200, fetchImpl = globalThis.fetch, maxQueueSize = 1000 }) {
    const url = new URL(baseURL, globalThis.location?.href)
    if (!['http:', 'https:'].includes(url.protocol) || url.search || url.hash || url.username || url.password || !Number.isFinite(retryBackoff) || retryBackoff < 0 || !writeKey || !Number.isInteger(maxRetries) || maxRetries < 0 || maxRetries > 10 || !Number.isInteger(maxQueueSize) || maxQueueSize < 1 || typeof fetchImpl !== 'function') throw new Error('go-dot: invalid options')
    url.pathname = url.pathname.replace(/\/$/, '') + (test ? '/api/v1/collect/test-events' : '/api/v1/collect/events')
    this.url = url.href; this.key = writeKey; this.fetch = fetchImpl; this.maxRetries = maxRetries; this.backoff = retryBackoff; this.maxQueueSize = maxQueueSize; this.queue = []; this.flushing = null
  }
  /** 将事件放入内存队列并返回事件 ID。 */
  track(input) {
    if (this.queue.length >= this.maxQueueSize) throw new Error('go-dot: queue full')
    const event = structuredClone({ ...input, event_id: input.event_id || uuidv7(), event_time: input.event_time || Date.now() })
    this.queue.push(event); return event.event_id
  }
  /** 返回待发送事件快照，供调用方在需要时持久化。 */
  pending() { return structuredClone(this.queue) }
  /** 分批发送；返回所有已处理事件的结果，拒绝事件不会自动重试。 */
  flush({ signal } = {}) {
    if (this.flushing) return this.flushing
    this.flushing = this._flush(signal).finally(() => { this.flushing = null }); return this.flushing
  }
  async _flush(signal) {
    const results = []; let remaining = this.queue.length, offset = 0
    try { while (remaining > 0) {
      const batch = this.queue.slice(0, Math.min(100, remaining)); const body = JSON.stringify({ events: batch }); let response
      for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
        if (attempt) await wait(this.backoff * 2 ** (attempt - 1), signal)
        try { response = await this.fetch(this.url, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Write-Key': this.key }, body, signal, redirect: 'error' }) }
        catch (err) { if (signal?.aborted || attempt === this.maxRetries) throw err; continue }
        if (response.status === 202) break
        const status = response.status
        if ((status !== 429 && status < 500) || attempt === this.maxRetries) throw new Error(`go-dot: HTTP ${status}`)
      }
      if (!response || response.status !== 202) throw new Error('go-dot: request failed')
      const data = await response.json(); validate(data, batch)
      this.queue.splice(0, batch.length); results.push(...data.results.map(item => ({ ...item, index: item.index + offset }))); remaining -= batch.length; offset += batch.length
    } } catch (error) { if (error && typeof error === 'object') error.completed = results; throw error }
    return results
  }
}
function validate(data, batch) {
  if (!Array.isArray(data.results) || data.results.length !== batch.length || data.accepted + data.rejected !== batch.length) throw new Error('go-dot: malformed response')
  const seen = new Set(); let accepted = 0, rejected = 0
  for (const item of data.results) {
    if (!Number.isInteger(item.index) || item.index < 0 || item.index >= batch.length || seen.has(item.index) || item.event_id !== batch[item.index].event_id) throw new Error('go-dot: malformed event result')
    seen.add(item.index); if (item.status === 'accepted') accepted++; else if (item.status === 'rejected') rejected++; else throw new Error('go-dot: malformed event status')
  }
  if (accepted !== data.accepted || rejected !== data.rejected) throw new Error('go-dot: malformed response counts')
}
function wait(ms, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) { reject(signal.reason); return }
    const abort = () => { clearTimeout(timer); reject(signal.reason) }
    const timer = setTimeout(() => { signal?.removeEventListener('abort', abort); resolve() }, ms)
    signal?.addEventListener('abort', abort, { once: true })
  })
}
