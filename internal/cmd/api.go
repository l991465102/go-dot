package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/redis/go-redis/v9"

	clickhouseadapter "github.com/lzw7758/go-dot/internal/adapter/clickhouse"
	"github.com/lzw7758/go-dot/internal/adapter/httpapi"
	mysqladapter "github.com/lzw7758/go-dot/internal/adapter/mysql"
	"github.com/lzw7758/go-dot/internal/adapter/rediscounter"
	"github.com/lzw7758/go-dot/internal/adapter/redisqueue"
	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/config"
	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/observability"
	"github.com/lzw7758/go-dot/internal/overview"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/reporting"
	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/lzw7758/go-dot/internal/tracking"
)

func runAPI(ctx context.Context) error {
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
	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
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
	authService := auth.NewService(mysqladapter.NewAuthRepository(db), []byte(cfg.Auth.JWTSecret), cfg.Auth.AccessTTL, cfg.Auth.RefreshTTL)
	if err := authService.EnsureInitialAdmin(ctx, "admin", "admin"); err != nil {
		return fmt.Errorf("ensure initial admin: %w", err)
	}
	projectRepo := mysqladapter.NewProjectRepository(db)
	projectService := project.NewService(projectRepo)
	trackingRepo := mysqladapter.NewTrackingRepository(db)
	snapshotCache := tracking.NewSnapshotCache(trackingRepo, time.Minute, time.Now)
	trackingService := tracking.NewService(trackingRepo, projectService, snapshotCache)
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address})
	defer redisClient.Close()
	queue := redisqueue.New(redisClient, redisqueue.WithDeadStream(cfg.Queue.DeadStream), redisqueue.WithPublishDedupTTL(cfg.Queue.PublishDedupTTL), redisqueue.WithOverviewStreams([]string{cfg.Queue.EventStream, cfg.Queue.InvalidStream}, cfg.Queue.ConsumerGroup))
	metrics := observability.NewMetrics()
	logger := observability.NewJSONLogger(os.Stdout, "api")
	observedQueue := observability.ObservedQueue{Next: queue, Metrics: metrics}
	statisticsStore := clickhouseadapter.NewStatisticsStore(chDB)
	statisticsService := statistics.NewService(projectService, rediscounter.New(redisClient), statisticsStore, snapshotCache,
		statistics.CompositeDiagnostics{Events: statisticsStore, DeadLetters: queue}, time.Now)
	reportRepository := mysqladapter.NewReportRepository(db)
	reportService := reporting.NewService(projectService, reportRepository, statisticsStore, reporting.ExcelExporter{}, time.Now).
		SetPropertyDailyStore(clickhouseadapter.NewPropertyStatisticsStore(chDB))
	collector := ingestion.NewCollector(snapshotCache, observedQueue, ingestion.Config{MaxEvents: cfg.Tracking.MaxBatchEvents, EventStream: cfg.Queue.EventStream, InvalidStream: cfg.Queue.InvalidStream})
	observedCollector := observability.ObservedCollectService{Next: collector, Metrics: metrics, Logger: logger}
	overviewService := overview.NewService(projectService, statisticsStore, queue, trackingRepo, time.Now)
	collectHandler := httpapi.NewCollectHandler(
		httpapi.NewCollectController(observedCollector, cfg.Tracking.MaxBodyBytes),
		httpapi.NewWriteKeyMiddleware(projectRepo, time.Now),
		httpapi.NewRateLimitMiddleware(httpapi.NewRedisTokenBucket(redisClient, 200, 100)),
	)
	managementHandler := httpapi.NewHandler(authService, projectService, cfg.Auth.SecureCookies, trackingService, statisticsService, overviewService, reportService)
	handler := http.NewServeMux()
	health := httpapi.NewHealthHandler(map[string]httpapi.HealthDependency{"mysql": db, "clickhouse": chDB, "redis": redisHealthDependency{client: redisClient}})
	handler.Handle("/health/", health)
	handler.Handle("/metrics", metrics.Handler())
	handler.Handle("/api/v1/collect/events", collectHandler)
	handler.Handle("/", managementHandler)
	rootHandler := httpapi.NewRequestIDMiddleware(logger.HTTPMiddleware(handler))

	server := g.Server()
	server.SetAddr(cfg.HTTP.Address)
	server.BindHandler("/*", ghttp.WrapH(rootHandler))
	server.Run()
	return nil
}

type redisHealthDependency struct{ client redis.UniversalClient }

func (d redisHealthDependency) PingContext(ctx context.Context) error {
	return d.client.Ping(ctx).Err()
}
