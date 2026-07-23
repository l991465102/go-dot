package config

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type Config struct {
	HTTP       HTTPConfig
	MySQL      MySQLConfig
	Redis      RedisConfig
	ClickHouse ClickHouseConfig
	Auth       AuthConfig
	Tracking   TrackingConfig
	Queue      QueueConfig
	Worker     WorkerConfig
	Scheduler  SchedulerConfig
}

type HTTPConfig struct {
	Address string
}

type MySQLConfig struct {
	DSN string
}

type RedisConfig struct {
	Address string
}

type ClickHouseConfig struct {
	DSN string
}

type AuthConfig struct {
	JWTSecret        string
	VisitorMasterKey string
	AccessTTL        time.Duration
	RefreshTTL       time.Duration
	SecureCookies    bool
}

type TrackingConfig struct {
	MaxBatchEvents int
	MaxBodyBytes   int
}

type QueueConfig struct {
	EventStream     string
	InvalidStream   string
	DeadStream      string
	ConsumerGroup   string
	PublishDedupTTL time.Duration
}

type WorkerConfig struct {
	Concurrency   int
	MaxDeliveries int
	ReclaimIdle   time.Duration
	DrainTimeout  time.Duration
	RetryBase     time.Duration
	RetryMax      time.Duration
}
type SchedulerConfig struct {
	AggregateEvery, ReclaimEvery, MetricsEvery, ExpireKeysEvery, ReclaimIdle, LeaseTTL time.Duration
	BatchSize, AggregateLookbackHours                                                  int
	PropertyAggregateRetentionDays                                                     int
}

func Load(ctx context.Context) (Config, error) {
	cfg := Config{
		HTTP:       HTTPConfig{Address: g.Cfg().MustGet(ctx, "server.address", ":8080").String()},
		MySQL:      MySQLConfig{DSN: os.Getenv("GO_DOT_MYSQL_DSN")},
		Redis:      RedisConfig{Address: envOr("GO_DOT_REDIS_ADDRESS", "redis:6379")},
		ClickHouse: ClickHouseConfig{DSN: os.Getenv("GO_DOT_CLICKHOUSE_DSN")},
		Auth: AuthConfig{
			JWTSecret:        os.Getenv("GO_DOT_JWT_SECRET"),
			VisitorMasterKey: os.Getenv("GO_DOT_VISITOR_MASTER_KEY"),
			AccessTTL:        15 * time.Minute,
			RefreshTTL:       7 * 24 * time.Hour,
		},
		Tracking: TrackingConfig{MaxBatchEvents: 100, MaxBodyBytes: 256 << 10},
		Queue: QueueConfig{
			EventStream:     envOr("GO_DOT_QUEUE_EVENT_STREAM", "tracking:{queue}:events"),
			InvalidStream:   envOr("GO_DOT_QUEUE_INVALID_STREAM", "tracking:{queue}:invalid"),
			DeadStream:      envOr("GO_DOT_QUEUE_DEAD_STREAM", "tracking:{queue}:dead"),
			ConsumerGroup:   envOr("GO_DOT_QUEUE_CONSUMER_GROUP", "tracking-workers"),
			PublishDedupTTL: 72 * time.Hour,
		},
		Worker:    WorkerConfig{Concurrency: 8, MaxDeliveries: 5, ReclaimIdle: 30 * time.Second, DrainTimeout: 30 * time.Second, RetryBase: time.Second, RetryMax: time.Minute},
		Scheduler: SchedulerConfig{AggregateEvery: time.Minute, ReclaimEvery: 15 * time.Second, MetricsEvery: time.Minute, ExpireKeysEvery: time.Hour, ReclaimIdle: time.Minute, LeaseTTL: 2 * time.Minute, BatchSize: 100, AggregateLookbackHours: 2, PropertyAggregateRetentionDays: 400},
	}
	if raw := os.Getenv("GO_DOT_SECURE_COOKIES"); raw != "" {
		secure, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, errors.New("GO_DOT_SECURE_COOKIES must be true or false")
		}
		cfg.Auth.SecureCookies = secure
	}
	if raw := os.Getenv("GO_DOT_QUEUE_PUBLISH_DEDUP_TTL"); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil || ttl < 72*time.Hour || ttl > 7*24*time.Hour {
			return Config{}, errors.New("GO_DOT_QUEUE_PUBLISH_DEDUP_TTL must be a duration between 72h and 168h")
		}
		cfg.Queue.PublishDedupTTL = ttl
	}
	if err := loadPositiveInt("GO_DOT_WORKER_CONCURRENCY", &cfg.Worker.Concurrency); err != nil {
		return Config{}, err
	}
	if err := loadPositiveInt("GO_DOT_WORKER_MAX_DELIVERIES", &cfg.Worker.MaxDeliveries); err != nil {
		return Config{}, err
	}
	if err := loadPositiveDuration("GO_DOT_WORKER_RECLAIM_IDLE", &cfg.Worker.ReclaimIdle); err != nil {
		return Config{}, err
	}
	if err := loadPositiveDuration("GO_DOT_WORKER_DRAIN_TIMEOUT", &cfg.Worker.DrainTimeout); err != nil {
		return Config{}, err
	}
	if err := loadPositiveDuration("GO_DOT_WORKER_RETRY_BASE", &cfg.Worker.RetryBase); err != nil {
		return Config{}, err
	}
	if err := loadPositiveDuration("GO_DOT_WORKER_RETRY_MAX", &cfg.Worker.RetryMax); err != nil {
		return Config{}, err
	}
	for _, item := range []struct {
		name   string
		target *time.Duration
	}{{"GO_DOT_SCHEDULER_AGGREGATE_EVERY", &cfg.Scheduler.AggregateEvery}, {"GO_DOT_SCHEDULER_RECLAIM_EVERY", &cfg.Scheduler.ReclaimEvery}, {"GO_DOT_SCHEDULER_METRICS_EVERY", &cfg.Scheduler.MetricsEvery}, {"GO_DOT_SCHEDULER_EXPIRE_KEYS_EVERY", &cfg.Scheduler.ExpireKeysEvery}, {"GO_DOT_SCHEDULER_RECLAIM_IDLE", &cfg.Scheduler.ReclaimIdle}, {"GO_DOT_SCHEDULER_LEASE_TTL", &cfg.Scheduler.LeaseTTL}} {
		if err := loadPositiveDuration(item.name, item.target); err != nil {
			return Config{}, err
		}
	}
	if err := loadPositiveInt("GO_DOT_SCHEDULER_BATCH_SIZE", &cfg.Scheduler.BatchSize); err != nil {
		return Config{}, err
	}
	if err := loadPositiveInt("GO_DOT_SCHEDULER_AGGREGATE_LOOKBACK_HOURS", &cfg.Scheduler.AggregateLookbackHours); err != nil {
		return Config{}, err
	}
	if err := loadPositiveInt("GO_DOT_PROPERTY_AGGREGATE_RETENTION_DAYS", &cfg.Scheduler.PropertyAggregateRetentionDays); err != nil {
		return Config{}, err
	}
	if cfg.Scheduler.PropertyAggregateRetentionDays < 90 {
		return Config{}, errors.New("GO_DOT_PROPERTY_AGGREGATE_RETENTION_DAYS must be at least 90")
	}
	if cfg.Scheduler.AggregateLookbackHours > 168 {
		return Config{}, errors.New("GO_DOT_SCHEDULER_AGGREGATE_LOOKBACK_HOURS must be between 1 and 168")
	}
	if cfg.Worker.RetryBase < 100*time.Millisecond || cfg.Worker.RetryBase > time.Hour {
		return Config{}, errors.New("GO_DOT_WORKER_RETRY_BASE must be between 100ms and 1h")
	}
	if cfg.Worker.RetryMax > 24*time.Hour {
		return Config{}, errors.New("GO_DOT_WORKER_RETRY_MAX must not exceed 24h")
	}
	if cfg.Worker.RetryMax < cfg.Worker.RetryBase {
		return Config{}, errors.New("GO_DOT_WORKER_RETRY_MAX must be greater than or equal to GO_DOT_WORKER_RETRY_BASE")
	}
	if err := validateQueueHashTags(cfg.Queue.EventStream, cfg.Queue.InvalidStream, cfg.Queue.DeadStream); err != nil {
		return Config{}, err
	}
	if cfg.Auth.JWTSecret == "" {
		return Config{}, errors.New("GO_DOT_JWT_SECRET is required")
	}
	if cfg.Auth.VisitorMasterKey == "" {
		return Config{}, errors.New("GO_DOT_VISITOR_MASTER_KEY is required")
	}
	return cfg, nil
}

func validateQueueHashTags(streams ...string) error {
	var common string
	for _, stream := range streams {
		tag, ok := configRedisHashTag(stream)
		if !ok {
			return errors.New("queue streams must contain a nonempty Redis hash tag")
		}
		if common == "" {
			common = tag
		} else if tag != common {
			return errors.New("queue streams must use the same Redis hash tag")
		}
	}
	return nil
}

func configRedisHashTag(key string) (string, bool) {
	start := strings.IndexByte(key, '{')
	if start < 0 {
		return "", false
	}
	end := strings.IndexByte(key[start+1:], '}')
	if end <= 0 {
		return "", false
	}
	return key[start+1 : start+1+end], true
}

func loadPositiveInt(name string, target *int) error {
	if raw := os.Getenv(name); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return errors.New(name + " must be a positive integer")
		}
		*target = value
	}
	return nil
}

func loadPositiveDuration(name string, target *time.Duration) error {
	if raw := os.Getenv(name); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return errors.New(name + " must be a positive duration")
		}
		*target = value
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
