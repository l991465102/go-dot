package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/redis/go-redis/v9"

	clickhouseadapter "github.com/lzw7758/go-dot/internal/adapter/clickhouse"
	"github.com/lzw7758/go-dot/internal/adapter/rediscounter"
	"github.com/lzw7758/go-dot/internal/adapter/redisqueue"
	"github.com/lzw7758/go-dot/internal/config"
	"github.com/lzw7758/go-dot/internal/ingestion"
)

func runWorker(ctx context.Context) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.ClickHouse.DSN == "" {
		return fmt.Errorf("GO_DOT_CLICKHOUSE_DSN is required")
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}

	db, err := sql.Open("clickhouse", cfg.ClickHouse.DSN)
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping clickhouse: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read worker hostname: %w", err)
	}
	consumer := fmt.Sprintf("%s-%d", hostname, os.Getpid())
	queue := redisqueue.New(redisClient, redisqueue.WithDeadStream(cfg.Queue.DeadStream), redisqueue.WithPublishDedupTTL(cfg.Queue.PublishDedupTTL))
	worker := ingestion.NewWorker(queue, clickhouseadapter.New(db), rediscounter.New(redisClient), ingestion.WorkerConfig{
		EventStream: cfg.Queue.EventStream, InvalidStream: cfg.Queue.InvalidStream, Group: cfg.Queue.ConsumerGroup,
		Consumer: consumer, VisitorMasterKey: []byte(cfg.Auth.VisitorMasterKey), Concurrency: cfg.Worker.Concurrency,
		MaxDeliveries: cfg.Worker.MaxDeliveries, ReclaimIdle: cfg.Worker.ReclaimIdle, DrainTimeout: cfg.Worker.DrainTimeout,
	})
	return worker.Run(ctx)
}
