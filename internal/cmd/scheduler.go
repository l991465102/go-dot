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
	aggregate := statistics.NewAggregator(clickhouseadapter.NewStatisticsStore(chDB), projectRepo, nil)
	queue := redisqueue.New(redisClient, redisqueue.WithDeadStream(cfg.Queue.DeadStream), redisqueue.WithPublishDedupTTL(cfg.Queue.PublishDedupTTL))
	monitor := redisqueue.NewQueueMonitor(redisClient, []string{cfg.Queue.EventStream, cfg.Queue.InvalidStream}, cfg.Queue.ConsumerGroup, cfg.Queue.DeadStream, redisqueue.NoopQueueSnapshotSink{})
	host, _ := os.Hostname()
	jobs := ingestion.SchedulerRuntimeJobs{Statistics: aggregate, Queue: queue, Observer: monitor, Keys: projectRepo}
	scheduler := ingestion.NewScheduler(redisqueue.NewLeaseManager(redisClient), jobs, ingestion.SchedulerConfig{EventStream: cfg.Queue.EventStream, InvalidStream: cfg.Queue.InvalidStream, Group: cfg.Queue.ConsumerGroup, ReclaimConsumer: fmt.Sprintf("scheduler-%s-%d", host, os.Getpid()), AggregateEvery: cfg.Scheduler.AggregateEvery, ReclaimEvery: cfg.Scheduler.ReclaimEvery, MetricsEvery: cfg.Scheduler.MetricsEvery, ExpireKeysEvery: cfg.Scheduler.ExpireKeysEvery, ReclaimIdle: cfg.Scheduler.ReclaimIdle, LeaseTTL: cfg.Scheduler.LeaseTTL, BatchSize: cfg.Scheduler.BatchSize})
	return scheduler.Run(ctx)
}
