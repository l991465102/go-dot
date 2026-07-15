package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/go-sql-driver/mysql"
	"github.com/lzw7758/go-dot/internal/adapter/migration"
)

func runMigrate(ctx context.Context) error {
	mysqlDSN := os.Getenv("GO_DOT_MYSQL_DSN")
	if mysqlDSN == "" {
		return errors.New("GO_DOT_MYSQL_DSN is required")
	}
	clickhouseDSN := os.Getenv("GO_DOT_CLICKHOUSE_DSN")
	if clickhouseDSN == "" {
		return errors.New("GO_DOT_CLICKHOUSE_DSN is required")
	}
	root := os.Getenv("GO_DOT_MIGRATIONS_DIR")
	if root == "" {
		root = "migrations"
	}

	if err := migrateDatabase(ctx, "mysql", mysqlDSN, migration.MySQL, filepath.Join(root, "mysql")); err != nil {
		return err
	}
	if err := migrateDatabase(ctx, "clickhouse", clickhouseDSN, migration.ClickHouse, filepath.Join(root, "clickhouse")); err != nil {
		return err
	}
	return nil
}

func migrateDatabase(ctx context.Context, driver, dsn string, dialect migration.Dialect, path string) error {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", dialect, err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping %s: %w", dialect, err)
	}
	if err := migration.Run(ctx, migration.NewSQLExecutor(db, dialect), dialect, os.DirFS(path)); err != nil {
		return fmt.Errorf("migrate %s: %w", dialect, err)
	}
	return nil
}
