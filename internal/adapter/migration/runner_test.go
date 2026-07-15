package migration

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
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
