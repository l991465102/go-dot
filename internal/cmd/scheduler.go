package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	clickhouseadapter "github.com/lzw7758/go-dot/internal/adapter/clickhouse"
	mysqladapter "github.com/lzw7758/go-dot/internal/adapter/mysql"
	"github.com/lzw7758/go-dot/internal/adapter/redisqueue"
	"github.com/lzw7758/go-dot/internal/config"
	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/observability"
	"github.com/lzw7758/go-dot/internal/statistics"
)

func runScheduler(ctx context.Context) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.MySQL.DSN == "" {
		return fmt.Errorf("GO_DOT_MYSQL_DSN is required")
	}
	if cfg.ClickHouse.DSN == "" {
		return fmt.Errorf("GO_DOT_CLICKHOUSE_DSN is required")
	}
	mysqlDB, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer mysqlDB.Close()
	if err := mysqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping mysql: %w", err)
	}
	chDB, err := sql.Open("clickhouse", cfg.ClickHouse.DSN)
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer chDB.Close()
	if err := chDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping clickhouse: %w", err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	projectRepo := mysqladapter.NewProjectRepository(mysqlDB)
	aggregate := statistics.NewAggregatorWithLookback(clickhouseadapter.NewStatisticsStore(chDB), projectRepo, nil, cfg.Scheduler.AggregateLookbackHours)
	queue := redisqueue.New(redisClient, redisqueue.WithDeadStream(cfg.Queue.DeadStream), redisqueue.WithPublishDedupTTL(cfg.Queue.PublishDedupTTL), redisqueue.WithOverviewStreams([]string{cfg.Queue.EventStream, cfg.Queue.InvalidStream}, cfg.Queue.ConsumerGroup))
	metrics := observability.NewMetrics()
	logger := observability.NewJSONLogger(os.Stdout, "scheduler")
	observedAggregate := observability.ObservedStatisticsRefresher{Next: aggregate, Metrics: metrics}
	monitor := redisqueue.NewQueueMonitor(redisClient, []string{cfg.Queue.EventStream, cfg.Queue.InvalidStream}, cfg.Queue.ConsumerGroup, cfg.Queue.DeadStream, observability.QueueSnapshotSink{Metrics: metrics})
	host, _ := os.Hostname()
	jobs := ingestion.SchedulerRuntimeJobs{Statistics: observedAggregate, Queue: queue, Observer: monitor, Keys: projectRepo}
	reporter := observability.ErrorLogger{Logger: logger}
	scheduler := ingestion.NewScheduler(redisqueue.NewLeaseManager(redisClient), jobs, ingestion.SchedulerConfig{EventStream: cfg.Queue.EventStream, InvalidStream: cfg.Queue.InvalidStream, Group: cfg.Queue.ConsumerGroup, ReclaimConsumer: fmt.Sprintf("scheduler-%s-%d", host, os.Getpid()), AggregateEvery: cfg.Scheduler.AggregateEvery, ReclaimEvery: cfg.Scheduler.ReclaimEvery, MetricsEvery: cfg.Scheduler.MetricsEvery, ExpireKeysEvery: cfg.Scheduler.ExpireKeysEvery, ReclaimIdle: cfg.Scheduler.ReclaimIdle, LeaseTTL: cfg.Scheduler.LeaseTTL, BatchSize: cfg.Scheduler.BatchSize, MaxDeliveries: cfg.Worker.MaxDeliveries, Reporter: reporter})
	metricsAddress := os.Getenv("GO_DOT_METRICS_ADDRESS")
	if metricsAddress == "" {
		metricsAddress = ":9091"
	}
	return runWithMetrics(ctx, metricsAddress, metrics.Handler(), scheduler.Run)
}
