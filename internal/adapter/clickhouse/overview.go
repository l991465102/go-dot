package clickhouse

import (
	"context"
	"errors"
	"fmt"

	"github.com/lzw7758/go-dot/internal/overview"
)

func (s *StatisticsStore) ValidCount(ctx context.Context, projectID uint64, window overview.Window) (int64, error) {
	if s == nil || s.db == nil || !identifierPattern.MatchString(s.rawTable) {
		return 0, errors.New("invalid ClickHouse overview store")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT count() FROM `+s.rawTable+` FINAL WHERE project_id=? AND received_at>=? AND received_at<?`, projectID, window.Start, window.End)
	if err != nil {
		return 0, fmt.Errorf("count valid events: %w", err)
	}
	defer rows.Close()
	var count int64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, err
		}
		return 0, errors.New("valid event count returned no row")
	}
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, rows.Err()
}

func (s *StatisticsStore) InvalidCount(ctx context.Context, projectID uint64, window overview.Window) (int64, error) {
	if s == nil || s.db == nil || !identifierPattern.MatchString(s.invalidEventsTable) {
		return 0, errors.New("invalid ClickHouse overview store")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT count() FROM `+s.invalidEventsTable+` FINAL WHERE project_id=? AND received_at>=? AND received_at<?`, projectID, window.Start, window.End)
	if err != nil {
		return 0, fmt.Errorf("count invalid events: %w", err)
	}
	defer rows.Close()
	var count int64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, err
		}
		return 0, errors.New("invalid event count returned no row")
	}
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, rows.Err()
}
func (s *StatisticsStore) ActivePointIDs(ctx context.Context, projectID uint64, window overview.Window) (map[uint64]bool, error) {
	if s == nil || s.db == nil || !identifierPattern.MatchString(s.rawTable) {
		return nil, errors.New("invalid ClickHouse overview store")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT point_id FROM `+s.rawTable+` FINAL WHERE project_id=? AND received_at>=? AND received_at<?`, projectID, window.Start, window.End)
	if err != nil {
		return nil, fmt.Errorf("query active points: %w", err)
	}
	defer rows.Close()
	out := map[uint64]bool{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
