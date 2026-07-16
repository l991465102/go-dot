package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/lzw7758/go-dot/internal/ports"
)

type StatisticsExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type StatisticsStore struct {
	db                                                                          StatisticsExecutor
	rawTable, invalidEventsTable, approximateTable, exactTable, generationTable string
}

func NewStatisticsStore(db StatisticsExecutor) *StatisticsStore {
	return newStatisticsStoreWithTables(db, "tracking_event", "business_stat_hourly", "business_stat_exact_hourly", "business_stat_hour_generation")
}
func newStatisticsStoreWithTables(db StatisticsExecutor, raw, approximate, exact, generation string) *StatisticsStore {
	return newStatisticsStoreWithAllTables(db, raw, "tracking_invalid_event", approximate, exact, generation)
}
func newStatisticsStoreWithAllTables(db StatisticsExecutor, raw, invalid, approximate, exact, generation string) *StatisticsStore {
	return &StatisticsStore{db: db, rawTable: raw, invalidEventsTable: invalid, approximateTable: approximate, exactTable: exact, generationTable: generation}
}

func (s *StatisticsStore) RefreshHour(ctx context.Context, hour time.Time, version uint64) error {
	return s.refresh(ctx, 0, hour, version)
}
func (s *StatisticsStore) RefreshProjectHour(ctx context.Context, projectID uint64, hour time.Time, version uint64) error {
	if projectID == 0 {
		return errors.New("project id is required")
	}
	return s.refresh(ctx, projectID, hour, version)
}

const deduplicatedEventsTemplate = `
SELECT
  project_id, event_id,
  argMax(domain_category_id, version) AS domain_category_id,
  argMax(domain_category_code, version) AS domain_category_code,
  argMax(module_category_id, version) AS module_category_id,
  argMax(module_category_code, version) AS module_category_code,
  argMax(scene_category_id, version) AS scene_category_id,
  argMax(scene_category_code, version) AS scene_category_code,
  argMax(point_id, version) AS point_id,
  argMax(point_code, version) AS point_code,
  argMax(platform, version) AS platform,
  argMax(channel, version) AS channel,
  argMax(visitor_hash, version) AS visitor_hash,
  argMax(count_pv, version) AS count_pv,
  argMax(count_uv, version) AS count_uv,
  argMax(uv_mode, version) AS uv_mode
FROM %s FINAL
WHERE (? = 0 OR project_id = ?) AND statistic_at >= ? AND statistic_at < ?
GROUP BY project_id, event_id`

func (s *StatisticsStore) refresh(ctx context.Context, projectID uint64, hour time.Time, version uint64) error {
	if s == nil || s.db == nil {
		return errors.New("ClickHouse statistics executor is required")
	}
	if hour.IsZero() || version == 0 {
		return errors.New("hour and version are required")
	}
	if !identifierPattern.MatchString(s.rawTable) || !identifierPattern.MatchString(s.approximateTable) || !identifierPattern.MatchString(s.exactTable) || !identifierPattern.MatchString(s.generationTable) {
		return errors.New("invalid ClickHouse statistics table name")
	}
	hour = hour.UTC()
	writeCtx := ch.Context(ctx, ch.WithSettings(statisticsRefreshSettings()))
	deduplicatedEvents := fmt.Sprintf(deduplicatedEventsTemplate, s.rawTable)
	base := `INSERT INTO ` + s.approximateTable + ` (hour,project_id,domain_category_id,domain_category_code,module_category_id,module_category_code,scene_category_id,scene_category_code,point_id,point_code,platform,channel,pv,uv_state,version)
SELECT ?, project_id, domain_category_id, domain_category_code, module_category_id, module_category_code, scene_category_id, scene_category_code, point_id, point_code, platform, channel,
 sum(toUInt64(count_pv)) AS pv, uniqCombined64StateIf(visitor_hash, count_uv) AS uv_state, ? AS version
FROM (` + deduplicatedEvents + `)
GROUP BY project_id, domain_category_id, domain_category_code, module_category_id, module_category_code, scene_category_id, scene_category_code, point_id, point_code, platform, channel`
	args := []any{hour, version, projectID, projectID, hour, hour.Add(time.Hour)}
	if projectID != 0 {
		args = []any{hour, version, projectID, hour, hour.Add(time.Hour)}
		base = bindRefreshQuery(base)
	}
	if _, err := s.db.ExecContext(writeCtx, base, args...); err != nil {
		return fmt.Errorf("refresh approximate hourly statistics: %w", err)
	}
	exact := strings.Replace(base, s.approximateTable, s.exactTable, 1)
	exact = strings.Replace(exact, "uniqCombined64StateIf", "uniqExactStateIf", 1)
	exact = strings.Replace(exact, "GROUP BY project_id, domain_category_id", "WHERE uv_mode = 'EXACT' GROUP BY project_id, domain_category_id", 1)
	if _, err := s.db.ExecContext(writeCtx, exact, args...); err != nil {
		return fmt.Errorf("refresh exact hourly statistics: %w", err)
	}
	markerQuery := `INSERT INTO ` + s.generationTable + ` (project_id,hour,generation,committed_at) VALUES (?,?,?,now64(3))`
	markerArgs := []any{projectID, hour, version}
	if projectID == 0 {
		markerQuery = `INSERT INTO ` + s.generationTable + ` (project_id,hour,generation,committed_at) SELECT DISTINCT project_id,?,?,now64(3) FROM ` + s.rawTable + ` FINAL WHERE statistic_at>=? AND statistic_at<?`
		markerArgs = []any{hour, version, hour, hour.Add(time.Hour)}
	}
	if _, err := s.db.ExecContext(writeCtx, markerQuery, markerArgs...); err != nil {
		return fmt.Errorf("commit hourly statistics generation: %w", err)
	}
	return nil
}

func statisticsRefreshSettings() ch.Settings {
	return ch.Settings{"async_insert": 0, "wait_for_async_insert": 1}
}

func bindRefreshQuery(query string) string {
	query = strings.Replace(query, "WHERE (? = 0 OR project_id = ?) AND statistic_at >= ? AND statistic_at < ?", "WHERE project_id = ? AND statistic_at >= ? AND statistic_at < ?", 1)
	return query
}

func (s *StatisticsStore) QuerySummary(ctx context.Context, filter ports.StatisticsFilter) (ports.Summary, error) {
	rows, err := s.query(ctx, filter, false)
	if err != nil {
		return ports.Summary{}, err
	}
	defer rows.Close()
	var out ports.Summary
	if rows.Next() {
		if err := rows.Scan(&out.PV, &out.UV); err != nil {
			return out, err
		}
	}
	out.Approximate = !filter.ExactUV
	return out, rows.Err()
}
func (s *StatisticsStore) QueryTrend(ctx context.Context, filter ports.StatisticsFilter) ([]ports.TrendPoint, error) {
	rows, err := s.query(ctx, filter, true)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ports.TrendPoint
	for rows.Next() {
		var p ports.TrendPoint
		if err := rows.Scan(&p.Period, &p.PV, &p.UV); err != nil {
			return nil, err
		}
		p.Approximate = !filter.ExactUV
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *StatisticsStore) query(ctx context.Context, f ports.StatisticsFilter, trend bool) (*sql.Rows, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("ClickHouse statistics executor is required")
	}
	if f.ProjectID == 0 || f.Start.IsZero() || !f.End.After(f.Start) {
		return nil, errors.New("project and valid time range are required")
	}
	table, merge := s.approximateTable, "uniqCombined64Merge(uv_state)"
	if f.ExactUV {
		table, merge = s.exactTable, "uniqExactMerge(uv_state)"
	}
	period := ""
	group := ""
	order := ""
	args := make([]any, 0, 10)
	if trend {
		period = "toStartOfHour(hour) AS period,"
		if f.Granularity == "day" {
			tz := f.Timezone
			if tz == "" {
				tz = "UTC"
			}
			period = "toStartOfDay(hour, ?) AS period,"
			args = append(args, tz)
		} else if f.Granularity != "" && f.Granularity != "hour" {
			return nil, errors.New("granularity must be hour or day")
		}
		group = " GROUP BY period"
		order = " ORDER BY period"
	}
	inner := `SELECT s.hour,s.project_id,s.domain_category_id,s.domain_category_code,s.module_category_id,s.module_category_code,s.scene_category_id,s.scene_category_code,s.point_id,s.point_code,s.platform,s.channel,s.version,any(s.pv) AS pv,any(s.uv_state) AS uv_state FROM ` + table + ` AS s INNER JOIN ` + s.generationTable + ` AS g FINAL ON s.project_id = g.project_id AND s.hour = g.hour AND s.version = g.generation WHERE s.project_id=? AND s.hour>=? AND s.hour<?`
	args = append(args, f.ProjectID, f.Start.UTC(), f.End.UTC())
	for _, item := range []struct {
		col   string
		value any
		set   bool
	}{{"domain_category_id", f.DomainCategoryID, f.DomainCategoryID != 0}, {"module_category_id", f.ModuleCategoryID, f.ModuleCategoryID != 0}, {"scene_category_id", f.SceneCategoryID, f.SceneCategoryID != 0}, {"point_id", f.PointID, f.PointID != 0}, {"point_code", f.PointCode, f.PointCode != ""}, {"platform", f.Platform, f.Platform != ""}, {"channel", f.Channel, f.Channel != ""}} {
		if item.set {
			inner += " AND s." + item.col + "=?"
			args = append(args, item.value)
		}
	}
	if len(f.PointCodes) > 0 {
		inner += " AND s.point_code IN " + placeholders(len(f.PointCodes))
		for _, code := range f.PointCodes {
			args = append(args, code)
		}
	}
	inner += ` GROUP BY s.hour,s.project_id,s.domain_category_id,s.domain_category_code,s.module_category_id,s.module_category_code,s.scene_category_id,s.scene_category_code,s.point_id,s.point_code,s.platform,s.channel,s.version`
	q := `SELECT ` + period + ` sum(pv), ` + merge + ` FROM (` + inner + `)` + group + order
	return s.db.QueryContext(ctx, q, args...)
}
