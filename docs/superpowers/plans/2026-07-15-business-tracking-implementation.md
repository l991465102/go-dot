# Go Dot Business Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a deployable GoFrame business tracking platform with governed tracking-point definitions, Redis Stream ingestion, idempotent real-time PV/UV, ClickHouse history, and a Vue 3 management console.

**Architecture:** Use one Go module with API, Worker, and Scheduler commands. Domain packages depend on explicit repository, queue, event-store, and real-time-counter ports; MySQL, Redis, and ClickHouse remain adapters. The Vue SPA calls versioned management and collection APIs, while Docker Compose supplies the standard development and acceptance environment.

**Tech Stack:** Go 1.26.5, GoFrame v2.10.2, MySQL 8.4 LTS, Redis 8, ClickHouse 26.3 LTS, Vue 3, TypeScript, Vite, Pinia, Element Plus, Apache ECharts, Vitest, Playwright, k6

## Global Constraints

- Go module path is `github.com/lzw7758/go-dot`; changing the eventual Git remote must not change package responsibilities.
- Use Go 1.26.5 and pin `github.com/gogf/gf/v2` to v2.10.2 in `go.mod`.
- Pin backend direct dependencies to `go-redis/v9` v9.21.0, `clickhouse-go/v2` v2.47.0, `jwt/v5` v5.3.1, `go-sql-driver/mysql` v1.10.0, `client_golang` v1.23.2, `testify` v1.11.1, and `x/crypto` v0.54.0.
- Use Node 24 LTS for frontend builds; commit `web/package-lock.json`.
- API, Worker, and Scheduler are separate commands from one binary: `go-dot api`, `go-dot worker`, `go-dot scheduler`.
- MySQL stores accounts and metadata; Redis provides Redis Stream and three-day real-time statistics; ClickHouse stores 30-day event detail and long-lived hourly snapshots.
- Point categories have exactly three levels: `DOMAIN`, `MODULE`, `SCENE`; points attach only to `SCENE`.
- Point definition states are `DRAFT`, `PUBLISHED`, `DEPRECATED`; development states are `PENDING`, `DEVELOPING`, `TESTING`, `ONLINE`.
- Published point definitions are immutable. A breaking change creates a copied point with a new code.
- Initial account is `admin/admin` and has `must_change_password=true`; management writes remain forbidden until the password changes.
- Collection batches contain 1-100 events and at most 256KB; `event_id` is mandatory and stable across retries.
- Redis real-time data must become visible within 5 seconds; ClickHouse detail and hourly snapshot data within 1 minute.
- ClickHouse raw and invalid event tables use a 30-day TTL; aggregate snapshots do not expire.
- Default UV mode is HyperLogLog-compatible approximation; selected points can use exact Redis Set and ClickHouse `uniqExactState` storage.
- Unknown event properties are rejected. Arbitrary properties never become real-time dimensions.
- Every task follows red-green-refactor, runs focused tests before the full suite, and ends in a reviewable commit.

---

## Planned File Map

### Backend entrypoints and configuration

- `main.go`: binary entrypoint.
- `internal/cmd/root.go`: selects API, Worker, or Scheduler.
- `internal/cmd/api.go`: constructs the HTTP application and routes.
- `internal/cmd/worker.go`: constructs consumers and blocks until shutdown.
- `internal/cmd/scheduler.go`: constructs periodic jobs and blocks until shutdown.
- `internal/config/config.go`: typed environment-backed configuration.
- `manifest/config/config.yaml`: non-secret local defaults.

### Domain and application modules

- `internal/auth/`: password, token, first-login, and project authorization rules.
- `internal/project/`: projects, members, write-key rotation, origins, and time zones.
- `internal/tracking/`: categories, points, snapshots, property schemas, and collection validation.
- `internal/statistics/`: visitor hashing, Redis keys, query ranges, and result models.
- `internal/ingestion/`: batch normalization, Worker orchestration, retry classification, and Scheduler jobs.
- `internal/ports/`: stable interfaces for metadata, queue, raw-event storage, counters, clock, and token issuance.

### Adapters

- `internal/adapter/mysql/`: GoFrame `gdb` repositories and transaction helpers.
- `internal/adapter/redisqueue/`: Redis Stream implementation of `ports.MessageQueue`.
- `internal/adapter/rediscounter/`: Lua-backed idempotent PV/UV implementation.
- `internal/adapter/clickhouse/`: raw, invalid, aggregate, and diagnostic storage.
- `internal/adapter/migration/`: checksum-aware MySQL and ClickHouse migration runner.
- `internal/adapter/httpapi/`: middleware, controllers, DTO mapping, and consistent errors.

### Contracts, migrations, deployment, and tests

- `api/*/v1/`: GoFrame request and response structs.
- `migrations/mysql/*.sql`: MySQL metadata schema.
- `migrations/clickhouse/*.sql`: ClickHouse raw and aggregate schema.
- `deploy/Dockerfile.backend`, `deploy/Dockerfile.web`, `docker-compose.yml`: standard environment.
- `tests/integration/`: container-backed backend acceptance tests.
- `tests/fault/`: ClickHouse outage, Pending recovery, idempotency, and DLQ scripts.
- `tests/load/collect.js`: k6 collection workload.

### Frontend

- `web/src/api/`: typed HTTP client modules.
- `web/src/stores/`: auth and active-project stores.
- `web/src/router/`: auth, first-password-change, and role guards.
- `web/src/layouts/`: project-aware application shell.
- `web/src/views/`: login, overview, point workbench, category, statistics, diagnostics, and project settings pages.
- `web/src/components/points/`: four-step point wizard and point detail components.
- `web/e2e/`: Playwright acceptance flows.

---

### Task 1: Bootstrap the GoFrame binary and typed configuration

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `internal/cmd/root.go`
- Create: `internal/cmd/api.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `api/system/v1/health.go`
- Create: `internal/adapter/httpapi/system_controller.go`
- Create: `manifest/config/config.yaml`
- Create: `.env.example`
- Create: `Makefile`

**Interfaces:**
- Produces: `config.Load(ctx context.Context) (config.Config, error)`.
- Produces: `cmd.Run(ctx context.Context, args []string) error`.
- Produces: liveness response `GET /health/live -> {"status":"ok"}`.

- [ ] **Step 1: Initialize the module and write a failing configuration test**

Create `go.mod` with this exact direct dependency baseline; `go mod tidy` will add indirect dependencies:

```go
module github.com/lzw7758/go-dot

go 1.26.0

toolchain go1.26.5

require (
    github.com/ClickHouse/clickhouse-go/v2 v2.47.0
    github.com/go-sql-driver/mysql v1.10.0
    github.com/gogf/gf/v2 v2.10.2
    github.com/golang-jwt/jwt/v5 v5.3.1
    github.com/prometheus/client_golang v1.23.2
    github.com/redis/go-redis/v9 v9.21.0
    github.com/stretchr/testify v1.11.1
    golang.org/x/crypto v0.54.0
)
```

The `toolchain` directive upgrades the current Go 1.24.5 host tool automatically before tests. Add this test to `internal/config/config_test.go`:

```go
func TestLoadRejectsMissingSecrets(t *testing.T) {
    t.Setenv("GO_DOT_JWT_SECRET", "")
    t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "")
    _, err := Load(context.Background())
    require.ErrorContains(t, err, "GO_DOT_JWT_SECRET")
}
```

- [ ] **Step 2: Verify the test fails before implementation**

Run: `go test ./internal/config -run TestLoadRejectsMissingSecrets -v`  
Expected: FAIL because `Load` is undefined.

- [ ] **Step 3: Implement exact typed configuration and validation**

Define the configuration shape in `internal/config/config.go`:

```go
type Config struct {
    HTTP       HTTPConfig
    MySQL      MySQLConfig
    Redis      RedisConfig
    ClickHouse ClickHouseConfig
    Auth       AuthConfig
    Tracking   TrackingConfig
}

type AuthConfig struct {
    JWTSecret        string
    VisitorMasterKey string
    AccessTTL        time.Duration
    RefreshTTL       time.Duration
}

func Load(ctx context.Context) (Config, error) {
    cfg := Config{
        HTTP: HTTPConfig{Address: g.Cfg().MustGet(ctx, "server.address", ":8080").String()},
        MySQL: MySQLConfig{DSN: os.Getenv("GO_DOT_MYSQL_DSN")},
        Redis: RedisConfig{Address: envOr("GO_DOT_REDIS_ADDRESS", "redis:6379")},
        ClickHouse: ClickHouseConfig{DSN: os.Getenv("GO_DOT_CLICKHOUSE_DSN")},
        Auth: AuthConfig{
            JWTSecret: os.Getenv("GO_DOT_JWT_SECRET"),
            VisitorMasterKey: os.Getenv("GO_DOT_VISITOR_MASTER_KEY"),
            AccessTTL: 15 * time.Minute,
            RefreshTTL: 7 * 24 * time.Hour,
        },
        Tracking: TrackingConfig{MaxBatchEvents: 100, MaxBodyBytes: 256 << 10},
    }
    if cfg.Auth.JWTSecret == "" {
        return Config{}, errors.New("GO_DOT_JWT_SECRET is required")
    }
    if cfg.Auth.VisitorMasterKey == "" {
        return Config{}, errors.New("GO_DOT_VISITOR_MASTER_KEY is required")
    }
    return cfg, nil
}
```

Put concrete local values in `.env.example`: MySQL database `go_dot`, Redis address `redis:6379`, ClickHouse database `go_dot`, JWT secret `local-jwt-secret-change-before-production`, and visitor key `local-visitor-key-change-before-production`.

- [ ] **Step 4: Add command dispatch and liveness HTTP route**

Implement `cmd.Run` so unknown or missing commands return `usage: go-dot <api|worker|scheduler>`. Implement `runAPI` with GoFrame `g.Server`, bind `GET /health/live`, and return `v1.HealthRes{Status: "ok"}`. Do not connect to databases in the liveness handler.

- [ ] **Step 5: Run focused and repository tests**

Run: `go test ./internal/config ./internal/cmd ./internal/adapter/httpapi -v`  
Expected: PASS.  
Run: `go test ./...`  
Expected: PASS.

- [ ] **Step 6: Commit the runnable foundation**

```bash
git add go.mod go.sum main.go internal api manifest .env.example Makefile
git commit -m "feat: bootstrap goframe service"
```

### Task 2: Add checksum-aware migrations and Docker Compose infrastructure

**Files:**
- Create: `internal/adapter/migration/runner.go`
- Create: `internal/adapter/migration/runner_test.go`
- Create: `migrations/mysql/0001_metadata.sql`
- Create: `migrations/clickhouse/0001_events.sql`
- Create: `migrations/clickhouse/0002_hourly_stats.sql`
- Create: `deploy/Dockerfile.backend`
- Create: `deploy/Dockerfile.web`
- Create: `docker-compose.yml`
- Create: `deploy/clickhouse/users.xml`
- Modify: `internal/cmd/root.go`
- Modify: `Makefile`

**Interfaces:**
- Produces: `migration.Run(ctx context.Context, db Executor, dialect Dialect, files fs.FS) error`.
- Produces: command `go-dot migrate` for both databases.
- Produces: Compose services `mysql`, `redis`, `clickhouse`, `api`, `worker`, `scheduler`, `web`.

- [ ] **Step 1: Write migration-order and checksum tests**

Use an in-memory fake `Executor` and assert these behaviors in `runner_test.go`:

```go
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
```

- [ ] **Step 2: Run tests and confirm red state**

Run: `go test ./internal/adapter/migration -v`  
Expected: FAIL because `Run`, `Executor`, and `Dialect` do not exist.

- [ ] **Step 3: Implement the migration runner**

Implement lexical ordering, SHA-256 checksums, a `schema_migrations(name, checksum, applied_at)` table, and one migration transaction for MySQL. For ClickHouse, execute statements sequentially and record the migration only after all statements succeed. The runner must reject an applied filename whose checksum changed.

- [ ] **Step 4: Create the complete initial schemas**

`0001_metadata.sql` creates `user_account`, `refresh_token`, `project`, `project_member`, `tracking_category`, `tracking_point`, `tracking_point_property`, `tracking_point_snapshot`, and `audit_log` with the unique keys from the design. Use `utf8mb4_0900_ai_ci`, UTC timestamps, `BIGINT UNSIGNED` identifiers, and JSON columns for schema snapshots.

`0001_events.sql` creates `tracking_event` and `tracking_invalid_event` with monthly partitions, `ReplacingMergeTree(version)`, low-cardinality platform/channel columns, and 30-day TTL expressions.

`0002_hourly_stats.sql` creates `business_stat_hourly` and `business_stat_exact_hourly` with `ReplacingMergeTree(version)`, `pv UInt64`, and aggregate-state columns for approximate and exact UV.

- [ ] **Step 5: Add the migration command and Compose health dependencies**

Pin images to `mysql:8.4`, `redis:8`, `clickhouse/clickhouse-server:26.3`, `golang:1.26.5`, and `node:24`. `api`, `worker`, and `scheduler` must wait for successful MySQL, Redis, and ClickHouse health checks and a completed `migrate` service. Persist database data in named volumes.

- [ ] **Step 6: Verify schema creation from empty volumes**

Run: `docker compose config`  
Expected: exit 0 and seven application/infrastructure services plus `migrate`.  
Run: `docker compose up -d mysql redis clickhouse migrate`  
Expected: `migrate` exits 0; database health checks report healthy.  
Run: `docker compose run --rm migrate`  
Expected: exit 0 with no migration reapplied.

- [ ] **Step 7: Commit infrastructure and migrations**

```bash
git add internal/adapter/migration migrations deploy docker-compose.yml Makefile internal/cmd/root.go
git commit -m "feat: add database migrations and compose stack"
```

### Task 3: Implement authentication, first-password change, projects, and project roles

**Files:**
- Create: `internal/auth/model.go`
- Create: `internal/auth/password.go`
- Create: `internal/auth/service.go`
- Create: `internal/auth/service_test.go`
- Create: `internal/project/model.go`
- Create: `internal/project/service.go`
- Create: `internal/project/service_test.go`
- Create: `internal/ports/auth_repository.go`
- Create: `internal/ports/project_repository.go`
- Create: `internal/adapter/mysql/auth_repository.go`
- Create: `internal/adapter/mysql/project_repository.go`
- Create: `internal/adapter/httpapi/auth_controller.go`
- Create: `internal/adapter/httpapi/project_controller.go`
- Create: `internal/adapter/httpapi/auth_middleware.go`
- Create: `api/auth/v1/auth.go`
- Create: `api/projects/v1/projects.go`
- Modify: `internal/cmd/api.go`

**Interfaces:**
- Produces: `auth.Service.Login(ctx, username, password) (auth.Session, error)`.
- Produces: `auth.Service.ChangePassword(ctx, userID, current, next string) error`.
- Produces: `project.Service.RequireRole(ctx, userID, projectID uint64, allowed ...project.Role) error`.
- Produces: `project.Service.RotateWriteKey(ctx, actorID, projectID uint64) (project.WriteKeyRotation, error)`.

- [ ] **Step 1: Write domain tests for bootstrap login and authorization**

Add table-driven tests that assert:

```go
func TestInitialAdminMustChangePassword(t *testing.T) {
    svc, repo := newAuthFixture(t)
    require.NoError(t, svc.EnsureInitialAdmin(context.Background(), "admin", "admin"))
    session, err := svc.Login(context.Background(), "admin", "admin")
    require.NoError(t, err)
    require.True(t, session.MustChangePassword)
    require.Equal(t, "admin", repo.UserByName("admin").Username)
}

func TestRequireRoleRejectsViewerWrite(t *testing.T) {
    svc := newProjectFixture(t, project.Member{UserID: 7, ProjectID: 9, Role: project.Viewer})
    err := svc.RequireRole(context.Background(), 7, 9, project.Admin)
    require.ErrorIs(t, err, project.ErrForbidden)
}
```

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./internal/auth ./internal/project -v`  
Expected: FAIL because services and models are undefined.

- [ ] **Step 3: Implement password, session, and first-login rules**

Use Argon2id with explicit encoded parameters. Store hashed refresh tokens, rotate them on refresh, and revoke them on logout. Access tokens last 15 minutes; refresh tokens last 7 days. Middleware must reject all management write methods while `must_change_password=true`, except `POST /api/v1/auth/change-password` and `POST /api/v1/auth/logout`.

- [ ] **Step 4: Implement project membership and write-key rotation**

Use 32 random bytes encoded with URL-safe base64 for write keys. Store only SHA-256 hashes. Rotation sets `previous_write_key_hash` and `previous_write_key_expires_at=now+24h`. A project creator becomes its first `ADMIN`. `VIEWER` cannot mutate settings, members, categories, or points.

- [ ] **Step 5: Add HTTP contracts and middleware tests**

Test with `httptest` that `admin/admin` login returns `must_change_password=true`, an attempted `POST /api/v1/projects` returns 403 before password change, password change returns 204, and the same project creation request then returns 201. Test a Viewer write request returns 403 even with a valid access token.

- [ ] **Step 6: Run focused and integration tests**

Run: `go test ./internal/auth ./internal/project ./internal/adapter/httpapi -v`  
Expected: PASS.  
Run: `go test ./...`  
Expected: PASS.

- [ ] **Step 7: Commit authentication and project isolation**

```bash
git add internal/auth internal/project internal/ports internal/adapter/mysql internal/adapter/httpapi api/auth api/projects internal/cmd/api.go
git commit -m "feat: add authentication and project roles"
```

### Task 4: Implement categories, point drafts, immutable publication, and snapshots

**Files:**
- Create: `internal/tracking/category.go`
- Create: `internal/tracking/point.go`
- Create: `internal/tracking/property.go`
- Create: `internal/tracking/service.go`
- Create: `internal/tracking/service_test.go`
- Create: `internal/ports/tracking_repository.go`
- Create: `internal/adapter/mysql/tracking_repository.go`
- Create: `internal/adapter/httpapi/tracking_controller.go`
- Create: `api/tracking/v1/categories.go`
- Create: `api/tracking/v1/points.go`
- Modify: `internal/cmd/api.go`

**Interfaces:**
- Produces: `tracking.Service.CreateCategory(ctx, actorID uint64, in tracking.CreateCategory) (tracking.Category, error)`.
- Produces: `tracking.Service.SaveDraft(ctx, actorID uint64, in tracking.DraftInput) (tracking.Point, error)`.
- Produces: `tracking.Service.UpdateDraft(ctx, actorID, pointID uint64, in tracking.DraftInput) (tracking.Point, error)`.
- Produces: `tracking.Service.DeleteDraft(ctx, actorID, pointID uint64) error`.
- Produces: `tracking.Service.Publish(ctx, actorID, pointID uint64) (tracking.Snapshot, error)`.
- Produces: `tracking.Service.Deprecate(ctx, actorID, pointID uint64, reason string, replacementID *uint64) error`.
- Produces: `tracking.Service.Copy(ctx, actorID, pointID uint64, newCode string) (tracking.Point, error)`.

- [ ] **Step 1: Write state-machine and validation tests**

Cover these exact cases:

```go
func TestPublishRequiresSceneAndCompleteSchema(t *testing.T) {
    svc := newTrackingFixture(t)
    point := validDraft()
    point.Category.Level = tracking.Module
    _, err := svc.Publish(context.Background(), 1, point.ID)
    require.ErrorContains(t, err, "SCENE category")
}

func TestPublishedPointCannotBeEditedOrDeleted(t *testing.T) {
    svc := newTrackingFixtureWithPublishedPoint(t)
    require.ErrorIs(t, svc.UpdateDraft(context.Background(), 1, publishedPointID, validDraftInput()), tracking.ErrImmutable)
    require.ErrorIs(t, svc.DeleteDraft(context.Background(), 1, publishedPointID), tracking.ErrImmutable)
}

func TestUnknownPropertyIsRejectedBySnapshot(t *testing.T) {
    snapshot := publishedProductViewSnapshot()
    err := snapshot.ValidateProperties(map[string]any{"product_id": "123", "extra": true})
    require.ErrorContains(t, err, "UNKNOWN_PROPERTY")
}
```

- [ ] **Step 2: Verify red state**

Run: `go test ./internal/tracking -v`  
Expected: FAIL because tracking types and services do not exist.

- [ ] **Step 3: Implement category and point domain rules**

Represent levels and states with typed strings and constructor validation. Publish in one MySQL transaction: lock the draft, validate a third-level active category, validate point code and property schema, insert a canonical JSON snapshot, update the point to `PUBLISHED`, and insert an audit record. No repository method may update core point fields unless the persisted state is `DRAFT`.

- [ ] **Step 4: Implement property validation**

Validate required fields, exact JSON types, RFC3339 datetime strings, enum membership, array element type, and unknown fields. Return stable error codes: `POINT_NOT_FOUND`, `POINT_NOT_PUBLISHED`, `PLATFORM_NOT_SUPPORTED`, `PROPERTY_REQUIRED`, `PROPERTY_TYPE_ERROR`, `PROPERTY_ENUM_INVALID`, `UNKNOWN_PROPERTY`.

- [ ] **Step 5: Add API tests for full lifecycle**

Create a Domain, Module, and Scene; save a draft; publish it; verify PATCH and DELETE return 409; copy it with `product_detail_view_v2`; deprecate the original with a reason and replacement ID; verify both remain queryable.

- [ ] **Step 6: Run all tracking tests and commit**

Run: `go test ./internal/tracking ./internal/adapter/mysql ./internal/adapter/httpapi -v`  
Expected: PASS.  
Run: `go test ./...`  
Expected: PASS.

```bash
git add internal/tracking internal/ports/tracking_repository.go internal/adapter/mysql/tracking_repository.go internal/adapter/httpapi/tracking_controller.go api/tracking internal/cmd/api.go
git commit -m "feat: add governed tracking point lifecycle"
```

### Task 5: Define the message-queue contract and Redis Stream adapter

**Files:**
- Create: `internal/ports/message_queue.go`
- Create: `internal/ports/message_queue_contract_test.go`
- Create: `internal/adapter/redisqueue/queue.go`
- Create: `internal/adapter/redisqueue/queue_test.go`
- Create: `internal/ingestion/message.go`
- Modify: `internal/config/config.go`

**Interfaces:**
- Produces: the queue contract below.

```go
type Message struct {
    ID          string
    Stream      string
    BatchID     string
    Payload     []byte
    Deliveries  int
    LastError   string
}

type BatchEnvelope struct {
    BatchID   string
    Payload   []byte
    CreatedAt time.Time
}

type MessageQueue interface {
    PublishBatch(ctx context.Context, stream string, batch BatchEnvelope) (string, error)
    Consume(ctx context.Context, stream, group, consumer string, count int, block time.Duration) ([]Message, error)
    Ack(ctx context.Context, stream, group string, ids ...string) error
    Retry(ctx context.Context, stream, group string, msg Message, cause error) error
    MoveToDLQ(ctx context.Context, sourceStream, group string, msg Message, cause error) error
    ReclaimStale(ctx context.Context, stream, group, consumer string, idle time.Duration, count int) ([]Message, error)
}
```

- [ ] **Step 1: Write a reusable contract test suite**

Create `ports.RunMessageQueueContract(t, factory)` and assert publication order, blocking consume, ACK removal from Pending, stale reclaim, delivery count increments, and DLQ payload preservation. The Redis adapter test calls the suite against the Compose Redis service.

- [ ] **Step 2: Run the contract suite in red state**

Run: `go test ./internal/adapter/redisqueue -v`  
Expected: FAIL because `redisqueue.New` is undefined.

- [ ] **Step 3: Implement Redis Stream semantics**

Use `XADD`, `XREADGROUP`, `XACK`, `XAUTOCLAIM`, and a Lua script that writes the dead-letter entry before ACKing the source. Ensure consumer groups are created with `MKSTREAM`. Store `batch_id`, JSON payload, deliveries, first-seen time, and last error as fields.

- [ ] **Step 4: Prove Pending recovery and DLQ behavior**

Run: `go test ./internal/adapter/redisqueue -run Contract -v`  
Expected: PASS with Redis available.  
Run: `go test ./internal/adapter/redisqueue -run TestMoveToDLQIsAtomic -v`  
Expected: PASS.

- [ ] **Step 5: Commit the replaceable queue seam**

```bash
git add internal/ports/message_queue.go internal/ports/message_queue_contract_test.go internal/adapter/redisqueue internal/ingestion/message.go internal/config/config.go
git commit -m "feat: add redis stream queue adapter"
```

### Task 6: Implement the collection API and synchronous point-schema validation

**Files:**
- Create: `api/collect/v1/events.go`
- Create: `internal/ingestion/collector.go`
- Create: `internal/ingestion/collector_test.go`
- Create: `internal/tracking/snapshot_cache.go`
- Create: `internal/adapter/httpapi/collect_controller.go`
- Create: `internal/adapter/httpapi/write_key_middleware.go`
- Create: `internal/adapter/httpapi/rate_limit_middleware.go`
- Modify: `internal/cmd/api.go`

**Interfaces:**
- Consumes: `ports.MessageQueue.PublishBatch` from Task 5.
- Consumes: published `tracking.Snapshot` from Task 4.
- Produces: `ingestion.Collector.Collect(ctx, project, request) (ingestion.CollectResult, error)`.
- Produces: `POST /api/v1/collect/events` with per-event results.

- [ ] **Step 1: Write collection behavior tests**

Test one batch containing a valid event, an unknown point, and an event with an invalid enum. Assert `accepted=1`, `rejected=2`, one effective queue batch, one invalid-event queue batch, stable `batch_id`, and no trusted project ID from the request body.

```go
func TestCollectSeparatesValidAndInvalidEvents(t *testing.T) {
    collector, queue := newCollectorFixture(t, publishedProductViewSnapshot())
    result, err := collector.Collect(context.Background(), projectFixture(), mixedRequest())
    require.NoError(t, err)
    require.Equal(t, 1, result.Accepted)
    require.Equal(t, 2, result.Rejected)
    require.Len(t, queue.Published("tracking:{queue}:events"), 1)
    require.Len(t, queue.Published("tracking:{queue}:invalid"), 1)
}
```

- [ ] **Step 2: Verify red state**

Run: `go test ./internal/ingestion -run TestCollect -v`  
Expected: FAIL because `Collector` is undefined.

- [ ] **Step 3: Implement normalization and synchronous validation**

Reject bodies over 256KB before JSON decoding. Require 1-100 events, UUIDv7 or ULID event IDs, supported platforms, and at least one visitor identifier. Load snapshots through a 60-second in-process cache backed by MySQL and invalidated on publish/deprecate. Attach snapshot ID, category codes, UV mode, project time zone, server receive time, and `time_corrected` to accepted messages.

- [ ] **Step 4: Implement write-key, Origin, and rate-limit middleware**

Hash `X-Write-Key` before lookup. Accept the previous key only before its stored expiry. Enforce configured Origins for Web requests. Use a Redis Lua token bucket keyed by project, IP, and device header. Return 401 for invalid keys, 403 for disallowed Origins, 413 for oversized bodies, and 429 with `Retry-After` for limits.

- [ ] **Step 5: Add HTTP contract tests**

Use `httptest` for valid batch 202, partial batch 202 with per-event errors, invalid key 401, disallowed Origin 403, body 257KB 413, and exhausted token bucket 429.

- [ ] **Step 6: Run tests and commit**

Run: `go test ./internal/ingestion ./internal/adapter/httpapi -run 'Collect|WriteKey|RateLimit' -v`  
Expected: PASS.  
Run: `go test ./...`  
Expected: PASS.

```bash
git add api/collect internal/ingestion internal/tracking/snapshot_cache.go internal/adapter/httpapi internal/cmd/api.go
git commit -m "feat: add validated event collection api"
```

### Task 7: Implement ClickHouse event storage, visitor hashing, and idempotent Redis counters

**Files:**
- Create: `internal/ports/event_store.go`
- Create: `internal/ports/realtime_counter.go`
- Create: `internal/statistics/visitor.go`
- Create: `internal/statistics/visitor_test.go`
- Create: `internal/adapter/clickhouse/event_store.go`
- Create: `internal/adapter/clickhouse/event_store_test.go`
- Create: `internal/adapter/rediscounter/counter.go`
- Create: `internal/adapter/rediscounter/update.lua`
- Create: `internal/adapter/rediscounter/counter_test.go`

**Interfaces:**
- Produces: `EventStore.SaveValid(ctx, token string, events []StoredEvent) error`.
- Produces: `EventStore.SaveInvalid(ctx, token string, events []InvalidEvent) error`.
- Produces: `RealtimeCounter.Apply(ctx, event CountEvent) (applied bool, err error)`.
- Produces: `statistics.HashVisitor(masterKey, projectSalt []byte, userID, anonymousID string) uint64`.

- [ ] **Step 1: Write deterministic visitor and counter-idempotency tests**

```go
func TestHashVisitorPrefersUserAndSeparatesProjects(t *testing.T) {
    a := HashVisitor([]byte("master"), []byte("project-a"), "42", "device")
    b := HashVisitor([]byte("master"), []byte("project-b"), "42", "device")
    require.NotEqual(t, a, b)
    require.Equal(t, a, HashVisitor([]byte("master"), []byte("project-a"), "42", "other-device"))
}

func TestApplyCountsEventOnce(t *testing.T) {
    counter := redisCounterFixture(t)
    first, err := counter.Apply(context.Background(), countEvent("evt-1"))
    require.NoError(t, err)
    second, err := counter.Apply(context.Background(), countEvent("evt-1"))
    require.NoError(t, err)
    require.True(t, first)
    require.False(t, second)
    require.EqualValues(t, 1, counter.PV(t, dayKey()))
}
```

- [ ] **Step 2: Confirm tests fail**

Run: `go test ./internal/statistics ./internal/adapter/rediscounter -v`  
Expected: FAIL because hashing and counter implementations are absent.

- [ ] **Step 3: Implement visitor HMAC and Redis Lua**

Hash `u:<user_id>` when user ID is present, otherwise `a:<anonymous_id>`, using HMAC-SHA256 over the project salt and master key, then read the first 8 bytes as big-endian `uint64`. The Lua script must atomically check a 72-hour event completion marker, increment hour/day PV keys, add to HLL or Set hour/day UV keys, set three-day TTLs, and create the completion marker.

- [ ] **Step 4: Implement reliable ClickHouse inserts**

Use ClickHouse asynchronous insert settings `async_insert=1` and `wait_for_async_insert=1`. Pass the queue `batch_id` as `insert_deduplication_token`. Serialize properties once as canonical JSON. Test that inserting the same token twice leaves one logical row under `FINAL`, and invalid events preserve error code and original payload.

- [ ] **Step 5: Run adapter tests**

Run: `go test ./internal/statistics ./internal/adapter/rediscounter ./internal/adapter/clickhouse -v`  
Expected: PASS with Redis and ClickHouse available.

- [ ] **Step 6: Commit storage and counter adapters**

```bash
git add internal/ports/event_store.go internal/ports/realtime_counter.go internal/statistics internal/adapter/clickhouse internal/adapter/rediscounter
git commit -m "feat: add clickhouse events and realtime counters"
```

### Task 8: Implement Worker orchestration, retries, Pending recovery, and DLQ

**Files:**
- Create: `internal/ingestion/worker.go`
- Create: `internal/ingestion/worker_test.go`
- Create: `internal/ingestion/errors.go`
- Create: `internal/ingestion/retry.go`
- Modify: `internal/cmd/worker.go`
- Modify: `internal/cmd/root.go`

**Interfaces:**
- Consumes: `MessageQueue`, `EventStore`, and `RealtimeCounter` from Tasks 5 and 7.
- Produces: `ingestion.Worker.Run(ctx context.Context) error`.
- Produces: `ingestion.Worker.Handle(ctx context.Context, msg ports.Message) error` for focused tests.

- [ ] **Step 1: Write orchestration tests with fakes**

Test exact ordering and outcomes:

```go
func TestWorkerStoresThenCountsThenAcks(t *testing.T) {
    worker, log := workerFixture(t)
    require.NoError(t, worker.Handle(context.Background(), validMessage()))
    require.Equal(t, []string{"store-valid", "count:evt-1", "ack"}, log.Entries())
}

func TestWorkerRetriesInfrastructureFailureWithoutAck(t *testing.T) {
    worker, queue := workerWithStoreError(t, errors.New("clickhouse unavailable"))
    require.Error(t, worker.Handle(context.Background(), validMessage()))
    require.Equal(t, 1, queue.RetryCount())
    require.Equal(t, 0, queue.AckCount())
}

func TestWorkerMovesFifthFailureToDLQ(t *testing.T) {
    worker, queue := workerWithDeliveries(t, 5)
    require.NoError(t, worker.Handle(context.Background(), invalidInfrastructureMessage()))
    require.Equal(t, 1, queue.DLQCount())
    require.Equal(t, 1, queue.AckCount())
}
```

- [ ] **Step 2: Verify red state**

Run: `go test ./internal/ingestion -run TestWorker -v`  
Expected: FAIL because `Worker` is undefined.

- [ ] **Step 3: Implement processing and error classification**

Decode and validate the immutable snapshot version, normalize storage rows, save ClickHouse first, apply Redis counters per valid event, then ACK. Classify point/schema failures as permanent invalid events and adapter timeouts/connections as retryable. Treat duplicate completion markers as successful no-op counts.

- [ ] **Step 4: Implement worker loop and graceful shutdown**

Read up to 16 Stream messages per call with a 2-second block. Limit concurrent batch handlers to configured worker concurrency. Stop accepting new messages on context cancellation, wait up to 30 seconds for active handlers, and leave unfinished messages Pending.

- [ ] **Step 5: Run focused tests and commit**

Run: `go test ./internal/ingestion -run 'TestWorker|TestRetry' -v`  
Expected: PASS.  
Run: `go test ./...`  
Expected: PASS.

```bash
git add internal/ingestion internal/cmd/worker.go internal/cmd/root.go
git commit -m "feat: add reliable tracking worker"
```

### Task 9: Implement Scheduler aggregation and stale-message jobs

**Files:**
- Create: `internal/ports/statistics_store.go`
- Create: `internal/statistics/aggregator.go`
- Create: `internal/statistics/aggregator_test.go`
- Create: `internal/ingestion/scheduler.go`
- Create: `internal/ingestion/scheduler_test.go`
- Create: `internal/adapter/clickhouse/statistics_store.go`
- Create: `internal/adapter/clickhouse/statistics_store_test.go`
- Modify: `internal/cmd/scheduler.go`
- Modify: `internal/cmd/root.go`

**Interfaces:**
- Produces: `StatisticsStore.RefreshHour(ctx, hour time.Time, version uint64) error`.
- Produces: `StatisticsStore.QuerySummary(ctx, filter StatisticsFilter) (Summary, error)`.
- Produces: `StatisticsStore.QueryTrend(ctx, filter StatisticsFilter) ([]TrendPoint, error)`.
- Produces: `ingestion.Scheduler.Run(ctx context.Context) error`.

- [ ] **Step 1: Write aggregation correctness tests**

Insert raw rows where `event_id=evt-1` occurs in two batches, users A and B appear in two hours, and one exact-UV point receives both. Refresh each hour and assert PV counts distinct event IDs, day approximate UV merges A/B once, and exact UV equals 2.

- [ ] **Step 2: Confirm red state**

Run: `go test ./internal/statistics ./internal/adapter/clickhouse -run 'Aggregate|RefreshHour' -v`  
Expected: FAIL because hourly refresh is absent.

- [ ] **Step 3: Implement versioned hourly refresh**

Query `tracking_event FINAL` for the target project-hour range. Group by hour, project, category codes, point, platform, and channel. Compute `count()` after deduplication as exact PV, `uniqCombined64State(visitor_hash)` for approximate UV, and `uniqExactState(visitor_hash)` for exact points. Insert versioned rows into the matching `ReplacingMergeTree` table.

- [ ] **Step 4: Implement periodic jobs**

Every minute refresh the current and previous project-local hour. Every 15 seconds reclaim messages idle for at least 60 seconds. Every minute update oldest Pending age and DLQ counters. Every hour expire previous write keys whose 24-hour grace period ended. Jobs must use Redis leases so only one Scheduler instance executes each time bucket.

- [ ] **Step 5: Test state merging and stale reclaim**

Run: `go test ./internal/statistics ./internal/ingestion ./internal/adapter/clickhouse -run 'Aggregate|Scheduler|Reclaim' -v`  
Expected: PASS.

- [ ] **Step 6: Commit Scheduler and historical aggregation**

```bash
git add internal/ports/statistics_store.go internal/statistics internal/ingestion/scheduler.go internal/ingestion/scheduler_test.go internal/adapter/clickhouse/statistics_store.go internal/adapter/clickhouse/statistics_store_test.go internal/cmd/scheduler.go internal/cmd/root.go
git commit -m "feat: add scheduler and hourly statistics"
```

### Task 10: Add statistics, recent-event, invalid-event, and dead-letter APIs

**Files:**
- Create: `api/statistics/v1/statistics.go`
- Create: `api/diagnostics/v1/diagnostics.go`
- Create: `internal/statistics/service.go`
- Create: `internal/statistics/service_test.go`
- Create: `internal/adapter/httpapi/statistics_controller.go`
- Create: `internal/adapter/httpapi/diagnostics_controller.go`
- Create: `internal/adapter/httpapi/pagination.go`
- Modify: `internal/cmd/api.go`

**Interfaces:**
- Consumes: Redis real-time counters and ClickHouse `StatisticsStore`.
- Produces: `/statistics/realtime`, `/statistics/summary`, `/statistics/trend`.
- Produces: `/diagnostics/invalid-events`, `/diagnostics/dead-letters`, `/diagnostics/recent-events`.

- [ ] **Step 1: Write query-routing and UV-merge tests**

Assert `Realtime` calls only Redis, `Summary` and `Trend` call only ClickHouse, exact points use the exact table, invalid time ranges return 400, and Viewer access succeeds while cross-project access returns 403.

- [ ] **Step 2: Verify red state**

Run: `go test ./internal/statistics ./internal/adapter/httpapi -run 'Statistics|Diagnostics' -v`  
Expected: FAIL because the service and controllers are absent.

- [ ] **Step 3: Implement filters and responses**

Support project, start/end RFC3339 timestamps, granularity `hour|day`, category codes, point codes, platform, and channel. Limit trend ranges to 90 days and recent raw-event queries to the latest 30 days. Use cursor pagination for raw and invalid events; never expose unhashed user or anonymous IDs.

- [ ] **Step 4: Implement diagnostics and error mapping**

Expose DLQ metadata without a replay endpoint. Map validation to 400, unauthenticated to 401, forbidden to 403, missing resources to 404, immutable conflicts to 409, oversized input to 413, rate limits to 429, and unexpected errors to 500 with a request ID.

- [ ] **Step 5: Run API tests and commit**

Run: `go test ./internal/statistics ./internal/adapter/httpapi -run 'Statistics|Diagnostics' -v`  
Expected: PASS.  
Run: `go test ./...`  
Expected: PASS.

```bash
git add api/statistics api/diagnostics internal/statistics internal/adapter/httpapi internal/cmd/api.go
git commit -m "feat: expose statistics and diagnostics api"
```

### Task 11: Scaffold the Vue application, authentication, and project-aware shell

**Files:**
- Create: `web/package.json`
- Create: `web/package-lock.json`
- Create: `web/vite.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/src/main.ts`
- Create: `web/src/App.vue`
- Create: `web/src/api/client.ts`
- Create: `web/src/api/auth.ts`
- Create: `web/src/api/projects.ts`
- Create: `web/src/stores/auth.ts`
- Create: `web/src/stores/project.ts`
- Create: `web/src/router/index.ts`
- Create: `web/src/layouts/AppLayout.vue`
- Create: `web/src/views/LoginView.vue`
- Create: `web/src/views/ChangePasswordView.vue`
- Create: `web/src/views/ProjectOverviewView.vue`
- Create: `web/src/test/setup.ts`
- Create: `web/src/stores/auth.spec.ts`

**Interfaces:**
- Consumes: auth and project APIs from Task 3.
- Produces: authenticated Vue shell with project selector and role-aware navigation.

- [ ] **Step 1: Scaffold Vue with exact dependencies and write the first failing store test**

Use Vue 3, Vite, TypeScript, Pinia, Vue Router, Element Plus, Axios, Vitest, Vue Test Utils, Playwright, and ECharts. Test:

```ts
it('redirects a first-login session to change-password', async () => {
  const auth = useAuthStore()
  auth.setSession({ accessToken: 'token', mustChangePassword: true, user: adminUser })
  await router.push('/projects/1/overview')
  expect(router.currentRoute.value.name).toBe('change-password')
})
```

- [ ] **Step 2: Verify the test fails**

Run: `npm --prefix web test -- --run src/stores/auth.spec.ts`  
Expected: FAIL because the store and guard are absent.

- [ ] **Step 3: Implement auth storage and refresh behavior**

Keep access tokens in memory, rely on the HttpOnly refresh cookie after reload, retry one 401 through `/auth/refresh`, and redirect to login if refresh fails. Never put access or refresh tokens in localStorage. Persist only the last active project ID.

- [ ] **Step 4: Implement the A-layout shell**

Create a dark left navigation with Overview, Points, Categories, Statistics, Diagnostics, and Project Settings. Put the project selector and user menu in the top bar. Hide mutation actions for Viewers, but retain backend 403 enforcement.

- [ ] **Step 5: Run frontend checks and commit**

Run: `npm --prefix web run typecheck`  
Expected: PASS.  
Run: `npm --prefix web test -- --run`  
Expected: PASS.  
Run: `npm --prefix web run build`  
Expected: PASS.

```bash
git add web
git commit -m "feat: add authenticated management shell"
```

### Task 12: Build category management, point workbench, wizard, and detail views

**Files:**
- Create: `web/src/api/tracking.ts`
- Create: `web/src/views/PointWorkbenchView.vue`
- Create: `web/src/views/PointDetailView.vue`
- Create: `web/src/views/CategoryManagementView.vue`
- Create: `web/src/components/points/PointWizard.vue`
- Create: `web/src/components/points/BasicDefinitionStep.vue`
- Create: `web/src/components/points/TriggerStatisticsStep.vue`
- Create: `web/src/components/points/PropertySchemaStep.vue`
- Create: `web/src/components/points/CollaborationPublishStep.vue`
- Create: `web/src/components/points/CodeExamples.vue`
- Create: `web/src/components/points/PointWizard.spec.ts`
- Modify: `web/src/router/index.ts`

**Interfaces:**
- Consumes: category and point APIs from Task 4.
- Produces: A-layout point workbench and four-step draft/publish workflow.

- [ ] **Step 1: Write failing wizard tests**

Test that Step 1 rejects a non-Scene category, Step 3 rejects an enum without values, draft save calls `PATCH /draft`, publish calls `/publish`, and a published point renders no edit action.

```ts
it('prevents publishing an enum property without values', async () => {
  const wrapper = mountWizardWithDraft({ properties: [{ code: 'source', type: 'enum', enumValues: [] }] })
  await wrapper.get('[data-test=publish]').trigger('click')
  expect(wrapper.text()).toContain('枚举属性至少需要一个枚举值')
  expect(api.publishPoint).not.toHaveBeenCalled()
})
```

- [ ] **Step 2: Verify red state**

Run: `npm --prefix web test -- --run src/components/points/PointWizard.spec.ts`  
Expected: FAIL because point components do not exist.

- [ ] **Step 3: Implement category tree and workbench**

Keep the category tree visible beside the point list. Support code/name search, state, behavior type, platform, owner, and date filters. Reset filters on project change. Admins can create, rename, reorder, and disable categories subject to backend constraints.

- [ ] **Step 4: Implement four-step wizard and detail page**

Autosave only when the user presses Save Draft. Show a publication review containing category path, trigger rules, PV/UV mode, platforms, properties, owners, and generated JavaScript, Go, and cURL examples. Published points expose Copy and Deprecate; Deprecate requires a reason and optional replacement point.

- [ ] **Step 5: Run component tests, typecheck, and build**

Run: `npm --prefix web test -- --run src/components/points/PointWizard.spec.ts`  
Expected: PASS.  
Run: `npm --prefix web run typecheck`  
Expected: PASS.  
Run: `npm --prefix web run build`  
Expected: PASS.

- [ ] **Step 6: Commit the tracking collaboration UI**

```bash
git add web/src/api/tracking.ts web/src/views web/src/components/points web/src/router/index.ts
git commit -m "feat: add point governance workbench"
```

### Task 13: Build overview, statistics, diagnostics, and project settings pages

**Files:**
- Create: `web/src/api/statistics.ts`
- Create: `web/src/api/diagnostics.ts`
- Create: `web/src/views/StatisticsView.vue`
- Create: `web/src/views/DiagnosticsView.vue`
- Create: `web/src/views/ProjectSettingsView.vue`
- Modify: `web/src/views/ProjectOverviewView.vue`
- Create: `web/src/components/statistics/MetricCards.vue`
- Create: `web/src/components/statistics/TrendChart.vue`
- Create: `web/src/components/diagnostics/InvalidEventTable.vue`
- Create: `web/src/components/diagnostics/DeadLetterTable.vue`
- Create: `web/src/views/ProjectOverviewView.spec.ts`

**Interfaces:**
- Consumes: statistics and diagnostics APIs from Task 10 and project settings APIs from Task 3.
- Produces: operational C-style overview page and analysis/diagnostic/settings pages.

- [ ] **Step 1: Write failing overview tests**

Assert the page labels Redis cards as “实时”, ClickHouse trend data as “约 1 分钟延迟”, displays invalid and dead-letter counts, and does not add the Redis current-hour value to the ClickHouse trend series.

- [ ] **Step 2: Verify red state**

Run: `npm --prefix web test -- --run src/views/ProjectOverviewView.spec.ts`  
Expected: FAIL because metric components are absent.

- [ ] **Step 3: Implement overview and statistics views**

Render real-time PV/UV cards, hourly/day trend, invalid rate, DLQ count, queue lag, and no-report points. Statistics filters cover date range, category, point, platform, channel, and granularity. Use ECharts with explicit approximate/exact UV labels from the API.

- [ ] **Step 4: Implement diagnostics and project settings**

Render cursor-paginated invalid events, recent events, and DLQ metadata. Do not expose replay. Settings support members, project time zone, allowed Origins, current write-key rotation, and the 24-hour old-key expiry notice. Show write-key plaintext only once immediately after creation or rotation.

- [ ] **Step 5: Run frontend verification and commit**

Run: `npm --prefix web test -- --run`  
Expected: PASS.  
Run: `npm --prefix web run typecheck`  
Expected: PASS.  
Run: `npm --prefix web run build`  
Expected: PASS.

```bash
git add web/src/api web/src/views web/src/components/statistics web/src/components/diagnostics
git commit -m "feat: add statistics and operations console"
```

### Task 14: Add observability, end-to-end acceptance, fault recovery, load testing, and operations docs

**Files:**
- Create: `internal/observability/metrics.go`
- Create: `internal/observability/logging.go`
- Create: `internal/adapter/httpapi/health_controller.go`
- Create: `internal/adapter/httpapi/request_id_middleware.go`
- Create: `tests/integration/tracking_flow_test.go`
- Create: `tests/fault/clickhouse_recovery.sh`
- Create: `tests/fault/dlq.sh`
- Create: `tests/load/collect.js`
- Create: `web/playwright.config.ts`
- Create: `web/e2e/auth-and-point.spec.ts`
- Create: `web/e2e/statistics-and-diagnostics.spec.ts`
- Create: `docs/operations.md`
- Create: `README.md`
- Modify: `internal/cmd/api.go`
- Modify: `internal/cmd/worker.go`
- Modify: `internal/cmd/scheduler.go`
- Modify: `docker-compose.yml`
- Modify: `Makefile`

**Interfaces:**
- Produces: `/health/live`, `/health/ready`, `/metrics`.
- Produces: `make test`, `make test-integration`, `make test-e2e`, `make test-fault`, `make load-test`.
- Produces: reproducible local deployment and failure-recovery instructions.

- [ ] **Step 1: Write readiness and metrics tests**

Assert liveness stays 200 when dependencies fail, readiness returns 503 when any required dependency is unavailable, readiness returns 200 after recovery, and `/metrics` contains collection, rejection, duplicate, queue lag, Pending, DLQ, ClickHouse batch, and aggregation-delay metric names.

- [ ] **Step 2: Implement structured logging and health endpoints**

Generate or propagate `X-Request-ID`. Include request ID, batch ID, event ID, and project ID where available. Redact passwords, access tokens, refresh tokens, write keys, user IDs, anonymous IDs, and properties. Dependency readiness checks use two-second timeouts and do not mutate state.

- [ ] **Step 3: Add the complete backend integration flow**

`tracking_flow_test.go` must bootstrap `admin/admin`, change the password to `Admin-Changed-2026!`, create a project, create three category levels, publish `product_detail_view`, submit a batch twice with the same event IDs, wait for Worker and Scheduler, then assert Redis PV=2, Redis UV=1, ClickHouse PV=2, ClickHouse UV=1, and no duplicate detail rows under `FINAL`.

- [ ] **Step 4: Add Playwright acceptance tests**

Test first-password redirect, project creation, category tree, point wizard, publication immutability, Viewer read-only behavior, overview latency labels, statistics filters, and invalid-event details. Run against the Compose web/API environment.

- [ ] **Step 5: Add deterministic fault scripts**

`clickhouse_recovery.sh` stops ClickHouse, submits a known batch, verifies a Pending entry exists, restarts ClickHouse, waits up to 90 seconds, and verifies one logical event plus one PV increment. `dlq.sh` points Worker at an unreachable ClickHouse address, waits for five deliveries, and verifies the message appears in `tracking:{queue}:dead` with its original payload and failure stage.

- [ ] **Step 6: Add the k6 workload and operations documentation**

`tests/load/collect.js` uses batches of 50 events and ramps to 2000 events/s for 10 minutes. It records accepted rate, p95 HTTP duration, rejection rate, queue lag, and drain time. `docs/operations.md` records tested hardware, exact Compose configuration, expected healthy output, backup boundaries, write-key rotation, controlled DLQ replay, Redis capacity for 72-hour dedup markers, and ClickHouse TTL verification.

- [ ] **Step 7: Run the full verification matrix**

Run: `make test`  
Expected: all Go unit and frontend unit tests PASS.  
Run: `make test-integration`  
Expected: backend integration flow PASS.  
Run: `make test-e2e`  
Expected: Playwright suites PASS.  
Run: `make test-fault`  
Expected: ClickHouse recovery and DLQ scripts PASS.  
Run: `docker compose config`  
Expected: exit 0.  
Run: `git diff --check`  
Expected: no output.

- [ ] **Step 8: Commit the verified release candidate**

```bash
git add internal/observability internal/adapter/httpapi internal/cmd tests web/playwright.config.ts web/e2e docs/operations.md README.md docker-compose.yml Makefile
git commit -m "test: verify tracking platform end to end"
```

---

## Final Release Verification

- [ ] Run `go test ./...` and record the package count and duration in `docs/operations.md`.
- [ ] Run `npm --prefix web test -- --run`, `npm --prefix web run typecheck`, and `npm --prefix web run build`; record exact versions from `npm --prefix web exec -- vue --version` and `node --version`.
- [ ] Run the integration, E2E, and fault suites from empty Docker volumes.
- [ ] Run the k6 workload on the intended test host; record CPU, memory, API p95, accepted rate, peak queue lag, and drain time without claiming production capacity from a developer laptop.
- [ ] Confirm `docker compose logs` contains no plaintext password, token, write key, user ID, anonymous ID, or event properties.
- [ ] Confirm `tracking_event` and `tracking_invalid_event` show 30-day TTL definitions and aggregate tables show no TTL.
- [ ] Confirm a Viewer receives HTTP 403 for every management write endpoint even if the UI action is manually invoked.
- [ ] Confirm the worktree is clean and the commit history contains one reviewable commit per task.
