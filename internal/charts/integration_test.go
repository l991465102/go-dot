package charts

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/lzw7758/go-dot/internal/project"
)

type allow struct{}

func (allow) RequireRole(context.Context, uint64, uint64, ...project.Role) error { return nil }

// TestMySQLCharts 验证保存、读取、跨项目限制和审计。
func TestMySQLCharts(t *testing.T) {
	dsn := os.Getenv("GO_DOT_REQUIREMENTS_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated migrated MySQL")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, e := db.ExecContext(ctx, q, args...)
		if e != nil {
			t.Fatal(e)
		}
		id, e := r.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	user := exec(`INSERT INTO user_account(username,password_hash) VALUES (UUID(),'unused')`)
	p := exec(`INSERT INTO project(code,name,write_key_hash,allowed_origins,visitor_salt,created_by) VALUES (UUID(),'test',SHA2(UUID(),256),JSON_ARRAY(),'salt',?)`, user)
	cat := exec(`INSERT INTO tracking_category(project_id,code,name,level,created_by) VALUES (?,UUID(),'scene','SCENE',?)`, p, user)
	for _, code := range []string{"page_view", "order_submit"} {
		exec(`INSERT INTO tracking_point(project_id,category_id,code,name,behavior_type,definition_state,supported_platforms,supported_channels,created_by,updated_by) VALUES (?,?,?,?,'CUSTOM','PUBLISHED',JSON_ARRAY('web'),JSON_ARRAY(),?,?)`, p, cat, code, code, user, user)
	}
	defer func() {
		db.Exec(`DELETE FROM audit_log WHERE project_id=?`, p)
		db.Exec(`DELETE FROM saved_chart WHERE project_id=?`, p)
		db.Exec(`DELETE FROM tracking_point WHERE project_id=?`, p)
		db.Exec(`DELETE FROM tracking_category WHERE project_id=?`, p)
		db.Exec(`DELETE FROM project WHERE id=?`, p)
		db.Exec(`DELETE FROM user_account WHERE id=?`, user)
	}()
	s := NewService(db, allow{})
	in := Input{Name: "下单漏斗", Kind: "FUNNEL", Config: Config{PointCodes: []string{"page_view", "order_submit"}, Start: "2026-01-01T00:00:00Z", End: "2026-01-02T00:00:00Z", WindowHours: 24}}
	id, err := s.Create(ctx, uint64(user), uint64(p), in)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(ctx, uint64(user), uint64(p), "FUNNEL")
	if err != nil || len(rows) != 1 || rows[0].Config.PointCodes[1] != "order_submit" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if err = s.Delete(ctx, uint64(user), uint64(p)+100000, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project delete: %v", err)
	}
	if err = s.Delete(ctx, uint64(user), uint64(p), id); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE project_id=?`, p).Scan(&count); err != nil || count != 2 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
}
