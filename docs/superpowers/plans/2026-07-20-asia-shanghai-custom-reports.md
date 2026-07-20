# Asia/Shanghai Time and Custom Reports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Display absolute timestamps in Asia/Shanghai and add temporary or saved multi-point daily PV/UV reports with XLSX export.

**Architecture:** Keep UTC at storage and API boundaries, centralize display formatting in the Vue client, store named report definitions transactionally in MySQL, and query committed ClickHouse hourly aggregates grouped into Asia/Shanghai calendar days. A new `internal/reporting` application module owns permissions, validation, zero filling, saved configurations, and export orchestration; HTTP and Vue layers remain thin adapters.

**Tech Stack:** Go 1.26, GoFrame HTTP host with `net/http` controllers, MySQL 8, ClickHouse, Excelize v2 streaming writer, Vue 3, TypeScript 6, Element Plus, Vitest, Playwright.

## Global Constraints

- Store and transport timestamps in UTC; format user-facing absolute timestamps with the fixed IANA timezone `Asia/Shanghai` as `YYYY-MM-DD HH:mm:ss`.
- Daily report boundaries are Asia/Shanghai calendar days with an inclusive start date and inclusive end date converted internally to `[start, end+1 day)`.
- Query 1-100 published or deprecated points over 1-90 calendar days; default to 30 days.
- Return zero rows for missing date/point combinations and mark the current Asia/Shanghai day incomplete.
- Sum PV only; never expose a cross-date or cross-point total UV.
- Admins mutate saved reports; Admin and Viewer roles query, view saved reports, and export.
- Preserve point order from the request or saved report.
- Prevent spreadsheet formula injection for user-controlled text beginning with `=`, `+`, `-`, or `@`.
- Do not include the pre-existing uncommitted `README.md` change in intermediate commits; merge README work deliberately in the final documentation task.

---

## File Structure

### New backend files

- `migrations/mysql/0002_custom_reports.sql` — saved report tables and foreign keys.
- `internal/reporting/model.go` — report configuration, daily query, aggregate, and export models.
- `internal/reporting/errors.go` — stable domain errors for validation, not-found, and conflict.
- `internal/reporting/service.go` — authorization, validation, CRUD, daily result ordering/zero fill, and export orchestration.
- `internal/reporting/service_test.go` — service behavior and permission tests.
- `internal/adapter/mysql/report_repository.go` — transactional report configuration persistence and point resolution.
- `internal/adapter/mysql/report_repository_test.go` — SQL transaction, optimistic lock, and project isolation tests.
- `internal/adapter/clickhouse/daily_report.go` — exact/approximate per-point daily aggregate queries.
- `internal/adapter/clickhouse/daily_report_test.go` — SQL shape and mixed-UV result tests.
- `internal/reporting/excel.go` — XLSX workbook generation and formula-safe cells.
- `internal/reporting/excel_test.go` — reopen and verify workbook contents.
- `api/reports/v1/reports.go` — HTTP request and response contracts.
- `internal/adapter/httpapi/report_controller.go` — report CRUD, query, and download endpoints.
- `internal/adapter/httpapi/report_controller_test.go` — endpoint, permission, and response header tests.

### New frontend files

- `web/src/utils/datetime.ts` — fixed Asia/Shanghai absolute-time formatter.
- `web/src/utils/datetime.spec.ts` — deterministic formatting tests.
- `web/src/api/reports.ts` — report CRUD/query/export client.
- `web/src/views/CustomReportsView.vue` — temporary and saved report workflow.
- `web/src/views/CustomReportsView.spec.ts` — admin/viewer interaction tests.

### Existing files modified

- `web/src/views/DiagnosticsView.vue`
- `web/src/views/DiagnosticsView.spec.ts`
- `web/src/components/diagnostics/InvalidEventTable.vue`
- `web/src/components/diagnostics/DeadLetterTable.vue`
- `web/src/views/ProjectSettingsView.vue`
- `web/src/views/ProjectSettingsView.spec.ts`
- `internal/adapter/httpapi/project_controller.go`
- `internal/cmd/api.go`
- `web/src/router/index.ts`
- `web/src/layouts/AppLayout.vue`
- `web/src/layouts/AppLayout.spec.ts`
- `web/src/styles.css`
- `web/e2e/statistics-and-diagnostics.spec.ts`
- `tests/integration/tracking_flow_test.go`
- `README.md`
- `go.mod`
- `go.sum`

---

### Task 1: Centralize Asia/Shanghai absolute-time formatting

**Files:**
- Create: `web/src/utils/datetime.ts`
- Create: `web/src/utils/datetime.spec.ts`
- Modify: `web/src/views/DiagnosticsView.vue`
- Modify: `web/src/views/DiagnosticsView.spec.ts`
- Modify: `web/src/components/diagnostics/InvalidEventTable.vue`
- Modify: `web/src/components/diagnostics/DeadLetterTable.vue`
- Modify: `web/src/views/ProjectSettingsView.vue`
- Modify: `web/src/views/ProjectSettingsView.spec.ts`

**Interfaces:**
- Produces: `DISPLAY_TIMEZONE = 'Asia/Shanghai'` and `formatShanghaiDateTime(value: string | Date | null | undefined): string`.
- Consumes: RFC3339 UTC strings already returned by diagnostics and project APIs.

- [ ] **Step 1: Write the failing formatter tests**

```ts
import { describe, expect, it } from 'vitest'
import { DISPLAY_TIMEZONE, formatShanghaiDateTime } from './datetime'

describe('formatShanghaiDateTime', () => {
  it('formats UTC in fixed Asia/Shanghai time', () => {
    expect(DISPLAY_TIMEZONE).toBe('Asia/Shanghai')
    expect(formatShanghaiDateTime('2026-07-20T02:51:03Z')).toBe('2026-07-20 10:51:03')
  })

  it('returns a dash for missing or invalid values', () => {
    expect(formatShanghaiDateTime('')).toBe('-')
    expect(formatShanghaiDateTime('not-a-date')).toBe('-')
    expect(formatShanghaiDateTime(undefined)).toBe('-')
  })
})
```

- [ ] **Step 2: Run the test and confirm RED**

Run: `cd web && npm test -- --run src/utils/datetime.spec.ts`

Expected: FAIL because `src/utils/datetime.ts` does not exist.

- [ ] **Step 3: Implement the fixed formatter**

```ts
export const DISPLAY_TIMEZONE = 'Asia/Shanghai'

const formatter = new Intl.DateTimeFormat('zh-CN', {
  timeZone: DISPLAY_TIMEZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
})

export function formatShanghaiDateTime(value: string | Date | null | undefined): string {
  if (!value) return '-'
  const parsed = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(parsed.getTime())) return '-'
  const parts = Object.fromEntries(formatter.formatToParts(parsed).map((part) => [part.type, part.value]))
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}
```

- [ ] **Step 4: Render formatted values through scoped slots**

Use `formatShanghaiDateTime` in all three diagnostics tables and the write-key expiry alert. Change time headers to include `（Asia/Shanghai）`. For example:

```vue
<el-table-column label="接收时间（Asia/Shanghai）" min-width="220">
  <template #default="scope">{{ formatShanghaiDateTime(scope.row.received_at) }}</template>
</el-table-column>
```

Add component assertions that `2026-07-20T02:51:03Z` is absent and `2026-07-20 10:51:03` is visible.

- [ ] **Step 5: Run focused and full frontend checks**

Run:

```bash
cd web
npm test -- --run src/utils/datetime.spec.ts src/views/DiagnosticsView.spec.ts src/views/ProjectSettingsView.spec.ts
npm run typecheck
```

Expected: focused tests PASS and typecheck exits 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/utils/datetime.ts web/src/utils/datetime.spec.ts web/src/views/DiagnosticsView.vue web/src/views/DiagnosticsView.spec.ts web/src/components/diagnostics/InvalidEventTable.vue web/src/components/diagnostics/DeadLetterTable.vue web/src/views/ProjectSettingsView.vue web/src/views/ProjectSettingsView.spec.ts
git commit -m "fix: display absolute times in Asia Shanghai"
```

---

### Task 2: Add saved-report models, schema, and transactional MySQL repository

**Files:**
- Create: `migrations/mysql/0002_custom_reports.sql`
- Create: `internal/reporting/model.go`
- Create: `internal/reporting/errors.go`
- Create: `internal/adapter/mysql/report_repository.go`
- Create: `internal/adapter/mysql/report_repository_test.go`
- Modify: `internal/adapter/migration/runner_test.go`

**Interfaces:**
- Produces: `reporting.Repository`, saved-report models, daily-query models, `DailyStore`, and `Exporter`; later tasks implement these stable ports without changing the service constructor.
- Consumes: `project_id`, tracking point rows, and MySQL transaction support.

- [ ] **Step 1: Define domain contracts and failing repository tests**

Create these public types:

```go
package reporting

type PointRef struct {
    ID uint64 `json:"id"`
    Code string `json:"code"`
    Name string `json:"name"`
    DefinitionState tracking.DefinitionState `json:"definition_state"`
    UVMode tracking.UVMode `json:"uv_mode"`
    SortNo int `json:"sort_no"`
}

type Report struct {
    ID uint64 `json:"id"`
    ProjectID uint64 `json:"project_id"`
    Name string `json:"name"`
    Description string `json:"description"`
    DefaultDays int `json:"default_days"`
    Points []PointRef `json:"points"`
    CreatedBy uint64 `json:"created_by"`
    UpdatedBy uint64 `json:"updated_by"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type ReportInput struct {
    Name string `json:"name"`
    Description string `json:"description"`
    DefaultDays int `json:"default_days"`
    PointIDs []uint64 `json:"point_ids"`
    ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

type Repository interface {
    List(context.Context, uint64) ([]Report, error)
    Get(context.Context, uint64, uint64) (Report, error)
    Create(context.Context, uint64, uint64, ReportInput) (Report, error)
    Update(context.Context, uint64, uint64, uint64, ReportInput) (Report, error)
    Delete(context.Context, uint64, uint64) error
    ResolvePoints(context.Context, uint64, []uint64) ([]PointRef, error)
}

type DailyQuery struct {
    ProjectID uint64 `json:"project_id"`
    StartDate string `json:"start_date"`
    EndDate string `json:"end_date"`
    PointIDs []uint64 `json:"point_ids"`
}

type DailyFilter struct {
    ProjectID uint64
    Start time.Time
    End time.Time
    Timezone string
    ApproximatePointIDs []uint64
    ExactPointIDs []uint64
}

type DailyAggregate struct {
    Date string
    PointID uint64
    PointCode string
    PV uint64
    UV uint64
}

type DailyStore interface {
    QueryDaily(context.Context, DailyFilter) ([]DailyAggregate, error)
}

type DailyItem struct {
    Date string `json:"date"`
    Complete bool `json:"complete"`
    PointID uint64 `json:"point_id"`
    PointCode string `json:"point_code"`
    PointName string `json:"point_name"`
    PV uint64 `json:"pv"`
    UV uint64 `json:"uv"`
    UVMode tracking.UVMode `json:"uv_mode"`
}

type DailyResult struct {
    Timezone string `json:"timezone"`
    StartDate string `json:"start_date"`
    EndDate string `json:"end_date"`
    PointCount int `json:"point_count"`
    DayCount int `json:"day_count"`
    TotalPV uint64 `json:"total_pv"`
    Items []DailyItem `json:"items"`
}

type ExportDocument struct {
    Result DailyResult
    ReportName string
    ReportDescription string
    ExportedBy string
    ExportedAt time.Time
}

type Exporter interface {
    Build(ExportDocument) ([]byte, error)
}

type ExportInput struct {
    DailyQuery
    ReportName string `json:"report_name"`
    ReportDescription string `json:"report_description"`
}

type ExportFile struct {
    Filename string
    Contents []byte
}
```

Define stable errors in `errors.go`:

```go
var (
    ErrInvalid = errors.New("REPORT_INVALID")
    ErrNotFound = errors.New("REPORT_NOT_FOUND")
    ErrConflict = errors.New("REPORT_CONFLICT")
)
```

Add repository tests proving create inserts the report, ordered point rows, and audit record in one transaction; duplicate names map to `reporting.ErrConflict`; update uses `updated_at` in the WHERE clause and maps zero affected rows to `ErrConflict`; reads and deletes always bind both project ID and report ID.

- [ ] **Step 2: Run repository tests and confirm RED**

Run: `go test ./internal/adapter/mysql -run 'TestReportRepository' -count=1`

Expected: FAIL because the reporting types and repository do not exist.

- [ ] **Step 3: Add the migration**

Create `0002_custom_reports.sql` with the two tables from the approved spec, InnoDB/utf8mb4, a foreign key from `custom_report_point.report_id` to `custom_report.id ON DELETE CASCADE`, and a foreign key from `custom_report_point.point_id` to `tracking_point.id` using `RESTRICT` deletion semantics.

- [ ] **Step 4: Implement the repository**

Implement `NewReportRepository(db *sql.DB) *ReportRepository`. Use transactions for create/update/delete, stable `sort_no` values from the input order, and this optimistic update form:

```sql
UPDATE custom_report
SET name=?, description=?, default_days=?, updated_by=?, updated_at=UTC_TIMESTAMP(3)
WHERE id=? AND project_id=? AND updated_at=?
```

`ResolvePoints` must return only rows matching `project_id`, requested IDs, and states `PUBLISHED` or `DEPRECATED`. Reorder resolved rows to match the first occurrence of each requested ID rather than SQL result order.

- [ ] **Step 5: Verify migration and repository tests**

Run:

```bash
go test ./internal/adapter/migration -count=1
go test ./internal/adapter/mysql -run 'TestReportRepository' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add migrations/mysql/0002_custom_reports.sql internal/reporting/model.go internal/reporting/errors.go internal/adapter/mysql/report_repository.go internal/adapter/mysql/report_repository_test.go internal/adapter/migration/runner_test.go
git commit -m "feat: persist custom report definitions"
```

---

### Task 3: Implement saved-report authorization and validation service

**Files:**
- Create: `internal/reporting/service.go`
- Create: `internal/reporting/service_test.go`

**Interfaces:**
- Consumes: `reporting.Repository` from Task 2 and an authorizer exposing `RequireRole(context.Context, actor, projectID, ...project.Role) error`.
- Produces: `NewService(auth Authorizer, repo Repository, daily DailyStore, exporter Exporter, now func() time.Time) *Service` plus report CRUD methods used by HTTP.

- [ ] **Step 1: Write failing service tests**

Create `service_test.go` fakes that implement `Authorizer` and every `Repository`
method with in-memory fields, then cover these exact cases:

```go
func TestViewerCanListButCannotCreateReport(t *testing.T) {
    auth := &fakeAuthorizer{role: project.Viewer}
    repo := &fakeRepository{reports: []Report{{ID: 1, ProjectID: 9, Name: "daily"}}}
    service := NewService(auth, repo, nil, nil, time.Now)
    items, err := service.ListReports(context.Background(), 3, 9)
    require.NoError(t, err)
    require.Len(t, items, 1)
    _, err = service.CreateReport(context.Background(), 3, 9, ReportInput{Name: "new", DefaultDays: 30, PointIDs: []uint64{7}})
    require.ErrorIs(t, err, project.ErrForbidden)
}

func TestCreateRejectsInvalidInputs(t *testing.T) {
    service := newAdminServiceForTest()
    tooMany := make([]uint64, 101)
    for i := range tooMany {
        tooMany[i] = uint64(i + 1)
    }
    for _, input := range []ReportInput{
        {Name: "", DefaultDays: 30, PointIDs: []uint64{7}},
        {Name: "daily", DefaultDays: 0, PointIDs: []uint64{7}},
        {Name: "daily", DefaultDays: 30, PointIDs: nil},
        {Name: "daily", DefaultDays: 30, PointIDs: tooMany},
    } {
        _, err := service.CreateReport(context.Background(), 1, 9, input)
        require.ErrorIs(t, err, ErrInvalid)
    }
}

func TestUpdateRequiresExpectedUpdatedAt(t *testing.T) {
    service := newAdminServiceForTest()
    _, err := service.UpdateReport(context.Background(), 1, 9, 7, ReportInput{Name: "daily", DefaultDays: 30, PointIDs: []uint64{3}})
    require.ErrorIs(t, err, ErrInvalid)
}

func TestPointOrderKeepsFirstOccurrence(t *testing.T) {
    service, repo := newAdminServiceWithRepositoryForTest()
    created, err := service.CreateReport(context.Background(), 1, 9, ReportInput{Name: "daily", DefaultDays: 30, PointIDs: []uint64{3, 2, 3}})
    require.NoError(t, err)
    require.Equal(t, []uint64{3, 2}, repo.lastPointIDs)
    require.Equal(t, []uint64{3, 2}, []uint64{created.Points[0].ID, created.Points[1].ID})
}
```

The helpers named above are test-local constructors, not production APIs. They must
return fakes with published same-project points for IDs 2, 3, and 7; add a separate
test whose resolver omits an ID and assert `ErrInvalid` for a draft or cross-project
selection.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/reporting -run 'Test(Viewer|Create|Update|PointOrder)' -count=1`

Expected: FAIL because `Service` is undefined.

- [ ] **Step 3: Implement CRUD service methods**

Implement:

```go
func (s *Service) ListReports(ctx context.Context, actor, projectID uint64) ([]Report, error)
func (s *Service) GetReport(ctx context.Context, actor, projectID, reportID uint64) (Report, error)
func (s *Service) CreateReport(ctx context.Context, actor, projectID uint64, in ReportInput) (Report, error)
func (s *Service) UpdateReport(ctx context.Context, actor, projectID, reportID uint64, in ReportInput) (Report, error)
func (s *Service) DeleteReport(ctx context.Context, actor, projectID, reportID uint64) error
```

Trim name/description, require name length 1-128, description length at most 1000, default days 1-90, and 1-100 unique point IDs. List/Get authorize Admin or Viewer; Create/Update/Delete authorize Admin only. Validate resolved points before repository mutation.

- [ ] **Step 4: Run service tests**

Run: `go test ./internal/reporting -run 'Test(Viewer|Create|Update|PointOrder)' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reporting/service.go internal/reporting/service_test.go
git commit -m "feat: govern saved custom reports"
```

---

### Task 4: Query daily per-point aggregates from ClickHouse

**Files:**
- Create: `internal/adapter/clickhouse/daily_report.go`
- Create: `internal/adapter/clickhouse/daily_report_test.go`
- Modify: `internal/reporting/service.go`
- Modify: `internal/reporting/service_test.go`

**Interfaces:**
- Implements: the `DailyStore.QueryDaily(context.Context, DailyFilter) ([]DailyAggregate, error)` port defined in Task 2.
- Produces: `Service.QueryDaily`.
- Consumes: ordered `[]PointRef`, Asia/Shanghai dates, and committed hourly aggregate tables.

- [ ] **Step 1: Write failing validation and zero-fill tests against the Task 2 types**

```go
func TestQueryDailyFillsMissingPointDaysAndMarksTodayIncomplete(t *testing.T) {
    location, err := time.LoadLocation("Asia/Shanghai")
    require.NoError(t, err)
    now := time.Date(2026, 7, 20, 10, 0, 0, 0, location)
    daily := &fakeDailyStore{items: []DailyAggregate{{Date: "2026-07-19", PointID: 3, PointCode: "checkout", PV: 4, UV: 2}}}
    repo := &fakeRepository{resolved: []PointRef{
        {ID: 3, Code: "checkout", Name: "Checkout", UVMode: tracking.Exact},
        {ID: 2, Code: "view", Name: "View", UVMode: tracking.Approximate},
    }}
    service := NewService(&fakeAuthorizer{role: project.Viewer}, repo, daily, nil, func() time.Time { return now })
    result, err := service.QueryDaily(context.Background(), 7, DailyQuery{ProjectID: 9, StartDate: "2026-07-19", EndDate: "2026-07-20", PointIDs: []uint64{3, 2}})
    require.NoError(t, err)
    require.Equal(t, 2, result.DayCount)
    require.Len(t, result.Items, 4)
    require.Equal(t, []string{"2026-07-20", "2026-07-20", "2026-07-19", "2026-07-19"}, []string{result.Items[0].Date, result.Items[1].Date, result.Items[2].Date, result.Items[3].Date})
    require.Equal(t, []uint64{3, 2, 3, 2}, []uint64{result.Items[0].PointID, result.Items[1].PointID, result.Items[2].PointID, result.Items[3].PointID})
    require.False(t, result.Items[0].Complete)
    require.True(t, result.Items[2].Complete)
    require.Equal(t, uint64(4), result.TotalPV)
}
```

The four-row assertion is the date/point matrix. Also add table tests for malformed
dates, future end dates, zero
points, 101 unique points, and a 91-day inclusive range. Use reflection on
`DailyResult` to assert there is no JSON field named `total_uv`.

- [ ] **Step 2: Add failing ClickHouse SQL tests**

Assert approximate points use `business_stat_hourly` plus `uniqCombined64Merge`, exact points use `business_stat_exact_hourly` plus `uniqExactMerge`, both join the generation table with `FINAL`, group by `toDate(hour, 'Asia/Shanghai')` and point ID/code, and bind point IDs rather than interpolate them.

- [ ] **Step 3: Run tests and confirm RED**

Run:

```bash
go test ./internal/reporting -run 'TestQueryDaily' -count=1
go test ./internal/adapter/clickhouse -run 'TestDailyReport' -count=1
```

Expected: FAIL because daily querying is not implemented.

- [ ] **Step 4: Implement ClickHouse mixed-mode queries**

Add `QueryDaily` to `StatisticsStore`. Partition points by UV mode, issue at most two bound queries, scan rows into `DailyAggregate{Date, PointID, PointCode, PV, UV}`, and merge results by date/point key. Use the same generation-version subquery pattern as `query` in `statistics_store.go` so only committed hourly generations are read.

- [ ] **Step 5: Implement service validation and zero fill**

Parse dates using `time.LoadLocation("Asia/Shanghai")`, reject future end dates and spans over 90 inclusive days, resolve and validate points, call the store once, index aggregates by `(date, pointID)`, and emit the complete date matrix. Compute `TotalPV` while building rows.

- [ ] **Step 6: Run focused tests**

Run:

```bash
go test ./internal/reporting -run 'TestQueryDaily' -count=1
go test ./internal/adapter/clickhouse -run 'TestDailyReport' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/reporting/service.go internal/reporting/service_test.go internal/adapter/clickhouse/daily_report.go internal/adapter/clickhouse/daily_report_test.go
git commit -m "feat: query daily statistics by point"
```

---

### Task 5: Generate safe two-sheet XLSX exports

**Files:**
- Create: `internal/reporting/excel.go`
- Create: `internal/reporting/excel_test.go`
- Modify: `internal/reporting/service.go`
- Modify: `internal/reporting/service_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `Exporter.Build(ExportDocument) ([]byte, error)` and `ExcelExporter`.
- Consumes: the exact `DailyResult` returned by Task 4 and report/export metadata.

- [ ] **Step 1: Add Excelize and write failing workbook tests**

Run: `go get github.com/xuri/excelize/v2@latest`

Then create a test that calls `ExcelExporter.Build`, opens returned bytes with `excelize.OpenReader`, and asserts sheets exactly equal `数据明细` and `报表说明`. Verify headers, an exact and approximate item, `Asia/Shanghai`, exporter username, and export time `2026-07-20 10:51:03`.

Add a second test with report name `=HYPERLINK("https://bad")` and assert the stored cell is plain text prefixed with an apostrophe, not a formula.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/reporting -run 'TestExcel' -count=1`

Expected: FAIL because `ExcelExporter` is undefined.

- [ ] **Step 3: Implement workbook generation**

Implement the `Exporter` port from Task 2 with `ExcelExporter`. Use Excelize stream
writers for both sheets, freeze the header row, set readable widths, finish both
streams, remove the default sheet, and call `WriteToBuffer` only after all workbook
operations succeed. Implement `safeSpreadsheetText` to prefix an apostrophe when
the first rune is `=`, `+`, `-`, or `@`.

Add the orchestration method:

```go
func (s *Service) ExportDaily(ctx context.Context, actor uint64, username string, in ExportInput) (ExportFile, error) {
    result, err := s.QueryDaily(ctx, actor, in.DailyQuery)
    if err != nil {
        return ExportFile{}, err
    }
    contents, err := s.exporter.Build(ExportDocument{Result: result, ReportName: in.ReportName, ReportDescription: in.ReportDescription, ExportedBy: username, ExportedAt: s.now().UTC()})
    if err != nil {
        return ExportFile{}, err
    }
    return ExportFile{Filename: exportFilename(in.ReportName, result.StartDate, result.EndDate), Contents: contents}, nil
}
```

`exportFilename` must fall back to `自定义报表`, strip path separators and control
characters, and append `_YYYYMMDD_YYYYMMDD.xlsx`.

- [ ] **Step 4: Run Excel tests and dependency checks**

Run:

```bash
go test ./internal/reporting -run 'TestExcel' -count=1
go mod tidy
go test ./internal/reporting -count=1
```

Expected: PASS and `go mod tidy` leaves a clean module graph.

- [ ] **Step 5: Commit**

```bash
git add internal/reporting/excel.go internal/reporting/excel_test.go internal/reporting/service.go internal/reporting/service_test.go go.mod go.sum
git commit -m "feat: export custom reports as xlsx"
```

---

### Task 6: Expose report CRUD, daily query, and Excel download APIs

**Files:**
- Create: `api/reports/v1/reports.go`
- Create: `internal/adapter/httpapi/report_controller.go`
- Create: `internal/adapter/httpapi/report_controller_test.go`
- Modify: `internal/adapter/httpapi/project_controller.go`
- Modify: `internal/cmd/api.go`

**Interfaces:**
- Consumes: `reporting.Service` CRUD/query/export methods and `auth.Principal.Username`.
- Produces: the five saved-report endpoints and two POST statistics endpoints from the spec.

- [ ] **Step 1: Write failing controller tests**

Cover:

```text
GET    /api/v1/projects/9/reports                  -> 200 for Viewer
POST   /api/v1/projects/9/reports                  -> 201 for Admin, 403 for Viewer
PATCH  /api/v1/projects/9/reports/7                -> 409 on stale expected_updated_at
DELETE /api/v1/projects/9/reports/7                -> 204 for Admin
POST   /api/v1/statistics/daily-by-point/query     -> JSON result for Viewer
POST   /api/v1/statistics/daily-by-point/export    -> XLSX headers and bytes for Viewer
```

Assert malformed dates and over-100 point lists return 400, cross-project access returns 403/404 without backend details, and download filename contains the date range.

- [ ] **Step 2: Run tests and confirm RED**

Run: `go test ./internal/adapter/httpapi -run 'TestReportController' -count=1`

Expected: FAIL because controller and routes are absent.

- [ ] **Step 3: Implement request contracts and controller**

Use `decodeJSON` with unknown-field rejection. Keep query and export POST routes authenticated but read-authorized inside the reporting service. For export, build bytes before writing headers, then set:

```go
w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
w.Header().Set("Content-Disposition", contentDisposition(filename))
w.Header().Set("Content-Length", strconv.Itoa(len(contents)))
w.WriteHeader(http.StatusOK)
_, _ = w.Write(contents)
```

Map `ErrInvalid` to 400, project forbidden to 403, `ErrNotFound` to 404, and `ErrConflict` to 409 using `writeAPIError` so request IDs remain available.

- [ ] **Step 4: Wire dependencies in `runAPI`**

Construct one MySQL report repository, reuse the ClickHouse `StatisticsStore` as `DailyStore`, construct `reporting.ExcelExporter`, and pass the reporting service to `NewHandler`. Extend the management-service type switch with `*reporting.Service` and register the report controller.

- [ ] **Step 5: Run HTTP and application tests**

Run:

```bash
go test ./internal/adapter/httpapi -run 'TestReportController' -count=1
go test ./internal/reporting ./internal/adapter/mysql ./internal/adapter/clickhouse ./internal/adapter/httpapi -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/reports/v1/reports.go internal/adapter/httpapi/report_controller.go internal/adapter/httpapi/report_controller_test.go internal/adapter/httpapi/project_controller.go internal/cmd/api.go
git commit -m "feat: expose custom report APIs"
```

---

### Task 7: Add frontend report API, route, and navigation

**Files:**
- Create: `web/src/api/reports.ts`
- Modify: `web/src/api/client.spec.ts`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/layouts/AppLayout.vue`
- Modify: `web/src/layouts/AppLayout.spec.ts`

**Interfaces:**
- Produces: typed report CRUD/query/export functions consumed by `CustomReportsView.vue`.
- Consumes: existing shared `apiClient`, project route parameter, and `projects.canMutate`.

- [ ] **Step 1: Write failing API and navigation tests**

Define and test these calls:

```ts
export function listReports(projectId: number, signal?: AbortSignal): Promise<SavedReport[]>
export function getReport(projectId: number, reportId: number, signal?: AbortSignal): Promise<SavedReport>
export function createReport(projectId: number, input: ReportMutation): Promise<SavedReport>
export function updateReport(projectId: number, reportId: number, input: ReportMutation & { expected_updated_at: string }): Promise<SavedReport>
export function deleteReport(projectId: number, reportId: number): Promise<void>
export function queryDaily(input: DailyQuery, signal?: AbortSignal): Promise<DailyResult>
export function exportDaily(input: DailyExport): Promise<{ blob: Blob; filename: string }>
```

Add a layout test that “自定义报表” points to route name `custom-reports` for the selected project.

- [ ] **Step 2: Run tests and confirm RED**

Run: `cd web && npm test -- --run src/api/client.spec.ts src/layouts/AppLayout.spec.ts`

Expected: FAIL because report API and navigation do not exist.

- [ ] **Step 3: Implement API types and download parsing**

Use `responseType: 'blob'` for export. Parse RFC 5987 `filename*=` when present and fall back to `custom-report.xlsx`. Do not use `window.open`, because it would omit the in-memory access token.

- [ ] **Step 4: Register lazy route and navigation item**

Add:

```ts
{ path: 'reports', name: 'custom-reports', component: () => import('@/views/CustomReportsView.vue') }
```

and sidebar item `{ label: '自定义报表', route: 'custom-reports', marker: '报' }`.

- [ ] **Step 5: Run focused tests and typecheck**

Run:

```bash
cd web
npm test -- --run src/api/client.spec.ts src/layouts/AppLayout.spec.ts
npm run typecheck
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/reports.ts web/src/api/client.spec.ts web/src/router/index.ts web/src/layouts/AppLayout.vue web/src/layouts/AppLayout.spec.ts
git commit -m "feat: add custom report navigation and client"
```

---

### Task 8: Build temporary and saved report page

**Files:**
- Create: `web/src/views/CustomReportsView.vue`
- Create: `web/src/views/CustomReportsView.spec.ts`
- Modify: `web/src/styles.css`

**Interfaces:**
- Consumes: `web/src/api/reports.ts`, `listPoints`, route project ID, `projects.canMutate`, and `formatShanghaiDateTime`.
- Produces: complete temporary query, saved-report, and download workflow.

- [ ] **Step 1: Write failing admin workflow test**

Mount with an Admin project, stub two published points and one deprecated point, and verify:

1. default range spans 30 Asia/Shanghai dates;
2. selecting ordered points and clicking 查询 renders date, status, code, name, PV, UV, and UV mode;
3. total PV is displayed and total UV text is absent;
4. 新建/保存配置/另存为/删除 controls are visible;
5. save sends point IDs in displayed order and stores `updated_at` for later optimistic PATCH;
6. export submits the currently edited dates and points, then triggers a blob download.

- [ ] **Step 2: Write failing Viewer and incomplete-day tests**

Mount with a Viewer project. Assert report selection, query, and export are visible, while all mutation controls are absent. Return an item with `complete: false` and assert “未完整” is visible next to the current date.

- [ ] **Step 3: Run tests and confirm RED**

Run: `cd web && npm test -- --run src/views/CustomReportsView.spec.ts`

Expected: FAIL because the view does not exist.

- [ ] **Step 4: Implement view state and loading rules**

Keep separate state for `selectedReport`, `savedSnapshot`, and editable query fields. Loading a saved report copies defaults into editable state. Querying never mutates `savedSnapshot`. “保存配置” PATCHes with `savedSnapshot.updated_at`; “另存为” POSTs a new report.

Fetch selectable points with remote search from both `PUBLISHED` and `DEPRECATED` filters, merge by ID, and cap selection at 100. Abort stale project/query requests using the same controller pattern as diagnostics.

- [ ] **Step 5: Implement page presentation and download**

Use Element Plus date range and multi-select controls, an ordered selected-point list with up/down controls, summary cards for point count/day count/total PV, and a table ordered exactly as returned. Show `统计时区：Asia/Shanghai`. Download with:

```ts
const url = URL.createObjectURL(blob)
const anchor = document.createElement('a')
anchor.href = url
anchor.download = filename
anchor.click()
URL.revokeObjectURL(url)
```

- [ ] **Step 6: Run focused tests, typecheck, and build**

Run:

```bash
cd web
npm test -- --run src/views/CustomReportsView.spec.ts
npm run typecheck
npm run build
```

Expected: tests PASS, typecheck exits 0, and Vite production build succeeds.

- [ ] **Step 7: Commit**

```bash
git add web/src/views/CustomReportsView.vue web/src/views/CustomReportsView.spec.ts web/src/styles.css
git commit -m "feat: add custom daily report workspace"
```

---

### Task 9: Add integration acceptance, documentation, and full verification

**Files:**
- Modify: `tests/integration/tracking_flow_test.go`
- Modify: `web/e2e/statistics-and-diagnostics.spec.ts`
- Modify: `README.md`
- Modify: `.superpowers/sdd/progress.md`

**Interfaces:**
- Consumes: all Tasks 1-8.
- Produces: executable acceptance proof and user-facing setup/use instructions.

- [ ] **Step 1: Add backend integration coverage**

Extend the tracking flow to create two points with different UV modes, report events on two Asia/Shanghai dates, run aggregation, save a named report, query its points, export XLSX, reopen the file, and assert page-equivalent rows. Create a Viewer fixture and assert it can query/export but receives 403 on saved-report mutation.

- [ ] **Step 2: Add Playwright workflow**

Add a browser test that opens “自定义报表”, selects two points, queries, sees the `Asia/Shanghai` label and PV/UV columns, saves a named report as Admin, and verifies the download event for Excel. Add a diagnostics assertion that the displayed timestamp has no `T` or trailing `Z`.

- [ ] **Step 3: Update README without discarding existing user edits**

Merge around the current worktree version of `README.md`. Document:

- migration and restart commands;
- the “自定义报表” navigation path;
- temporary versus saved reports;
- Admin and Viewer permissions;
- 30-day default, 90-day/100-point limits;
- Asia/Shanghai date boundary and incomplete-current-day marker;
- Excel sheet names and formula-safety behavior;
- UTC storage versus Asia/Shanghai display;
- direct API examples for query and export.

- [ ] **Step 4: Run complete verification**

Run:

```bash
go test ./... -count=1
go test -race ./... -count=1
cd web
npm test -- --run
npm run typecheck
npm run build
npm run test:e2e
```

Expected: all Go tests, race tests, frontend tests, typecheck, build, and Playwright tests pass. If Docker acceptance is available, also run the documented isolated acceptance compose and confirm the XLSX opens with both expected sheets.

- [ ] **Step 5: Check repository hygiene**

Run:

```bash
git diff --check
git status --short
rg -n '\[DEBUG-' internal web/src tests README.md
```

Expected: no whitespace errors, no debugging instrumentation or unfinished markers, and only intentional task files remain modified.

- [ ] **Step 6: Commit final acceptance and documentation**

```bash
git add tests/integration/tracking_flow_test.go web/e2e/statistics-and-diagnostics.spec.ts README.md .superpowers/sdd/progress.md
git commit -m "test: verify custom reports end to end"
```

---

## Final Review Checklist

- The fixed formatter is used for every absolute timestamp named in the design.
- API and database timestamps remain UTC.
- Saved-report reads and mutations bind `project_id`; Admin/Viewer permissions match the matrix.
- Optimistic PATCH uses `expected_updated_at` and maps stale writes to 409.
- Mixed exact and approximate points are queried separately and returned together without UV merging.
- Missing date/point pairs are present as zero rows; current Shanghai day is incomplete.
- Query and export share the same service result.
- XLSX contains exactly `数据明细` and `报表说明`, and user text cannot become a formula.
- Vue temporary edits do not overwrite saved reports without an explicit Admin save.
- Existing README edits are preserved and integrated rather than replaced.
