package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type Dialect string

const (
	MySQL      Dialect = "mysql"
	ClickHouse Dialect = "clickhouse"
)

type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	MigrationChecksum(ctx context.Context, name string) (checksum string, found bool, err error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Transaction, error)
}

type Transaction interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Commit() error
	Rollback() error
}

type SQLExecutor struct {
	db      *sql.DB
	dialect Dialect
}

func NewSQLExecutor(db *sql.DB, dialect Dialect) *SQLExecutor {
	return &SQLExecutor{db: db, dialect: dialect}
}

func (e *SQLExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return e.db.ExecContext(ctx, query, args...)
}

func (e *SQLExecutor) MigrationChecksum(ctx context.Context, name string) (string, bool, error) {
	var checksum string
	err := e.db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE name = ? LIMIT 1", name).Scan(&checksum)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return checksum, true, nil
}

func (e *SQLExecutor) BeginTx(ctx context.Context, opts *sql.TxOptions) (Transaction, error) {
	return e.db.BeginTx(ctx, opts)
}

func Run(ctx context.Context, db Executor, dialect Dialect, files fs.FS) error {
	if err := ensureMigrationTable(ctx, db, dialect); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		name := filepath.ToSlash(entry.Name())
		contents, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		checksum := checksum(contents)
		appliedChecksum, found, err := db.MigrationChecksum(ctx, name)
		if err != nil {
			return fmt.Errorf("read migration %s state: %w", name, err)
		}
		if found {
			if appliedChecksum != checksum {
				return fmt.Errorf("migration %s checksum mismatch: database=%s file=%s", name, appliedChecksum, checksum)
			}
			continue
		}

		statements := splitStatements(string(contents))
		switch dialect {
		case MySQL:
			if err := applyMySQL(ctx, db, name, checksum, statements); err != nil {
				return err
			}
		case ClickHouse:
			if err := applyClickHouse(ctx, db, name, checksum, statements); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported migration dialect %q", dialect)
		}
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, db Executor, dialect Dialect) error {
	var statement string
	switch dialect {
	case MySQL:
		statement = `CREATE TABLE IF NOT EXISTS schema_migrations (
name VARCHAR(255) NOT NULL PRIMARY KEY,
checksum CHAR(64) NOT NULL,
applied_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`
	case ClickHouse:
		statement = `CREATE TABLE IF NOT EXISTS schema_migrations (
name String,
checksum FixedString(64),
applied_at DateTime64(6, 'UTC')
) ENGINE = ReplacingMergeTree(applied_at)
ORDER BY name`
	default:
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	_, err := db.ExecContext(ctx, statement)
	return err
}

func applyMySQL(ctx context.Context, db Executor, name, checksum string, statements []string) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("execute migration %s: %w", name, err)
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(name, checksum, applied_at) VALUES (?, ?, UTC_TIMESTAMP(6))", name, checksum); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}

func applyClickHouse(ctx context.Context, db Executor, name, checksum string, statements []string) error {
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("execute migration %s: %w", name, err)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations(name, checksum, applied_at) VALUES (?, ?, now64(6))", name, checksum); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return nil
}

func checksum(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func splitStatements(contents string) []string {
	parts := strings.Split(contents, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
