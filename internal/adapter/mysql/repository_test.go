package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lzw7758/go-dot/internal/auth"
	"github.com/lzw7758/go-dot/internal/project"
)

func TestProjectRepositoryRejectsRotationDuringGraceUnderRowLock(t *testing.T) {
	now := time.Now().UTC()
	script, db := openScriptedDB(t, []driver.Value{now.Add(time.Hour)})
	repo := NewProjectRepository(db)

	err := repo.RotateWriteKey(context.Background(), 9, "new-hash", now, now.Add(24*time.Hour))
	require.ErrorIs(t, err, project.ErrWriteKeyRotationInProgress)
	require.Len(t, script.queries, 1)
	require.Contains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	require.Empty(t, script.execQueries)
	require.Equal(t, 1, script.rollbacks)
}

func TestAuthRepositorySuccessorInsertFailureRollsBackOldRevocation(t *testing.T) {
	now := time.Now().UTC()
	script, db := openScriptedDB(t, []driver.Value{uint64(7), "old-hash", now.Add(time.Hour), nil, "admin", "password-hash", "Admin", false, "ACTIVE", nil})
	script.execErrors = []error{nil, errors.New("insert successor failed")}
	repo := NewAuthRepository(db)
	successor := auth.RefreshToken{Hash: "new-hash", ExpiresAt: now.Add(7 * 24 * time.Hour)}

	_, err := repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.ErrorContains(t, err, "insert successor failed")
	require.Equal(t, 1, script.rollbacks)
	require.Zero(t, script.commits)

	// The scripted database presents the old row as unrevoked again, matching
	// the state a real transactional rollback preserves.
	_, err = repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.NoError(t, err)
	require.Equal(t, 1, script.commits)
}

func TestAuthRepositoryUserLookupFailureRollsBackAndOldTokenRemainsUsable(t *testing.T) {
	now := time.Now().UTC()
	script, db := openScriptedDB(t, []driver.Value{uint64(7), "old-hash", now.Add(time.Hour), nil, "admin", "password-hash", "Admin", false, "ACTIVE", nil})
	script.queryErrors = []error{errors.New("user lookup failed")}
	repo := NewAuthRepository(db)
	successor := auth.RefreshToken{Hash: "new-hash", ExpiresAt: now.Add(7 * 24 * time.Hour)}

	_, err := repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.ErrorContains(t, err, "user lookup failed")
	require.Equal(t, 1, script.rollbacks)
	require.Empty(t, script.execQueries)

	_, err = repo.RotateRefreshToken(context.Background(), "old-hash", successor, now)
	require.NoError(t, err)
	require.Equal(t, 1, script.commits)
}

func TestTrackingPublishLocksDraftAndCommitsSnapshotStateAndAuditTogether(t *testing.T) {
	pointRow := []driver.Value{
		uint64(7), uint64(9), uint64(3), "product_detail_view", "Product detail", nil, "VIEW", "DRAFT", "PENDING",
		nil, nil, nil, nil, true, true, "APPROXIMATE", "EVENT_TIME", []byte(`["WEB"]`), []byte(`[]`), nil, nil, nil,
		nil, nil, nil, uint64(1), uint64(1), uint64(3), uint64(9), "SCENE", true,
	}
	propertyRow := []driver.Value{"product_id", "Product ID", "STRING", true, nil, nil, nil, nil, 0}
	script, db := openScriptedDB(t, nil)
	script.queryRows = [][]driver.Value{pointRow, propertyRow}
	script.execResults = []driver.Result{scriptResult{lastID: 41, affected: 1}, scriptResult{affected: 1}, scriptResult{affected: 1}}

	_, err := NewTrackingRepository(db).Publish(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Len(t, script.queries, 2)
	require.Contains(t, strings.ToUpper(script.queries[0]), "FOR UPDATE")
	require.Len(t, script.execQueries, 3)
	require.Contains(t, script.execQueries[0], "tracking_point_snapshot")
	require.Contains(t, script.execQueries[1], "definition_state='DRAFT'")
	require.Contains(t, script.execQueries[2], "audit_log")
	require.Equal(t, 1, script.commits)
	require.Zero(t, script.rollbacks)
}

var (
	registerScriptedDriver sync.Once
	scriptRegistry         sync.Map
)

func openScriptedDB(t *testing.T, row []driver.Value) (*dbScript, *sql.DB) {
	t.Helper()
	registerScriptedDriver.Do(func() { sql.Register("task3-scripted", scriptedDriver{}) })
	name := t.Name()
	script := &dbScript{row: row}
	scriptRegistry.Store(name, script)
	t.Cleanup(func() { scriptRegistry.Delete(name) })
	db, err := sql.Open("task3-scripted", name)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return script, db
}

type dbScript struct {
	mu          sync.Mutex
	row         []driver.Value
	queryRows   [][]driver.Value
	execErrors  []error
	execResults []driver.Result
	queryErrors []error
	queries     []string
	execQueries []string
	commits     int
	rollbacks   int
}

type scriptedDriver struct{}

func (scriptedDriver) Open(name string) (driver.Conn, error) {
	value, ok := scriptRegistry.Load(name)
	if !ok {
		return nil, errors.New("missing script")
	}
	return &scriptedConn{script: value.(*dbScript)}, nil
}

type scriptedConn struct{ script *dbScript }

func (c *scriptedConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *scriptedConn) Close() error              { return nil }
func (c *scriptedConn) Begin() (driver.Tx, error) { return &scriptedTx{script: c.script}, nil }
func (c *scriptedConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *scriptedConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.script.mu.Lock()
	defer c.script.mu.Unlock()
	c.script.queries = append(c.script.queries, query)
	if len(c.script.queryErrors) > 0 {
		err := c.script.queryErrors[0]
		c.script.queryErrors = c.script.queryErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	row := c.script.row
	if len(c.script.queryRows) > 0 {
		row = c.script.queryRows[0]
		c.script.queryRows = c.script.queryRows[1:]
	}
	return &scriptedRows{row: append([]driver.Value(nil), row...)}, nil
}
func (c *scriptedConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.script.mu.Lock()
	defer c.script.mu.Unlock()
	c.script.execQueries = append(c.script.execQueries, query)
	if len(c.script.execErrors) > 0 {
		err := c.script.execErrors[0]
		c.script.execErrors = c.script.execErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(c.script.execResults) > 0 {
		result := c.script.execResults[0]
		c.script.execResults = c.script.execResults[1:]
		return result, nil
	}
	return driver.RowsAffected(1), nil
}

type scriptResult struct{ lastID, affected int64 }

func (r scriptResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r scriptResult) RowsAffected() (int64, error) { return r.affected, nil }

type scriptedTx struct{ script *dbScript }

func (t *scriptedTx) Commit() error {
	t.script.mu.Lock()
	defer t.script.mu.Unlock()
	t.script.commits++
	return nil
}
func (t *scriptedTx) Rollback() error {
	t.script.mu.Lock()
	defer t.script.mu.Unlock()
	t.script.rollbacks++
	return nil
}

type scriptedRows struct {
	row  []driver.Value
	read bool
}

func (r *scriptedRows) Columns() []string {
	columns := make([]string, len(r.row))
	for i := range columns {
		columns[i] = "column"
	}
	return columns
}
func (r *scriptedRows) Close() error { return nil }
func (r *scriptedRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(dest, r.row)
	return nil
}
