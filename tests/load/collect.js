import http from 'k6/http'
import { check, sleep } from 'k6'
import { Rate, Trend } from 'k6/metrics'

export const acceptedRate = new Rate('accepted_rate')
export const rejectionRate = new Rate('rejection_rate')
export const queueLag = new Trend('queue_lag', true)
export const drainTime = new Trend('drain_time_ms', true)

export const options = {
  scenarios: {
    collect: {
      executor: 'ramping-arrival-rate',
      startRate: 2,
      timeUnit: '1s',
      preAllocatedVUs: 80,
      maxVUs: 400,
      stages: [
        { target: 10, duration: '1m' },
        { target: 40, duration: '2m' },
        { target: 40, duration: '10m' }, // 40 requests/s x 50 = 2,000 events/s
        { target: 0, duration: '1m' },
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    accepted_rate: ['rate>0.99'],
    rejection_rate: ['rate<0.01'],
  },
}

function uuidv7() {
  const timestamp = Date.now().toString(16).padStart(12, '0')
  const random = () => Math.floor(Math.random() * 0x10000).toString(16).padStart(4, '0')
  const variant = (8 + Math.floor(Math.random() * 4)).toString(16) + random().slice(1)
  return `${timestamp.slice(0, 8)}-${timestamp.slice(8)}-7${random().slice(1)}-${variant}-${random()}${random()}${random()}`
}

export default function () {
  const now = Date.now()
  const events = Array.from({ length: 50 }, () => ({
    event_id: uuidv7(),
    point_code: __ENV.GO_DOT_LOAD_POINT_CODE || 'product_detail_view',
    event_time: now,
    anonymous_id: `k6-${__VU}-${__ITER}`,
    platform: 'web',
    properties: {},
  }))
  const response = http.post(`${__ENV.GO_DOT_LOAD_BASE_URL || 'http://127.0.0.1:8080'}/api/v1/collect/events`, JSON.stringify({ events }), {
    headers: { 'Content-Type': 'application/json', 'X-Write-Key': __ENV.GO_DOT_LOAD_WRITE_KEY },
  })
  const accepted = check(response, { accepted: (r) => r.status === 202 && r.json('accepted') === 50 })
  acceptedRate.add(accepted)
  rejectionRate.add(response.status === 202 ? Number(response.json('rejected')) > 0 : true)
  if (__ENV.GO_DOT_LOAD_QUEUE_LAG_URL && __ITER % 10 === 0) {
    const lag = http.get(__ENV.GO_DOT_LOAD_QUEUE_LAG_URL)
    if (lag.status === 200) queueLag.add(Number(lag.json('queue.pending') || 0))
  }
  sleep(0.01)
}

export function teardown() {
  if (!__ENV.GO_DOT_LOAD_QUEUE_LAG_URL) return
  const started = Date.now()
  const timeout = Number(__ENV.GO_DOT_LOAD_DRAIN_TIMEOUT_MS || 120000)
  while (Date.now() - started < timeout) {
    const response = http.get(__ENV.GO_DOT_LOAD_QUEUE_LAG_URL)
    if (response.status === 200) {
      const pending = Number(response.json('queue.pending') || 0)
      queueLag.add(pending)
      if (pending === 0) {
        drainTime.add(Date.now() - started)
        return
      }
    }
    sleep(1)
  }
  drainTime.add(timeout)
}
