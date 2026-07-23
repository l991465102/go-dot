package mysql

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/reporting"
	"github.com/lzw7758/go-dot/internal/tracking"
)

func TestReportRepositoryCreateCommitsReportOrderedPointsAndAuditTogether(t *testing.T) {
	createdAt := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Millisecond)
	script, db := openScriptedDB(t, []driver.Value{createdAt, updatedAt})
	script.execResults = []driver.Result{
		scriptResult{lastID: 12, affected: 1},
		scriptResult{affected: 1},
		scriptResult{affected: 1},
		scriptResult{affected: 1},
	}

	created, err := NewReportRepository(db).Create(context.Background(), 9, 3, reporting.ReportInput{
		Name: "Conversion", Description: "Daily conversion", DefaultDays: 30, PointIDs: []uint64{7, 5},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(12), created.ID)
	require.Equal(t, uint64(9), created.ProjectID)
	require.Equal(t, createdAt, created.CreatedAt)
	require.Equal(t, updatedAt, created.UpdatedAt)
	require.Len(t, script.execQueries, 4)
	require.Contains(t, script.execQueries[0], "INSERT INTO custom_report")
	require.Contains(t, script.execQueries[1], "INSERT INTO custom_report_point")
	require.Contains(t, script.execQueries[1], "p.project_id=?")
	require.Contains(t, script.execQueries[1], "definition_state IN ('PUBLISHED','DEPRECATED')")
	require.Contains(t, script.execQueries[2], "INSERT INTO custom_report_point")
	require.Contains(t, script.execQueries[3], "audit_log")
	require.EqualValues(t, 0, script.execArgs[1][1].Value)
	require.EqualValues(t, 7, script.execArgs[1][2].Value)
	require.EqualValues(t, 9, script.execArgs[1][3].Value)
	require.EqualValues(t, 1, script.execArgs[2][1].Value)
	require.EqualValues(t, 5, script.execArgs[2][2].Value)
	require.EqualValues(t, 9, script.execArgs[2][3].Value)
	require.Equal(t, 1, script.commits)
	require.Zero(t, script.rollbacks)
}

func TestReportRepositoryCreateRejectsCrossProjectPointAndRollsBack(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.execResults = []driver.Result{
		scriptResult{lastID: 12, affected: 1},
		scriptResult{affected: 0},
	}

	_, err := NewReportRepository(db).Create(context.Background(), 9, 3, reporting.ReportInput{
		Name: "Conversion", DefaultDays: 30, PointIDs: []uint64{77},
	})
	require.ErrorIs(t, err, reporting.ErrInvalid)
	require.Len(t, script.execQueries, 2)
	require.Contains(t, script.execQueries[1], "p.project_id=?")
	require.Contains(t, script.execQueries[1], "definition_state IN ('PUBLISHED','DEPRECATED')")
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)
}

func TestReportRepositoryCreateMapsDuplicateNameToConflict(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.execErrors = []error{&mysqldriver.MySQLError{Number: 1062, Message: "duplicate name"}}

	_, err := NewReportRepository(db).Create(context.Background(), 9, 3, reporting.ReportInput{Name: "Conversion"})
	require.ErrorIs(t, err, reporting.ErrConflict)
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)
}

func TestReportRepositoryUpdateUsesOptimisticTimestampAndMapsNoMatchToConflict(t *testing.T) {
	expected := time.Date(2026, 7, 20, 8, 30, 0, 123000000, time.UTC)
	script, db := openScriptedDB(t, nil)
	script.execResults = []driver.Result{scriptResult{affected: 0}}

	_, err := NewReportRepository(db).Update(context.Background(), 9, 12, 3, reporting.ReportInput{
		Name: "Conversion", DefaultDays: 14, ExpectedUpdatedAt: &expected,
	})
	require.ErrorIs(t, err, reporting.ErrConflict)
	require.Len(t, script.execQueries, 1)
	query := strings.Join(strings.Fields(script.execQueries[0]), " ")
	require.Contains(t, query, "updated_at=GREATEST(UTC_TIMESTAMP(3), TIMESTAMPADD(MICROSECOND,1000,updated_at))")
	require.Contains(t, query, "WHERE id=? AND project_id=? AND updated_at=?")
	require.Equal(t, expected, script.execArgs[0][6].Value)
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)
}

func TestReportRepositoryUpdateReplacesMappingsAndAuditsInOneTransaction(t *testing.T) {
	expected := time.Date(2026, 7, 20, 8, 30, 0, 123000000, time.UTC)
	createdAt := expected.Add(-time.Hour)
	updatedAt := expected.Add(time.Millisecond)
	script, db := openScriptedDB(t, []driver.Value{uint64(2), createdAt, updatedAt})
	script.execResults = []driver.Result{
		scriptResult{affected: 1},
		scriptResult{affected: 2},
		scriptResult{affected: 0},
		scriptResult{affected: 1},
		scriptResult{affected: 1},
		scriptResult{affected: 1},
	}

	updated, err := NewReportRepository(db).Update(context.Background(), 9, 12, 3, reporting.ReportInput{
		Name: "Conversion v2", DefaultDays: 14, PointIDs: []uint64{7, 5}, ExpectedUpdatedAt: &expected,
	})
	require.NoError(t, err)
	require.Equal(t, updatedAt, updated.UpdatedAt)
	require.Len(t, script.execQueries, 6)
	require.Contains(t, script.execQueries[1], "DELETE rp FROM custom_report_point")
	require.Contains(t, script.execQueries[2], "DELETE cp FROM custom_report_property")
	for _, index := range []int{3, 4} {
		require.Contains(t, script.execQueries[index], "p.project_id=?")
		require.Contains(t, script.execQueries[index], "definition_state IN ('PUBLISHED','DEPRECATED')")
	}
	require.Contains(t, script.execQueries[5], "audit_log")
	require.EqualValues(t, 9, script.execArgs[5][0].Value)
	require.EqualValues(t, 3, script.execArgs[5][1].Value)
	require.Equal(t, "UPDATE_REPORT", script.execArgs[5][2].Value)
	require.Equal(t, "CUSTOM_REPORT", script.execArgs[5][3].Value)
	require.EqualValues(t, 12, script.execArgs[5][4].Value)
	require.Equal(t, 1, script.commits)
	require.Zero(t, script.rollbacks)
}

func TestReportRepositoryResolvePointsFiltersDeduplicatesAndRestoresInputOrder(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRowSets = [][][]driver.Value{{
		{uint64(7), "checkout", "Checkout", "PUBLISHED", "EXACT"},
		{uint64(5), "view", "View", "DEPRECATED", "APPROXIMATE"},
	}}

	points, err := NewReportRepository(db).ResolvePoints(context.Background(), 9, []uint64{5, 7, 5, 8})
	require.NoError(t, err)
	require.Equal(t, []uint64{5, 7}, []uint64{points[0].ID, points[1].ID})
	require.Equal(t, []int{0, 1}, []int{points[0].SortNo, points[1].SortNo})
	require.Len(t, script.queries, 1)
	require.Contains(t, script.queries[0], "project_id=?")
	require.Contains(t, script.queries[0], "definition_state IN ('PUBLISHED','DEPRECATED')")
}

func TestReportRepositoryResolvePropertiesRestoresSelectionOrderAndAnalytics(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.queryRowSets = [][][]driver.Value{{
		{uint64(7), "checkout", "EXACT", "status", "状态", "ENUM", []byte(`["success","fail"]`), "DIMENSION", nil},
		{uint64(7), "checkout", "EXACT", "amount", "金额", "DECIMAL", nil, "METRIC", []byte(`["COUNT","SUM","AVG"]`)},
	}}
	requested := []reporting.PropertySelection{
		{PointID: 7, PropertyCode: "amount", Aggregations: []tracking.MetricAggregation{tracking.AggregateSum}},
		{PointID: 7, PropertyCode: "status"},
	}

	properties, err := NewReportRepository(db).ResolveProperties(context.Background(), 9, requested)
	require.NoError(t, err)
	require.Equal(t, []string{"amount", "status"}, []string{properties[0].PropertyCode, properties[1].PropertyCode})
	require.Equal(t, tracking.AnalyticsMetric, properties[0].Analytics.Mode)
	require.Equal(t, []tracking.MetricAggregation{tracking.AggregateCount, tracking.AggregateSum, tracking.AggregateAvg}, properties[0].Analytics.Aggregations)
	require.Equal(t, []tracking.MetricAggregation{tracking.AggregateSum}, properties[0].Aggregations)
	require.Equal(t, []string{"success", "fail"}, properties[1].EnumValues)
	require.Contains(t, script.queries[0], "pp.code=?")
	require.NotContains(t, script.queries[0], "pp.property_code")
}

func TestReportRepositoryGetScopesReportAndPointReadsToProject(t *testing.T) {
	created := time.Date(2026, 7, 20, 7, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{
		{uint64(12), uint64(9), "Conversion", "Daily conversion", int64(30), uint64(3), uint64(4), created, updated},
		{uint64(7), "checkout", "Checkout", "PUBLISHED", "EXACT", int64(0)},
	}

	report, err := NewReportRepository(db).Get(context.Background(), 9, 12)
	require.NoError(t, err)
	require.Equal(t, uint64(12), report.ID)
	require.Len(t, report.Points, 1)
	require.Len(t, script.queries, 3)
	for _, query := range script.queries {
		require.Contains(t, query, "project_id=?")
	}
	require.Contains(t, script.queries[0], "id=?")
	require.Contains(t, script.queries[1], "report_id=?")
}

func TestReportRepositoryGetMapsMissingRowToNotFound(t *testing.T) {
	_, db := openScriptedDB(t, nil)
	_, err := NewReportRepository(db).Get(context.Background(), 9, 12)
	require.ErrorIs(t, err, reporting.ErrNotFound)
}

func TestReportRepositoryDeleteScopesMutationToProjectAndCommits(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.execResults = []driver.Result{scriptResult{affected: 1}}

	err := NewReportRepository(db).Delete(context.Background(), 9, 12)
	require.NoError(t, err)
	require.Len(t, script.execQueries, 1)
	require.Contains(t, script.execQueries[0], "project_id=?")
	require.Contains(t, script.execQueries[0], "id=?")
	require.EqualValues(t, 12, script.execArgs[0][0].Value)
	require.EqualValues(t, 9, script.execArgs[0][1].Value)
	require.Equal(t, 1, script.commits)
}

func TestReportRepositoryDeleteMissingRowIsNotFound(t *testing.T) {
	script, db := openScriptedDB(t, nil)
	script.execResults = []driver.Result{scriptResult{affected: 0}}

	err := NewReportRepository(db).Delete(context.Background(), 9, 12)
	require.True(t, errors.Is(err, reporting.ErrNotFound))
	require.Equal(t, 1, script.rollbacks)
}
