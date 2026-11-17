package mysql

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/lzw7758/go-dot/internal/ingestion"
)

// TestTestEventStoreMySQL 验证测试记录的项目隔离和七天查询窗口。
func TestTestEventStoreMySQL(t *testing.T) {
	dsn := os.Getenv("GO_DOT_REQUIREMENTS_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated migrated database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	res, err := db.Exec(`INSERT INTO user_account (username,password_hash) VALUES (UUID(),'unused')`)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO project (code,name,write_key_hash,allowed_origins,visitor_salt,created_by) VALUES (UUID(),'test',SHA2(UUID(),256),JSON_ARRAY(),'salt',?)`, user)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := res.LastInsertId()
	defer func() {
		db.Exec(`DELETE FROM tracking_test_event WHERE project_id=?`, p)
		db.Exec(`DELETE FROM project WHERE id=?`, p)
		db.Exec(`DELETE FROM user_account WHERE id=?`, user)
	}()
	store := NewTestEventStore(db)
	err = store.SaveTestRecords(ctx, uint64(p), []ingestion.TestRecord{{PointCode: "example", Platform: "web", Status: "accepted", ReceivedAt: time.Now().UTC()}, {PointCode: "example", Platform: "web", Status: "rejected", Code: "PROPERTY_REQUIRED", ReceivedAt: time.Now().UTC()}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.RecentTestRecords(ctx, uint64(p), "example")
	if err != nil || len(got) != 2 || got[0].Code != "PROPERTY_REQUIRED" {
		t.Fatalf("records=%+v err=%v", got, err)
	}
	other, err := store.RecentTestRecords(ctx, uint64(p)+100000, "example")
	if err != nil || len(other) != 0 {
		t.Fatalf("other=%+v err=%v", other, err)
	}
}
