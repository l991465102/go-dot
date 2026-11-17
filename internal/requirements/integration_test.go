package requirements

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

// TestMySQLWorkflow 使用独立测试数据库验证需求、任务及审计的事务行为。
func TestMySQLWorkflow(t *testing.T) {
	dsn := os.Getenv("GO_DOT_REQUIREMENTS_TEST_DSN")
	if dsn == "" {
		t.Skip("set GO_DOT_REQUIREMENTS_TEST_DSN to an isolated migrated database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
	user := exec(`INSERT INTO user_account (username,password_hash) VALUES (UUID(),'unused')`)
	p := exec(`INSERT INTO project (code,name,write_key_hash,allowed_origins,visitor_salt,created_by) VALUES (UUID(),'test',SHA2(UUID(),256),JSON_ARRAY(),'salt',?)`, user)
	exec(`INSERT INTO project_member (project_id,user_id,role) VALUES (?,?,'ADMIN')`, p, user)
	cat := exec(`INSERT INTO tracking_category (project_id,code,name,level,created_by) VALUES (?,UUID(),'scene','SCENE',?)`, p, user)
	point := exec(`INSERT INTO tracking_point (project_id,category_id,code,name,behavior_type,supported_platforms,supported_channels,created_by,updated_by) VALUES (?,?,UUID(),'test','CUSTOM',JSON_ARRAY('web','server'),JSON_ARRAY(),?,?)`, p, cat, user, user)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM audit_log WHERE project_id=?`, p)
		db.Exec(`DELETE a FROM tracking_task_acceptance a JOIN tracking_requirement_task t ON t.id=a.task_id JOIN tracking_requirement r ON r.id=t.requirement_id WHERE r.project_id=?`, p)
		db.Exec(`DELETE t FROM tracking_requirement_task t JOIN tracking_requirement r ON r.id=t.requirement_id WHERE r.project_id=?`, p)
		db.Exec(`DELETE FROM tracking_requirement WHERE project_id=?`, p)
		db.Exec(`DELETE FROM tracking_point WHERE project_id=?`, p)
		db.Exec(`DELETE FROM tracking_category WHERE project_id=?`, p)
		db.Exec(`DELETE FROM project_member WHERE project_id=?`, p)
		db.Exec(`DELETE FROM project WHERE id=?`, p)
		db.Exec(`DELETE FROM user_account WHERE id=?`, user)
	})
	s := NewService(db, allow{})
	rid, err := s.Create(ctx, uint64(user), uint64(p), Input{Title: "转化需求", DueDate: "2026-12-01"})
	if err != nil {
		t.Fatal(err)
	}
	assignee := uint64(user)
	input := TaskInput{PointID: uint64(point), Platform: "web", AssigneeID: &assignee, State: "PENDING"}
	if err = s.SaveTask(ctx, uint64(user), uint64(p), rid, 0, input); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Tasks(ctx, uint64(user), uint64(p), rid)
	if err != nil || len(tasks) != 1 || tasks[0].PointName != "test" {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	input.State = "TESTING"
	if err = s.SaveTask(ctx, uint64(user), uint64(p), rid, tasks[0].ID, input); err != nil {
		t.Fatal(err)
	}
	tasks, err = s.Tasks(ctx, uint64(user), uint64(p), rid)
	if err != nil || tasks[0].State != "TESTING" {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	if err = s.Update(ctx, uint64(user), uint64(p), rid, Input{Title: "更新需求"}); err != nil {
		t.Fatal(err)
	}
	if err = s.AddAcceptance(ctx, uint64(user), uint64(p), rid, tasks[0].ID, AcceptanceInput{Result: "PASS", Note: "已验证"}); err != nil {
		t.Fatal(err)
	}
	acceptances, err := s.ListAcceptances(ctx, uint64(user), uint64(p), rid, tasks[0].ID)
	if err != nil || len(acceptances) != 1 || acceptances[0].Result != "PASS" {
		t.Fatalf("acceptances=%+v err=%v", acceptances, err)
	}
	if err = s.DeleteTask(ctx, uint64(user), uint64(p), rid, tasks[0].ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleting accepted task: %v", err)
	}
	input.Platform = "server"
	if err = s.SaveTask(ctx, uint64(user), uint64(p), rid, tasks[0].ID, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("changing task platform: %v", err)
	}
	if err = s.SaveTask(ctx, uint64(user), uint64(p), rid, 0, input); err != nil {
		t.Fatal(err)
	}
	tasks, err = s.Tasks(ctx, uint64(user), uint64(p), rid)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	if err = s.DeleteTask(ctx, uint64(user), uint64(p), rid, tasks[1].ID); err != nil {
		t.Fatal(err)
	}
	input.Platform = "ios"
	if err = s.SaveTask(ctx, uint64(user), uint64(p), rid, 0, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported platform: %v", err)
	}
	if _, err = s.Tasks(ctx, uint64(user), uint64(p)+100000, rid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross project: %v", err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE project_id=?`, p).Scan(&count); err != nil || count != 7 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
	for i := 0; i < 101; i++ {
		exec(`INSERT INTO tracking_requirement(project_id,title,goal,scenario,requirement_url,created_by) VALUES (?,'分页测试','','','',?)`, p, user)
	}
	first, err := s.List(ctx, uint64(user), uint64(p))
	if err != nil || len(first) != 100 {
		t.Fatalf("first page length=%d err=%v", len(first), err)
	}
	second, err := s.List(ctx, uint64(user), uint64(p), first[len(first)-1].ID)
	if err != nil || len(second) != 2 || second[len(second)-1].ID != rid {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
}
