package config

import (
	"context"
	"errors"
	"os"
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
}

type TrackingConfig struct {
	MaxBatchEvents int
	MaxBodyBytes   int
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
	}
	if cfg.Auth.JWTSecret == "" {
		return Config{}, errors.New("GO_DOT_JWT_SECRET is required")
	}
	if cfg.Auth.VisitorMasterKey == "" {
		return Config{}, errors.New("GO_DOT_VISITOR_MASTER_KEY is required")
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
