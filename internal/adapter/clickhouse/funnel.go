package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lzw7758/go-dot/internal/funnel"
)

// QueryFunnel 基于事件明细按访客和严格时间顺序计算漏斗。
func (s *StatisticsStore) QueryFunnel(ctx context.Context, q funnel.Query) ([]uint64, error) {
	if s == nil || s.db == nil || !identifierPattern.MatchString(s.rawTable) || len(q.PointCodes) < 2 || len(q.PointCodes) > 5 {
		return nil, errors.New("invalid funnel store")
	}
	conditions := make([]string, len(q.PointCodes))
	args := []any{q.WindowHours * 3600000}
	conditions[0] = "(point_code=? AND event_at>=? AND event_at<?)"
	args = append(args, q.PointCodes[0], q.Start.UTC(), q.End.UTC())
	for i := 1; i < len(conditions); i++ {
		conditions[i] = "point_code=?"
		args = append(args, q.PointCodes[i])
	}
	counts := make([]string, len(conditions))
	for i := range counts {
		counts[i] = fmt.Sprintf("countIf(level >= %d)", i+1)
	}
	query := `SELECT ` + strings.Join(counts, ",") + ` FROM (SELECT visitor_hash, windowFunnel(?, 'strict_increase')(toUInt64(toUnixTimestamp64Milli(event_at)), ` + strings.Join(conditions, ",") + `) AS level FROM (SELECT project_id,event_id,argMax(point_code,version) AS point_code,argMax(visitor_hash,version) AS visitor_hash,argMax(statistic_at,version) AS event_at,argMax(platform,version) AS platform,argMax(channel,version) AS channel FROM ` + s.rawTable + ` FINAL WHERE project_id=? AND statistic_at>=? AND statistic_at<? GROUP BY project_id,event_id) WHERE point_code IN ` + placeholders(len(q.PointCodes))
	args = append(args, q.ProjectID, q.Start.UTC(), q.EvaluatedUntil.UTC())
	for _, code := range q.PointCodes {
		args = append(args, code)
	}
	if q.Platform != "" {
		query += " AND platform=?"
		args = append(args, q.Platform)
	}
	if q.Channel != "" {
		query += " AND channel=?"
		args = append(args, q.Channel)
	}
	query += " GROUP BY visitor_hash)"
	values := make([]uint64, len(counts))
	dest := make([]any, len(counts))
	for i := range values {
		dest[i] = &values[i]
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query funnel: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("empty funnel result")
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, fmt.Errorf("query funnel: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}
