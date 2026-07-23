package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/reporting"
	"github.com/lzw7758/go-dot/internal/tracking"
)

func (s *PropertyStatisticsStore) QueryPropertyDaily(ctx context.Context, filter reporting.PropertyDailyFilter, properties []reporting.PropertyRef) (reporting.PropertyDailyData, error) {
	var result reporting.PropertyDailyData
	if s == nil || s.db == nil {
		return result, errors.New("ClickHouse property statistics executor is required")
	}
	if filter.ProjectID == 0 || filter.Start.IsZero() || !filter.End.After(filter.Start) || filter.Timezone != "Asia/Shanghai" {
		return result, errors.New("project, Asia/Shanghai timezone, and valid time range are required")
	}
	for _, table := range []string{s.metricTable, s.approximateTable, s.exactTable, s.generationTable} {
		if !identifierPattern.MatchString(table) {
			return result, errors.New("invalid ClickHouse property statistics table name")
		}
	}
	for _, property := range properties {
		switch property.Analytics.Mode {
		case tracking.AnalyticsMetric:
			items, err := s.queryMetricDaily(ctx, filter, property)
			if err != nil {
				return result, err
			}
			result.Metrics = append(result.Metrics, items...)
		case tracking.AnalyticsDimension:
			items, err := s.queryDimensionDaily(ctx, filter, property)
			if err != nil {
				return result, err
			}
			result.Dimensions = append(result.Dimensions, items...)
		default:
			return result, fmt.Errorf("unsupported analytics mode %s", property.Analytics.Mode)
		}
	}
	hours, err := s.queryCommittedPropertyHours(ctx, filter)
	if err != nil {
		return result, err
	}
	result.CommittedHours = hours
	return result, nil
}

func (s *PropertyStatisticsStore) queryCommittedPropertyHours(ctx context.Context, filter reporting.PropertyDailyFilter) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hour FROM `+s.generationTable+` FINAL WHERE project_id=? AND hour>=? AND hour<? ORDER BY hour`,
		filter.ProjectID, filter.Start.UTC(), filter.End.UTC())
	if err != nil {
		return nil, fmt.Errorf("query committed property hours: %w", err)
	}
	defer rows.Close()
	var hours []time.Time
	for rows.Next() {
		var hour time.Time
		if err := rows.Scan(&hour); err != nil {
			return nil, fmt.Errorf("scan committed property hour: %w", err)
		}
		hours = append(hours, hour.UTC())
	}
	return hours, rows.Err()
}

func (s *PropertyStatisticsStore) queryMetricDaily(ctx context.Context, filter reporting.PropertyDailyFilter, property reporting.PropertyRef) ([]reporting.MetricAggregate, error) {
	needCount, needSum, needMin, needMax := false, false, false, false
	for _, aggregation := range property.Aggregations {
		switch aggregation {
		case tracking.AggregateCount:
			needCount = true
		case tracking.AggregateSum:
			needSum = true
		case tracking.AggregateAvg:
			needCount, needSum = true, true
		case tracking.AggregateMin:
			needMin = true
		case tracking.AggregateMax:
			needMax = true
		}
	}
	hourlyMetrics := make([]string, 0, 4)
	if needCount {
		hourlyMetrics = append(hourlyMetrics, "any(s.value_count) AS value_count")
	}
	if needSum {
		hourlyMetrics = append(hourlyMetrics, "any(s.value_sum) AS value_sum")
	}
	if needMin {
		hourlyMetrics = append(hourlyMetrics, "any(s.value_min) AS value_min")
	}
	if needMax {
		hourlyMetrics = append(hourlyMetrics, "any(s.value_max) AS value_max")
	}
	inner := `SELECT s.hour,s.project_id,s.point_id,s.point_code,s.point_version,s.property_code,s.version,` + strings.Join(hourlyMetrics, ",") + `
FROM ` + s.metricTable + ` AS s
INNER JOIN ` + s.generationTable + ` AS g FINAL
ON s.project_id=g.project_id AND s.hour=g.hour AND s.version=g.generation
WHERE s.project_id=? AND s.hour>=? AND s.hour<? AND s.point_id=? AND s.property_code=?
GROUP BY s.hour,s.project_id,s.point_id,s.point_code,s.point_version,s.property_code,s.version`
	metrics := make([]string, 0, 4)
	if needCount {
		metrics = append(metrics, "sum(value_count)")
	}
	if needSum {
		metrics = append(metrics, "toString(sum(value_sum))")
	}
	if needMin {
		metrics = append(metrics, "toString(min(value_min))")
	}
	if needMax {
		metrics = append(metrics, "toString(max(value_max))")
	}
	query := `SELECT toString(toDate(hour,'Asia/Shanghai')),point_id,point_code,property_code,` + strings.Join(metrics, ",") + `
FROM (` + inner + `) GROUP BY toDate(hour,'Asia/Shanghai'),point_id,point_code,property_code ORDER BY 1 DESC`
	rows, err := s.db.QueryContext(ctx, query, filter.ProjectID, filter.Start.UTC(), filter.End.UTC(), property.PointID, property.PropertyCode)
	if err != nil {
		return nil, fmt.Errorf("query metric property %s: %w", property.PropertyCode, err)
	}
	defer rows.Close()
	var result []reporting.MetricAggregate
	for rows.Next() {
		var item reporting.MetricAggregate
		destinations := []any{&item.Date, &item.PointID, &item.PointCode, &item.PropertyCode}
		if needCount {
			destinations = append(destinations, &item.Count)
		}
		if needSum {
			destinations = append(destinations, &item.Sum)
		}
		if needMin {
			destinations = append(destinations, &item.Min)
		}
		if needMax {
			destinations = append(destinations, &item.Max)
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan metric property %s: %w", property.PropertyCode, err)
		}
		item.Present = true
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PropertyStatisticsStore) queryDimensionDaily(ctx context.Context, filter reporting.PropertyDailyFilter, property reporting.PropertyRef) ([]reporting.DimensionAggregate, error) {
	table, mergeState, merge := s.approximateTable, "uniqCombined64MergeState", "uniqCombined64Merge"
	if property.UVMode == tracking.Exact {
		table, mergeState, merge = s.exactTable, "uniqExactMergeState", "uniqExactMerge"
	}
	hourly := `SELECT s.hour,s.project_id,s.point_id,s.point_code,s.point_version,s.property_code,s.property_value,s.version,
any(s.event_count) AS event_count,any(s.uv_state) AS uv_state
FROM ` + table + ` AS s
INNER JOIN ` + s.generationTable + ` AS g FINAL
ON s.project_id=g.project_id AND s.hour=g.hour AND s.version=g.generation
WHERE s.project_id=? AND s.hour>=? AND s.hour<? AND s.point_id=? AND s.property_code=?
GROUP BY s.hour,s.project_id,s.point_id,s.point_code,s.point_version,s.property_code,s.property_value,s.version`
	byValue := `SELECT toString(toDate(hour,'Asia/Shanghai')) AS date,point_id,any(point_code) AS point_code,property_code,property_value,
sum(event_count) AS event_count,` + mergeState + `(uv_state) AS uv_state
FROM (` + hourly + `) GROUP BY date,point_id,property_code,property_value`
	query := `SELECT date,point_id,point_code,property_code,property_value,event_count,` + merge + `(uv_state)
FROM (` + byValue + `) ORDER BY date DESC,event_count DESC,property_value`
	hasOverflowFlag := false
	if property.Type == tracking.String {
		ranked := `SELECT *,row_number() OVER (PARTITION BY date ORDER BY event_count DESC,property_value) AS value_rank FROM (` + byValue + `)`
		query = `SELECT date,point_id,any(point_code),property_code,if(value_rank<=50,property_value,'') AS property_bucket,
value_rank>50 AS is_overflow,sum(event_count),` + merge + `(uv_state)
FROM (` + ranked + `)
GROUP BY date,point_id,property_code,property_bucket,is_overflow ORDER BY date DESC,sum(event_count) DESC,is_overflow,property_bucket`
		hasOverflowFlag = true
	}
	rows, err := s.db.QueryContext(ctx, query, filter.ProjectID, filter.Start.UTC(), filter.End.UTC(), property.PointID, property.PropertyCode)
	if err != nil {
		return nil, fmt.Errorf("query dimension property %s: %w", property.PropertyCode, err)
	}
	defer rows.Close()
	var result []reporting.DimensionAggregate
	for rows.Next() {
		var item reporting.DimensionAggregate
		destinations := []any{&item.Date, &item.PointID, &item.PointCode, &item.PropertyCode, &item.Value}
		var overflow bool
		if hasOverflowFlag {
			destinations = append(destinations, &overflow)
		}
		destinations = append(destinations, &item.EventCount, &item.UV)
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan dimension property %s: %w", property.PropertyCode, err)
		}
		if overflow {
			item.Value = "其他"
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
