package clickhouse

import (
	"context"
	"errors"
	"fmt"

	"github.com/lzw7758/go-dot/internal/reporting"
)

func (s *StatisticsStore) QueryDaily(ctx context.Context, filter reporting.DailyFilter) ([]reporting.DailyAggregate, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("ClickHouse statistics executor is required")
	}
	if filter.ProjectID == 0 || filter.Start.IsZero() || !filter.End.After(filter.Start) || filter.Timezone != "Asia/Shanghai" {
		return nil, errors.New("project, Asia/Shanghai timezone, and valid time range are required")
	}
	if !identifierPattern.MatchString(s.approximateTable) || !identifierPattern.MatchString(s.exactTable) || !identifierPattern.MatchString(s.generationTable) {
		return nil, errors.New("invalid ClickHouse statistics table name")
	}

	type queryPart struct {
		table    string
		uvMerge  string
		pointIDs []uint64
	}
	parts := []queryPart{
		{table: s.approximateTable, uvMerge: "uniqCombined64Merge(uv_state)", pointIDs: filter.ApproximatePointIDs},
		{table: s.exactTable, uvMerge: "uniqExactMerge(uv_state)", pointIDs: filter.ExactPointIDs},
	}
	type dailyKey struct {
		date    string
		pointID uint64
	}
	merged := make(map[dailyKey]reporting.DailyAggregate)
	for _, part := range parts {
		if len(part.pointIDs) == 0 {
			continue
		}
		inner := `SELECT s.hour,s.project_id,s.domain_category_id,s.domain_category_code,s.module_category_id,s.module_category_code,s.scene_category_id,s.scene_category_code,s.point_id,s.point_code,s.platform,s.channel,s.version,any(s.pv) AS pv,any(s.uv_state) AS uv_state FROM ` + part.table + ` AS s INNER JOIN ` + s.generationTable + ` AS g FINAL ON s.project_id = g.project_id AND s.hour = g.hour AND s.version = g.generation WHERE s.project_id=? AND s.hour>=? AND s.hour<? AND s.point_id IN ` + placeholders(len(part.pointIDs)) + ` GROUP BY s.hour,s.project_id,s.domain_category_id,s.domain_category_code,s.module_category_id,s.module_category_code,s.scene_category_id,s.scene_category_code,s.point_id,s.point_code,s.platform,s.channel,s.version`
		query := `SELECT toString(toDate(hour, 'Asia/Shanghai')) AS date,point_id,point_code,sum(pv),` + part.uvMerge + ` FROM (` + inner + `) GROUP BY toDate(hour, 'Asia/Shanghai'),point_id,point_code ORDER BY date,point_id`
		args := make([]any, 0, 3+len(part.pointIDs))
		args = append(args, filter.ProjectID, filter.Start.UTC(), filter.End.UTC())
		for _, pointID := range part.pointIDs {
			args = append(args, pointID)
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query daily statistics from %s: %w", part.table, err)
		}
		for rows.Next() {
			var aggregate reporting.DailyAggregate
			if err := rows.Scan(&aggregate.Date, &aggregate.PointID, &aggregate.PointCode, &aggregate.PV, &aggregate.UV); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan daily statistics: %w", err)
			}
			key := dailyKey{date: aggregate.Date, pointID: aggregate.PointID}
			if existing, ok := merged[key]; ok {
				existing.PV += aggregate.PV
				existing.UV += aggregate.UV
				if existing.PointCode == "" {
					existing.PointCode = aggregate.PointCode
				}
				merged[key] = existing
			} else {
				merged[key] = aggregate
			}
		}
		rowErr := rows.Err()
		closeErr := rows.Close()
		if rowErr != nil {
			return nil, fmt.Errorf("iterate daily statistics: %w", rowErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close daily statistics rows: %w", closeErr)
		}
	}

	out := make([]reporting.DailyAggregate, 0, len(merged))
	for _, aggregate := range merged {
		out = append(out, aggregate)
	}
	return out, nil
}
