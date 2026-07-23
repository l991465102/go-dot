package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/lzw7758/go-dot/internal/reporting"
)

var _ reporting.Repository = (*ReportRepository)(nil)

type ReportRepository struct{ db *sql.DB }

func NewReportRepository(db *sql.DB) *ReportRepository { return &ReportRepository{db: db} }

func (r *ReportRepository) List(ctx context.Context, projectID uint64) ([]reporting.Report, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,project_id,name,description,default_days,created_by,updated_by,created_at,updated_at FROM custom_report WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]reporting.Report, 0)
	for rows.Next() {
		var item reporting.Report
		if err := scanReport(rows, &item); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Points, err = loadReportPoints(ctx, r.db, projectID, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Properties, err = loadReportProperties(ctx, r.db, projectID, items[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *ReportRepository) Get(ctx context.Context, projectID, reportID uint64) (reporting.Report, error) {
	var report reporting.Report
	err := scanReport(r.db.QueryRowContext(ctx, `SELECT id,project_id,name,description,default_days,created_by,updated_by,created_at,updated_at FROM custom_report WHERE project_id=? AND id=?`, projectID, reportID), &report)
	if errors.Is(err, sql.ErrNoRows) {
		return report, reporting.ErrNotFound
	}
	if err != nil {
		return report, err
	}
	report.Points, err = loadReportPoints(ctx, r.db, projectID, reportID)
	if err == nil {
		report.Properties, err = loadReportProperties(ctx, r.db, projectID, reportID)
	}
	return report, err
}

func (r *ReportRepository) Create(ctx context.Context, projectID, actorID uint64, in reporting.ReportInput) (created reporting.Report, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return created, err
	}
	defer rollbackOnError(tx, &err)

	result, err := tx.ExecContext(ctx, `INSERT INTO custom_report(project_id,name,description,default_days,created_by,updated_by,created_at,updated_at) VALUES(?,?,?,?,?,?,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`, projectID, in.Name, in.Description, in.DefaultDays, actorID, actorID)
	if err != nil {
		return created, reportConflict(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return created, err
	}
	created = reporting.Report{
		ID: uint64(id), ProjectID: projectID, Name: in.Name, Description: in.Description,
		DefaultDays: in.DefaultDays, CreatedBy: actorID, UpdatedBy: actorID,
		Points: pointRefs(in.PointIDs),
	}
	if err = insertPointMappings(ctx, tx, projectID, created.ID, in.PointIDs); err != nil {
		return created, err
	}
	if err = insertPropertyMappings(ctx, tx, created.ID, in.PropertySelections); err != nil {
		return created, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT created_at,updated_at FROM custom_report WHERE id=? AND project_id=?`, created.ID, projectID).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
		return created, err
	}
	after, err := json.Marshal(created)
	if err != nil {
		return created, err
	}
	if err = insertAudit(ctx, tx, projectID, actorID, "CREATE_REPORT", "CUSTOM_REPORT", created.ID, nil, after); err != nil {
		return created, err
	}
	if err = tx.Commit(); err != nil {
		return created, err
	}
	return created, nil
}

func (r *ReportRepository) Update(ctx context.Context, projectID, reportID, actorID uint64, in reporting.ReportInput) (updated reporting.Report, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return updated, err
	}
	defer rollbackOnError(tx, &err)

	result, err := tx.ExecContext(ctx, `UPDATE custom_report
SET name=?, description=?, default_days=?, updated_by=?, updated_at=GREATEST(UTC_TIMESTAMP(3), TIMESTAMPADD(MICROSECOND,1000,updated_at))
WHERE id=? AND project_id=? AND updated_at=?`, in.Name, in.Description, in.DefaultDays, actorID, reportID, projectID, in.ExpectedUpdatedAt)
	if err != nil {
		return updated, reportConflict(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return updated, err
	}
	if affected == 0 {
		return updated, reporting.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `DELETE rp FROM custom_report_point rp JOIN custom_report r ON r.id=rp.report_id WHERE rp.report_id=? AND r.project_id=?`, reportID, projectID); err != nil {
		return updated, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE cp FROM custom_report_property cp JOIN custom_report r ON r.id=cp.report_id WHERE cp.report_id=? AND r.project_id=?`, reportID, projectID); err != nil {
		return updated, err
	}
	if err = insertPointMappings(ctx, tx, projectID, reportID, in.PointIDs); err != nil {
		return updated, err
	}
	if err = insertPropertyMappings(ctx, tx, reportID, in.PropertySelections); err != nil {
		return updated, err
	}
	updated = reporting.Report{
		ID: reportID, ProjectID: projectID, Name: in.Name, Description: in.Description,
		DefaultDays: in.DefaultDays, Points: pointRefs(in.PointIDs), UpdatedBy: actorID,
	}
	if err = tx.QueryRowContext(ctx, `SELECT created_by,created_at,updated_at FROM custom_report WHERE id=? AND project_id=?`, reportID, projectID).Scan(&updated.CreatedBy, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
		return updated, err
	}
	after, err := json.Marshal(updated)
	if err != nil {
		return updated, err
	}
	if err = insertAudit(ctx, tx, projectID, actorID, "UPDATE_REPORT", "CUSTOM_REPORT", reportID, nil, after); err != nil {
		return updated, err
	}
	if err = tx.Commit(); err != nil {
		return updated, err
	}
	return updated, nil
}

func (r *ReportRepository) ResolveProperties(ctx context.Context, projectID uint64, requested []reporting.PropertySelection) ([]reporting.PropertyRef, error) {
	if len(requested) == 0 {
		return []reporting.PropertyRef{}, nil
	}
	conditions := make([]string, 0, len(requested))
	args := []any{projectID}
	for _, selection := range requested {
		conditions = append(conditions, "(p.id=? AND pp.code=?)")
		args = append(args, selection.PointID, selection.PropertyCode)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.code,p.uv_mode,pp.code,pp.name,pp.data_type,
pp.enum_values,pp.analytics_mode,pp.analytics_aggregations
FROM tracking_point p JOIN tracking_point_property pp ON pp.point_id=p.id
WHERE p.project_id=? AND p.definition_state IN ('PUBLISHED','DEPRECATED') AND (`+strings.Join(conditions, " OR ")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[string]reporting.PropertyRef, len(requested))
	for rows.Next() {
		var ref reporting.PropertyRef
		var enumJSON, aggregationJSON []byte
		if err := rows.Scan(&ref.PointID, &ref.PointCode, &ref.UVMode, &ref.PropertyCode, &ref.PropertyName,
			&ref.Type, &enumJSON, &ref.Analytics.Mode, &aggregationJSON); err != nil {
			return nil, err
		}
		if len(enumJSON) > 0 {
			if err := json.Unmarshal(enumJSON, &ref.EnumValues); err != nil {
				return nil, err
			}
		}
		if len(aggregationJSON) > 0 {
			if err := json.Unmarshal(aggregationJSON, &ref.Analytics.Aggregations); err != nil {
				return nil, err
			}
		}
		found[fmt.Sprintf("%d:%s", ref.PointID, ref.PropertyCode)] = ref
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	resolved := make([]reporting.PropertyRef, 0, len(requested))
	for i, selection := range requested {
		if ref, ok := found[fmt.Sprintf("%d:%s", selection.PointID, selection.PropertyCode)]; ok {
			ref.PropertySelection = selection
			ref.SortNo = i
			resolved = append(resolved, ref)
		}
	}
	return resolved, nil
}

func (r *ReportRepository) Delete(ctx context.Context, projectID, reportID uint64) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackOnError(tx, &err)
	result, err := tx.ExecContext(ctx, `DELETE FROM custom_report WHERE id=? AND project_id=?`, reportID, projectID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return reporting.ErrNotFound
	}
	return tx.Commit()
}

func (r *ReportRepository) ResolvePoints(ctx context.Context, projectID uint64, requested []uint64) ([]reporting.PointRef, error) {
	ids := uniqueIDs(requested)
	if len(ids) == 0 {
		return []reporting.PointRef{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, projectID)
	for _, id := range ids {
		args = append(args, id)
	}
	query := fmt.Sprintf(`SELECT id,code,name,definition_state,uv_mode FROM tracking_point WHERE project_id=? AND id IN (%s) AND definition_state IN ('PUBLISHED','DEPRECATED')`, placeholders)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[uint64]reporting.PointRef, len(ids))
	for rows.Next() {
		var point reporting.PointRef
		if err := rows.Scan(&point.ID, &point.Code, &point.Name, &point.DefinitionState, &point.UVMode); err != nil {
			return nil, err
		}
		byID[point.ID] = point
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	resolved := make([]reporting.PointRef, 0, len(byID))
	for _, id := range ids {
		if point, ok := byID[id]; ok {
			point.SortNo = len(resolved)
			resolved = append(resolved, point)
		}
	}
	return resolved, nil
}

type reportScanner interface{ Scan(...any) error }

func scanReport(scanner reportScanner, report *reporting.Report) error {
	return scanner.Scan(&report.ID, &report.ProjectID, &report.Name, &report.Description, &report.DefaultDays, &report.CreatedBy, &report.UpdatedBy, &report.CreatedAt, &report.UpdatedAt)
}

func loadReportPoints(ctx context.Context, q queryer, projectID, reportID uint64) ([]reporting.PointRef, error) {
	rows, err := q.QueryContext(ctx, `SELECT p.id,p.code,p.name,p.definition_state,p.uv_mode,rp.sort_no FROM custom_report_point rp JOIN custom_report r ON r.id=rp.report_id JOIN tracking_point p ON p.id=rp.point_id AND p.project_id=r.project_id WHERE r.project_id=? AND rp.report_id=? ORDER BY rp.sort_no`, projectID, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]reporting.PointRef, 0)
	for rows.Next() {
		var point reporting.PointRef
		if err := rows.Scan(&point.ID, &point.Code, &point.Name, &point.DefinitionState, &point.UVMode, &point.SortNo); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

func loadReportProperties(ctx context.Context, q queryer, projectID, reportID uint64) ([]reporting.PropertyRef, error) {
	rows, err := q.QueryContext(ctx, `SELECT p.id,p.code,p.uv_mode,pp.code,pp.name,pp.data_type,
pp.enum_values,pp.analytics_mode,pp.analytics_aggregations,cp.aggregations,cp.sort_no
FROM custom_report_property cp
JOIN custom_report r ON r.id=cp.report_id
JOIN tracking_point p ON p.id=cp.point_id AND p.project_id=r.project_id
JOIN tracking_point_property pp ON pp.point_id=p.id AND pp.code=cp.property_code
WHERE r.project_id=? AND cp.report_id=? ORDER BY cp.sort_no`, projectID, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]reporting.PropertyRef, 0)
	for rows.Next() {
		var ref reporting.PropertyRef
		var enumJSON, allowedJSON, selectedJSON []byte
		if err := rows.Scan(&ref.PointID, &ref.PointCode, &ref.UVMode, &ref.PropertyCode, &ref.PropertyName,
			&ref.Type, &enumJSON, &ref.Analytics.Mode, &allowedJSON, &selectedJSON, &ref.SortNo); err != nil {
			return nil, err
		}
		if len(enumJSON) > 0 {
			if err := json.Unmarshal(enumJSON, &ref.EnumValues); err != nil {
				return nil, err
			}
		}
		if len(allowedJSON) > 0 {
			if err := json.Unmarshal(allowedJSON, &ref.Analytics.Aggregations); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(selectedJSON, &ref.Aggregations); err != nil {
			return nil, err
		}
		result = append(result, ref)
	}
	return result, rows.Err()
}

func insertPointMappings(ctx context.Context, tx *sql.Tx, projectID, reportID uint64, pointIDs []uint64) error {
	for sortNo, pointID := range pointIDs {
		result, err := tx.ExecContext(ctx, `INSERT INTO custom_report_point(report_id,point_id,sort_no)
SELECT ?,p.id,? FROM tracking_point p
WHERE p.id=? AND p.project_id=? AND p.definition_state IN ('PUBLISHED','DEPRECATED')`, reportID, sortNo, pointID, projectID)
		if err != nil {
			return reportConflict(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return reporting.ErrInvalid
		}
	}
	return nil
}

func insertPropertyMappings(ctx context.Context, tx *sql.Tx, reportID uint64, selections []reporting.PropertySelection) error {
	for sortNo, selection := range selections {
		aggregations, err := json.Marshal(selection.Aggregations)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO custom_report_property(report_id,point_id,property_code,aggregations,sort_no) VALUES(?,?,?,?,?)`,
			reportID, selection.PointID, selection.PropertyCode, aggregations, sortNo)
		if err != nil {
			return reportConflict(err)
		}
	}
	return nil
}

func pointRefs(ids []uint64) []reporting.PointRef {
	points := make([]reporting.PointRef, len(ids))
	for i, id := range ids {
		points[i] = reporting.PointRef{ID: id, SortNo: i}
	}
	return points
}

func uniqueIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	result := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func reportConflict(err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return reporting.ErrConflict
	}
	return err
}
