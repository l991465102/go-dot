package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestRunAppliesFilesOnceInLexicalOrder(t *testing.T) {
	db := newFakeExecutor()
	files := fstest.MapFS{
		"002_second.sql": {Data: []byte("CREATE TABLE second(id INT);")},
		"001_first.sql":  {Data: []byte("CREATE TABLE first(id INT);")},
	}
	require.NoError(t, Run(context.Background(), db, MySQL, files))
	require.NoError(t, Run(context.Background(), db, MySQL, files))
	require.Equal(t, []string{"001_first.sql", "002_second.sql"}, db.AppliedNames())
}

func TestRunRejectsChangedAppliedMigration(t *testing.T) {
	db := newFakeExecutorWithApplied("001_first.sql", "old-checksum")
	files := fstest.MapFS{"001_first.sql": {Data: []byte("SELECT 1;")}}
	require.ErrorContains(t, Run(context.Background(), db, MySQL, files), "checksum mismatch")
}

func TestRunRollsBackMySQLMigrationWhenAStatementFails(t *testing.T) {
	db := newFakeExecutor()
	db.failQuery = "BROKEN"
	files := fstest.MapFS{"001_first.sql": {Data: []byte("CREATE TABLE first(id INT); BROKEN;")}}

	require.ErrorContains(t, Run(context.Background(), db, MySQL, files), "execute migration")
	require.Empty(t, db.AppliedNames())
	require.Equal(t, 1, db.rollbacks)
}

func TestRunDoesNotRecordClickHouseMigrationUntilAllStatementsSucceed(t *testing.T) {
	db := newFakeExecutor()
	db.failQuery = "BROKEN"
	files := fstest.MapFS{"001_first.sql": {Data: []byte("CREATE TABLE first(id INT); BROKEN;")}}

	require.ErrorContains(t, Run(context.Background(), db, ClickHouse, files), "execute migration")
	require.Empty(t, db.AppliedNames())
}

func TestRunRetriesAfterPartiallyAppliedDDL(t *testing.T) {
	tests := []struct {
		name      string
		dialect   Dialect
		directory string
		wantNames []string
	}{
		{name: "mysql", dialect: MySQL, directory: "mysql", wantNames: []string{"0001_metadata.sql", "0002_custom_reports.sql", "0003_property_analytics.sql"}},
		{name: "clickhouse", dialect: ClickHouse, directory: "clickhouse", wantNames: []string{"0001_events.sql", "0002_hourly_stats.sql", "0003_non_replicated_deduplication.sql", "0004_event_statistics_contract.sql", "0005_hourly_generation_commit.sql", "0006_hourly_category_codes.sql", "0007_property_analytics.sql"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newPartialDDLExecutor()
			db.failCreateNumber = 2

			require.ErrorContains(t, Run(context.Background(), db, tt.dialect, migrationFiles(t, tt.directory)), "execute migration")
			require.Empty(t, db.AppliedNames())

			require.NoError(t, Run(context.Background(), db, tt.dialect, migrationFiles(t, tt.directory)))
			require.Equal(t, tt.wantNames, db.AppliedNames())
		})
	}
}

func TestClickHouseMigrationsEnableNonReplicatedDeduplication(t *testing.T) {
	raw, err := fs.ReadFile(migrationFiles(t, "clickhouse"), "0003_non_replicated_deduplication.sql")
	require.NoError(t, err)
	contents := string(raw)
	require.Contains(t, contents, "ALTER TABLE tracking_event MODIFY SETTING non_replicated_deduplication_window = 1000")
	require.Contains(t, contents, "ALTER TABLE tracking_invalid_event MODIFY SETTING non_replicated_deduplication_window = 1000")
}

func TestMySQLCustomReportMigrationHasRelationshipConstraints(t *testing.T) {
	raw, err := fs.ReadFile(migrationFiles(t, "mysql"), "0002_custom_reports.sql")
	require.NoError(t, err)
	contents := string(raw)
	require.Contains(t, contents, "UNIQUE KEY uk_custom_report_project_name (project_id, name)")
	require.Contains(t, contents, "FOREIGN KEY (report_id) REFERENCES custom_report (id) ON DELETE CASCADE")
	require.Contains(t, contents, "FOREIGN KEY (point_id) REFERENCES tracking_point (id) ON DELETE RESTRICT")
}

func migrationFiles(t *testing.T, directory string) fs.FS {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return os.DirFS(filepath.Join(filepath.Dir(filename), "../../../migrations", directory))
}

type fakeExecutor struct {
	applied      map[string]string
	appliedNames []string
	failQuery    string
	rollbacks    int
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{applied: make(map[string]string)}
}

func newFakeExecutorWithApplied(name, checksum string) *fakeExecutor {
	db := newFakeExecutor()
	db.applied[name] = checksum
	return db
}

func (f *fakeExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if query == f.failQuery {
		return nil, errors.New("forced failure")
	}
	if len(args) == 2 {
		name, nameOK := args[0].(string)
		checksum, checksumOK := args[1].(string)
		if nameOK && checksumOK {
			f.applied[name] = checksum
			f.appliedNames = append(f.appliedNames, name)
		}
	}
	return fakeResult{}, nil
}

func (f *fakeExecutor) MigrationChecksum(_ context.Context, name string) (string, bool, error) {
	checksum, ok := f.applied[name]
	return checksum, ok, nil
}

func (f *fakeExecutor) BeginTx(context.Context, *sql.TxOptions) (Transaction, error) {
	return &fakeTransaction{db: f}, nil
}

func (f *fakeExecutor) AppliedNames() []string {
	return append([]string(nil), f.appliedNames...)
}

type fakeTransaction struct {
	db       *fakeExecutor
	name     string
	checksum string
}

func (f *fakeTransaction) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if query == f.db.failQuery {
		return nil, errors.New("forced failure")
	}
	if len(args) == 2 {
		name, nameOK := args[0].(string)
		checksum, checksumOK := args[1].(string)
		if nameOK && checksumOK {
			f.name = name
			f.checksum = checksum
		}
	}
	return fakeResult{}, nil
}

func (f *fakeTransaction) Commit() error {
	if f.name != "" {
		f.db.applied[f.name] = f.checksum
		f.db.appliedNames = append(f.db.appliedNames, f.name)
	}
	return nil
}

func (f *fakeTransaction) Rollback() error {
	f.db.rollbacks++
	return nil
}

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, errors.New("not supported") }
func (fakeResult) RowsAffected() (int64, error) { return 0, nil }

var _ fs.FS = fstest.MapFS{}

type partialDDLExecutor struct {
	*fakeExecutor
	created          map[string]struct{}
	createCount      int
	failCreateNumber int
}

func newPartialDDLExecutor() *partialDDLExecutor {
	return &partialDDLExecutor{fakeExecutor: newFakeExecutor(), created: make(map[string]struct{})}
}

func (f *partialDDLExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return f.exec(ctx, query, args...)
}

func (f *partialDDLExecutor) BeginTx(context.Context, *sql.TxOptions) (Transaction, error) {
	return &partialDDLTransaction{db: f}, nil
}

func (f *partialDDLExecutor) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	upper := strings.ToUpper(strings.TrimSpace(query))
	if strings.HasPrefix(upper, "CREATE TABLE") && !strings.Contains(upper, "SCHEMA_MIGRATIONS") {
		f.createCount++
		if f.createCount == f.failCreateNumber {
			f.failCreateNumber = 0
			return nil, errors.New("forced DDL failure")
		}
		name, ifNotExists, err := createdTable(query)
		if err != nil {
			return nil, err
		}
		if _, exists := f.created[name]; exists && !ifNotExists {
			return nil, fmt.Errorf("table %s already exists", name)
		}
		f.created[name] = struct{}{}
	}
	return f.fakeExecutor.ExecContext(ctx, query, args...)
}

func createdTable(query string) (string, bool, error) {
	fields := strings.Fields(query)
	if len(fields) < 3 {
		return "", false, errors.New("invalid CREATE TABLE statement")
	}
	ifNotExists := len(fields) >= 6 && strings.EqualFold(strings.Join(fields[2:5], " "), "IF NOT EXISTS")
	nameIndex := 2
	if ifNotExists {
		nameIndex = 5
	}
	return strings.Trim(fields[nameIndex], "`"), ifNotExists, nil
}

type partialDDLTransaction struct {
	db       *partialDDLExecutor
	name     string
	checksum string
}

func (f *partialDDLTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if len(args) == 2 {
		name, nameOK := args[0].(string)
		checksum, checksumOK := args[1].(string)
		if nameOK && checksumOK {
			f.name = name
			f.checksum = checksum
			return fakeResult{}, nil
		}
	}
	return f.db.exec(ctx, query, args...)
}

func (f *partialDDLTransaction) Commit() error {
	if f.name != "" {
		f.db.applied[f.name] = f.checksum
		f.db.appliedNames = append(f.db.appliedNames, f.name)
	}
	return nil
}

func (f *partialDDLTransaction) Rollback() error { return nil }
