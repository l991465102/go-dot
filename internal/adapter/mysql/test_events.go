package mysql

import (
	"context"
	"database/sql"
	"time"

	"github.com/lzw7758/go-dot/internal/ingestion"
)

type TestEventStore struct{ db *sql.DB }

// NewTestEventStore 创建测试事件存储。
func NewTestEventStore(db *sql.DB) *TestEventStore { return &TestEventStore{db: db} }

// SaveTestRecords 原子保存脱敏校验结果，并清理过期记录。
func (s *TestEventStore) SaveTestRecords(ctx context.Context, projectID uint64, records []ingestion.TestRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range records {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tracking_test_event (project_id,point_code,platform,app_version,status,error_code,received_at) VALUES (?,?,?,?,?,?,?)`, projectID, r.PointCode, r.Platform, r.AppVersion, r.Status, r.Code, r.ReceivedAt); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tracking_test_event WHERE received_at < ? LIMIT 1000`, time.Now().UTC().Add(-7*24*time.Hour)); err != nil {
		return err
	}
	return tx.Commit()
}

// RecentTestRecords 返回项目内点位最近七天的脱敏校验结果。
func (s *TestEventStore) RecentTestRecords(ctx context.Context, projectID uint64, pointCode string) ([]ingestion.TestRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT point_code,platform,app_version,status,error_code,received_at FROM tracking_test_event WHERE project_id=? AND point_code=? AND received_at>=? ORDER BY received_at DESC,id DESC LIMIT 100`, projectID, pointCode, time.Now().UTC().Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ingestion.TestRecord{}
	for rows.Next() {
		var r ingestion.TestRecord
		if err = rows.Scan(&r.PointCode, &r.Platform, &r.AppVersion, &r.Status, &r.Code, &r.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
