package observability

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type QueueSnapshot struct {
	Lag, Pending, DeadLetters int64
	OldestPendingAge          time.Duration
}

type Metrics struct {
	registry                                             *prometheus.Registry
	collection, accepted, rejected, duplicates, failures prometheus.Counter
	queueLag, queuePending, queueDLQ, queueOldest        prometheus.Gauge
	consume, redisOperation, clickhouseDuration          prometheus.Histogram
	retries, clickhouseFailures                          prometheus.Counter
	clickhouseBatch                                      prometheus.Histogram
	realtimeDelay                                        prometheus.Gauge
	aggregationDelay                                     prometheus.Collector
	aggregationLastSuccess                               atomic.Int64
	now                                                  func() time.Time
}

func NewMetrics() *Metrics {
	return newMetrics(time.Now)
}

func newMetrics(now func() time.Time) *Metrics {
	m := &Metrics{registry: prometheus.NewRegistry(), now: now}
	m.collection = prometheus.NewCounter(prometheus.CounterOpts{Name: "go_dot_collection_events_total", Help: "Events received by collection."})
	m.accepted = prometheus.NewCounter(prometheus.CounterOpts{Name: "go_dot_collection_accepted_total", Help: "Events accepted by collection."})
	m.rejected = prometheus.NewCounter(prometheus.CounterOpts{Name: "go_dot_collection_rejected_total", Help: "Events rejected by validation."})
	m.duplicates = prometheus.NewCounter(prometheus.CounterOpts{Name: "go_dot_collection_duplicate_total", Help: "Duplicate events ignored."})
	m.failures = prometheus.NewCounter(prometheus.CounterOpts{Name: "go_dot_collection_failed_total", Help: "Collection events not accepted due to infrastructure failure."})
	m.queueLag = prometheus.NewGauge(prometheus.GaugeOpts{Name: "go_dot_queue_lag", Help: "Outstanding queue messages."})
	m.queuePending = prometheus.NewGauge(prometheus.GaugeOpts{Name: "go_dot_queue_pending", Help: "Redis Stream Pending messages."})
	m.queueDLQ = prometheus.NewGauge(prometheus.GaugeOpts{Name: "go_dot_queue_dlq", Help: "Dead-letter messages."})
	m.queueOldest = prometheus.NewGauge(prometheus.GaugeOpts{Name: "go_dot_queue_oldest_pending_age_seconds", Help: "Age of oldest Pending message."})
	m.consume = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "go_dot_worker_consume_duration_seconds", Help: "Worker batch processing duration."})
	m.retries = prometheus.NewCounter(prometheus.CounterOpts{Name: "go_dot_worker_retries_total", Help: "Worker retry attempts."})
	m.redisOperation = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "go_dot_redis_operation_duration_seconds", Help: "Redis operation duration."})
	m.clickhouseBatch = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "go_dot_clickhouse_batch_size", Help: "ClickHouse write batch size.", Buckets: []float64{1, 10, 50, 100, 500}})
	m.clickhouseDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "go_dot_clickhouse_batch_duration_seconds", Help: "ClickHouse write duration."})
	m.clickhouseFailures = prometheus.NewCounter(prometheus.CounterOpts{Name: "go_dot_clickhouse_failures_total", Help: "ClickHouse write failures."})
	m.realtimeDelay = prometheus.NewGauge(prometheus.GaugeOpts{Name: "go_dot_realtime_statistics_delay_seconds", Help: "Realtime counter delay."})
	m.aggregationDelay = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "go_dot_aggregation_delay_seconds", Help: "Seconds since the last successful hourly aggregate refresh; -1 before the first success."}, func() float64 {
		last := m.aggregationLastSuccess.Load()
		if last == 0 {
			return -1
		}
		delay := m.now().Sub(time.Unix(0, last)).Seconds()
		if delay < 0 {
			return 0
		}
		return delay
	})
	m.registry.MustRegister(m.collection, m.accepted, m.rejected, m.duplicates, m.failures, m.queueLag, m.queuePending, m.queueDLQ, m.queueOldest, m.consume, m.retries, m.redisOperation, m.clickhouseBatch, m.clickhouseDuration, m.clickhouseFailures, m.realtimeDelay, m.aggregationDelay)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
func (m *Metrics) ObserveCollection(accepted, rejected int, err error) {
	total := accepted + rejected
	m.collection.Add(float64(total))
	m.accepted.Add(float64(accepted))
	m.rejected.Add(float64(rejected))
	if err != nil {
		m.failures.Add(float64(max(1, total)))
	}
}
func (m *Metrics) ObserveDuplicate(count int) { m.duplicates.Add(float64(count)) }
func (m *Metrics) SetQueueSnapshot(s QueueSnapshot) {
	m.queueLag.Set(float64(s.Lag))
	m.queuePending.Set(float64(s.Pending))
	m.queueDLQ.Set(float64(s.DeadLetters))
	m.queueOldest.Set(s.OldestPendingAge.Seconds())
}
func (m *Metrics) ObserveWorker(duration time.Duration, retry bool) {
	m.consume.Observe(duration.Seconds())
	if retry {
		m.retries.Inc()
	}
}
func (m *Metrics) ObserveRedis(duration time.Duration) { m.redisOperation.Observe(duration.Seconds()) }
func (m *Metrics) ObserveClickHouseBatch(size int, duration time.Duration, err error) {
	m.clickhouseBatch.Observe(float64(size))
	m.clickhouseDuration.Observe(duration.Seconds())
	if err != nil {
		m.clickhouseFailures.Inc()
	}
}
func (m *Metrics) SetRealtimeDelay(seconds float64)    { m.realtimeDelay.Set(seconds) }
func (m *Metrics) MarkAggregationSuccess(at time.Time) { m.aggregationLastSuccess.Store(at.UnixNano()) }
