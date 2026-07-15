package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/redis/go-redis/v9"

	"github.com/lzw7758/go-dot/internal/adapter/httpapi"
	mysqladapter "github.com/lzw7758/go-dot/internal/adapter/mysql"
	"github.com/lzw7758/go-dot/internal/adapter/redisqueue"
	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/config"
	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/project"
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
	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping mysql: %w", err)
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
	queue := redisqueue.New(redisClient, redisqueue.WithDeadStream(cfg.Queue.DeadStream), redisqueue.WithPublishDedupTTL(cfg.Queue.PublishDedupTTL))
	collector := ingestion.NewCollector(snapshotCache, queue, ingestion.Config{MaxEvents: cfg.Tracking.MaxBatchEvents, EventStream: cfg.Queue.EventStream, InvalidStream: cfg.Queue.InvalidStream})
	collectHandler := httpapi.NewCollectHandler(
		httpapi.NewCollectController(collector, cfg.Tracking.MaxBodyBytes),
		httpapi.NewWriteKeyMiddleware(projectRepo, time.Now),
		httpapi.NewRateLimitMiddleware(httpapi.NewRedisTokenBucket(redisClient, 200, 100)),
	)
	managementHandler := httpapi.NewHandler(authService, projectService, os.Getenv("GO_DOT_SECURE_COOKIES") == "true", trackingService)
	handler := http.NewServeMux()
	handler.Handle("/api/v1/collect/events", collectHandler)
	handler.Handle("/", managementHandler)

	server := g.Server()
	server.SetAddr(cfg.HTTP.Address)
	server.BindHandler("GET:/health/live", httpapi.SystemController{}.Live)
	server.BindHandler("/*", ghttp.WrapH(handler))
	server.Run()
	return nil
}
