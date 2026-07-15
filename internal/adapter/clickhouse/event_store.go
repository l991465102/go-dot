package clickhouse

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/lzw7758/go-dot/internal/ports"
)

type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type EventStore struct {
	db           Executor
	validTable   string
	invalidTable string
}

func New(db Executor) *EventStore {
	return newWithTables(db, "tracking_event", "tracking_invalid_event")
}

func newWithTables(db Executor, validTable, invalidTable string) *EventStore {
	return &EventStore{db: db, validTable: validTable, invalidTable: invalidTable}
}

func (s *EventStore) SaveValid(ctx context.Context, token string, events []ports.StoredEvent) error {
	if len(events) == 0 {
		return nil
	}
	if err := validateStore(s, token); err != nil {
		return err
	}
	const columns = `(event_id,batch_id,project_id,point_id,point_code,point_version,domain_category_id,domain_category_code,module_category_id,module_category_code,scene_category_id,scene_category_code,visitor_hash,platform,channel,app_version,session_hash,client_event_at,received_at,statistic_at,statistic_date,time_corrected,properties_json,version)`
	args := make([]any, 0, len(events)*24)
	rows := make([]string, 0, len(events))
	for i, event := range events {
		properties, err := canonicalJSON(event.Properties)
		if err != nil {
			return fmt.Errorf("canonicalize valid event %d properties: %w", i, err)
		}
		statisticDate, err := localDate(event.StatisticAt, event.ProjectTimezone)
		if err != nil {
			return fmt.Errorf("valid event %d: %w", i, err)
		}
		version := event.Version
		if version == 0 {
			version = timestampVersion(event.ReceivedAt)
		}
		rows = append(rows, placeholders(24))
		args = append(args,
			event.EventID, token, event.ProjectID, event.PointID, event.PointCode, event.PointVersion,
			event.DomainCategoryID, event.DomainCategoryCode, event.ModuleCategoryID, event.ModuleCategoryCode,
			event.SceneCategoryID, event.SceneCategoryCode, fmt.Sprintf("%064x", event.VisitorHash),
			event.Platform, event.Channel, event.AppVersion, event.SessionHash,
			event.ClientEventAt.UTC(), event.ReceivedAt.UTC(), event.StatisticAt.UTC(), statisticDate,
			event.TimeCorrected, properties, version,
		)
	}
	query := `INSERT INTO ` + s.validTable + ` ` + columns + ` VALUES ` + strings.Join(rows, ",")
	if _, err := s.db.ExecContext(insertContext(ctx, token), query, args...); err != nil {
		return fmt.Errorf("insert valid events: %w", err)
	}
	return nil
}

func (s *EventStore) SaveInvalid(ctx context.Context, token string, events []ports.InvalidEvent) error {
	if len(events) == 0 {
		return nil
	}
	if err := validateStore(s, token); err != nil {
		return err
	}
	const columns = `(project_id,batch_id,event_id,point_code,platform,channel,error_code,error_message,raw_json,received_at,version)`
	args := make([]any, 0, len(events)*11)
	rows := make([]string, 0, len(events))
	for i, event := range events {
		raw, err := canonicalRawJSON(event.Original)
		if err != nil {
			return fmt.Errorf("canonicalize invalid event %d payload: %w", i, err)
		}
		version := event.Version
		if version == 0 {
			version = timestampVersion(event.ReceivedAt)
		}
		rows = append(rows, placeholders(11))
		args = append(args, event.ProjectID, token, event.EventID, event.PointCode, event.Platform, event.Channel,
			event.ErrorCode, event.ErrorMessage, raw, event.ReceivedAt.UTC(), version)
	}
	query := `INSERT INTO ` + s.invalidTable + ` ` + columns + ` VALUES ` + strings.Join(rows, ",")
	if _, err := s.db.ExecContext(insertContext(ctx, token), query, args...); err != nil {
		return fmt.Errorf("insert invalid events: %w", err)
	}
	return nil
}

func validateStore(store *EventStore, token string) error {
	if store == nil || store.db == nil {
		return errors.New("ClickHouse executor is required")
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("ClickHouse insert deduplication token is required")
	}
	if !identifierPattern.MatchString(store.validTable) || !identifierPattern.MatchString(store.invalidTable) {
		return errors.New("invalid ClickHouse event table name")
	}
	return nil
}

var identifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func insertContext(ctx context.Context, token string) context.Context {
	return ch.Context(ctx, ch.WithSettings(insertSettings(token)))
}

func insertSettings(token string) ch.Settings {
	return ch.Settings{
		"async_insert":               1,
		"wait_for_async_insert":      1,
		"async_insert_deduplicate":   1,
		"insert_deduplication_token": token,
	}
}

func canonicalJSON(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	// encoding/json orders map keys, so this single serialization is canonical.
	return string(raw), nil
}

func canonicalRawJSON(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "null", nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("multiple JSON values")
		}
		return "", fmt.Errorf("read trailing JSON: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func localDate(statisticAt time.Time, timezone string) (time.Time, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load project timezone %q: %w", timezone, err)
	}
	local := statisticAt.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC), nil
}

func timestampVersion(at time.Time) uint64 {
	if at.IsZero() {
		return 1
	}
	return uint64(at.UnixMilli())
}

func placeholders(count int) string {
	return "(" + strings.TrimSuffix(strings.Repeat("?,", count), ",") + ")"
}

var _ ports.EventStore = (*EventStore)(nil)
