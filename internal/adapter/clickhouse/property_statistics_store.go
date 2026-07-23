package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

type PropertyStatisticsStore struct {
	db                                                                   StatisticsExecutor
	rawTable, metricTable, approximateTable, exactTable, generationTable string
}

func (s *PropertyStatisticsStore) ConfigureRetention(ctx context.Context, days int) error {
	if s == nil || s.db == nil {
		return errors.New("ClickHouse property statistics executor is required")
	}
	if days < 90 {
		return errors.New("property aggregate retention must be at least 90 days")
	}
	for _, table := range []string{s.metricTable, s.approximateTable, s.exactTable, s.generationTable} {
		if !identifierPattern.MatchString(table) {
			return errors.New("invalid ClickHouse property statistics table name")
		}
		query := `ALTER TABLE ` + table + ` MODIFY TTL hour + INTERVAL ` + strconv.Itoa(days) + ` DAY DELETE`
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("configure property aggregate retention for %s: %w", table, err)
		}
	}
	return nil
}

func NewPropertyStatisticsStore(db StatisticsExecutor) *PropertyStatisticsStore {
	return newPropertyStatisticsStoreWithTables(
		db,
		"tracking_property_event",
		"property_metric_hourly",
		"property_dimension_hourly_approximate",
		"property_dimension_hourly_exact",
		"property_stat_hour_generation",
	)
}

func newPropertyStatisticsStoreWithTables(db StatisticsExecutor, raw, metric, approximate, exact, generation string) *PropertyStatisticsStore {
	return &PropertyStatisticsStore{
		db: db, rawTable: raw, metricTable: metric, approximateTable: approximate,
		exactTable: exact, generationTable: generation,
	}
}

const deduplicatedPropertyEventsTemplate = `
SELECT
  project_id, event_id, property_code,
  argMax(point_id, version) AS point_id,
  argMax(point_code, version) AS point_code,
  argMax(point_version, version) AS point_version,
  argMax(property_type, version) AS property_type,
  argMax(value_string, version) AS value_string,
  argMax(value_decimal, version) AS value_decimal,
  argMax(visitor_hash, version) AS visitor_hash,
  argMax(uv_mode, version) AS uv_mode
FROM %s FINAL
WHERE project_id = ? AND statistic_at >= ? AND statistic_at < ?
GROUP BY project_id, event_id, property_code`

func (s *PropertyStatisticsStore) RefreshProjectHour(ctx context.Context, projectID uint64, hour time.Time, version uint64) error {
	if s == nil || s.db == nil {
		return errors.New("ClickHouse property statistics executor is required")
	}
	if projectID == 0 || hour.IsZero() || version == 0 {
		return errors.New("project, hour and version are required")
	}
	for _, table := range []string{s.rawTable, s.metricTable, s.approximateTable, s.exactTable, s.generationTable} {
		if !identifierPattern.MatchString(table) {
			return errors.New("invalid ClickHouse property statistics table name")
		}
	}
	hour = hour.UTC()
	writeCtx := ch.Context(ctx, ch.WithSettings(statisticsRefreshSettings()))
	deduplicated := fmt.Sprintf(deduplicatedPropertyEventsTemplate, s.rawTable)
	baseArgs := []any{hour, version, projectID, hour, hour.Add(time.Hour)}

	metric := `INSERT INTO ` + s.metricTable + ` (hour,project_id,point_id,point_code,point_version,property_code,value_count,value_sum,value_min,value_max,version)
SELECT ?, project_id, point_id, point_code, point_version, property_code,
 count() AS value_count, sum(value_decimal) AS value_sum, min(value_decimal) AS value_min, max(value_decimal) AS value_max, ? AS version
FROM (` + deduplicated + `)
WHERE property_type IN ('INTEGER','DECIMAL') AND value_decimal IS NOT NULL
GROUP BY project_id,point_id,point_code,point_version,property_code`
	if _, err := s.db.ExecContext(writeCtx, metric, baseArgs...); err != nil {
		return fmt.Errorf("refresh metric property statistics: %w", err)
	}

	dimension := `INSERT INTO ` + s.approximateTable + ` (hour,project_id,point_id,point_code,point_version,property_code,property_value,event_count,uv_state,version)
SELECT ?, project_id, point_id, point_code, point_version, property_code, value_string,
 count() AS event_count, uniqCombined64State(visitor_hash) AS uv_state, ? AS version
FROM (` + deduplicated + `)
WHERE property_type IN ('STRING','ENUM','BOOLEAN')
GROUP BY project_id,point_id,point_code,point_version,property_code,value_string`
	if _, err := s.db.ExecContext(writeCtx, dimension, baseArgs...); err != nil {
		return fmt.Errorf("refresh approximate dimension property statistics: %w", err)
	}
	exact := strings.Replace(dimension, s.approximateTable, s.exactTable, 1)
	exact = strings.Replace(exact, "uniqCombined64State", "uniqExactState", 1)
	exact = strings.Replace(exact, "WHERE property_type IN", "WHERE uv_mode = 'EXACT' AND property_type IN", 1)
	if _, err := s.db.ExecContext(writeCtx, exact, baseArgs...); err != nil {
		return fmt.Errorf("refresh exact dimension property statistics: %w", err)
	}

	marker := `INSERT INTO ` + s.generationTable + ` (project_id,hour,generation,committed_at) VALUES (?,?,?,now64(3))`
	if _, err := s.db.ExecContext(writeCtx, marker, projectID, hour, version); err != nil {
		return fmt.Errorf("commit property statistics generation: %w", err)
	}
	return nil
}
