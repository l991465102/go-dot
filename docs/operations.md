# go-dot 运维与发布手册

## 1. 当前验证记录

记录日期：2026-07-16。

当前开发主机为 macOS Darwin 24.1.0 arm64；Go 为 1.26.5。主机当前默认 Node.js 是 23.11.0，不符合项目要求的 Node 24，因此前端发布验证应在 Compose 的 `node:24-alpine` 构建阶段或切换 Node 24 后执行。Docker Client/Server 均为 27.3.1。当前未安装 k6。

本文不预填虚假的性能数据。下表是本次候选版本的实际验收记录：

| 项目 | 实测值 |
|---|---|
| 测试主机 CPU / 内存 | 开发主机，未作为容量测试机记录 |
| `go test ./...` 包数量 / 耗时 | 25 个包通过；最终 `make test` 全栈 7.66 秒（Go build cache 已预热） |
| Vue / Node 精确版本 | Vue 3.5.39；Compose 构建 Node 24；Vite 8.1.4 |
| API p95 / accepted rate | 未执行 k6，不代表容量 |
| 峰值 queue lag / drain time | 未执行 k6 |
| Integration / E2E / fault | 全新隔离 volumes：Integration 6.448 秒通过；Playwright 7/7 通过；精确 ClickHouse 恢复和 DLQ 两项通过 |

Compose 固定使用 MySQL 8.4、Redis 8、ClickHouse 26.3、Go 1.26.5、Node 24，并启用 API、Web 和 Prometheus 健康检查。`docker compose config` 是配置发布门禁。

本次还通过了 `go vet ./...`、全包 `go test -race`、前端 typecheck/build，以及官方 npm registry 的生产依赖审计（0 vulnerabilities）。隔离环境实测 live/ready 正常，原始表 30 天 TTL、聚合表无 TTL，日志 canary 无命中。上述记录证明功能验收通过，不等价于 2,000 events/s 的容量证明。
Prometheus 的三个进程目标 `api:8080`、`worker:9091`、`scheduler:9091` 已在隔离环境实测均为 `up`。实测停 Worker 后新增一批消息时 `lag=1,pending=0`，恢复并消费后均归零。

## 2. 标准发布与健康输出

```bash
docker compose pull
docker compose build --pull
docker compose up -d mysql redis clickhouse
docker compose run --rm migrate
docker compose up -d api worker scheduler web prometheus
docker compose ps
curl -sS http://127.0.0.1:8080/health/live
curl -sS http://127.0.0.1:8080/health/ready
```

健康输出分别应为 `{"status":"ok"}` 和 `{"status":"ready"}`。ready 会在一个总计 2 秒的 context 内只读 Ping MySQL、Redis、ClickHouse；任何必需依赖失败返回 503 和失败依赖名称。live 不探测依赖。

## 3. 完整发布验收

使用唯一 Compose project 和验收端口启动一套全新的环境，避免删除开发者现有 volumes：

```bash
cp deploy/acceptance.env.example .env.acceptance
# 释放固定验收端口；不带 -v，因此旧 go-dot-acceptance volumes 会保留。
docker compose --env-file .env.acceptance -p go-dot-acceptance -f docker-compose.yml -f deploy/docker-compose.acceptance.yml down
export GO_DOT_ACCEPTANCE_PROJECT="go-dot-acceptance-$(date +%Y%m%d%H%M%S)-$$"
acceptance_compose=(docker compose --env-file .env.acceptance -p "$GO_DOT_ACCEPTANCE_PROJECT" -f docker-compose.yml -f deploy/docker-compose.acceptance.yml)
"${acceptance_compose[@]}" up -d --build
"${acceptance_compose[@]}" ps
GO_DOT_E2E_COMPOSE_PROJECT="$GO_DOT_ACCEPTANCE_PROJECT" GO_DOT_E2E_ENV_FILE=.env.acceptance make test-e2e-bootstrap

GO_DOT_INTEGRATION_BASE_URL=http://127.0.0.1:18080 \
GO_DOT_REDIS_TEST_ADDRESS=127.0.0.1:16379 \
GO_DOT_CLICKHOUSE_TEST_DSN='clickhouse://default:@127.0.0.1:19001/go_dot' \
GO_DOT_INTEGRATION_FIXTURE_FILE=/tmp/go-dot-acceptance-fixture.env \
GO_DOT_INTEGRATION_VIEWER_USERNAME=e2e_viewer \
GO_DOT_INTEGRATION_VIEWER_PASSWORD=admin \
make test-integration

GO_DOT_E2E_BOOTSTRAP=1 \
GO_DOT_E2E_COMPOSE_PROJECT="$GO_DOT_ACCEPTANCE_PROJECT" \
GO_DOT_E2E_ENV_FILE=.env.acceptance \
GO_DOT_E2E_BASE_URL=http://127.0.0.1:15173 \
GO_DOT_E2E_WORKERS=1 \
GO_DOT_E2E_ADMIN_PASSWORD='Admin-Changed-2026!' \
GO_DOT_E2E_FIRST_PASSWORD='Admin-Changed-2026!' \
GO_DOT_E2E_FIRST_NEW_PASSWORD='First-Changed-2026!' \
GO_DOT_E2E_VIEWER_PASSWORD='Admin-Changed-2026!' \
make test-e2e
```

故障测试完成后用 `"${acceptance_compose[@]}" down -v --remove-orphans` 删除本次唯一 project 的容器和 volumes；不要对默认项目或被保留的旧 `go-dot-acceptance` 执行 `down -v`。

Integration 环境变量未配置时测试会跳过；一旦配置，依赖和两个 Viewer fixture 变量不可用必须失败。第一次 bootstrap 在全新栈中创建 `e2e_viewer`；测试再从 `admin/admin` 启动，改密，创建项目和三级分类，通过公开 API 给 Viewer 添加成员关系，发布 EXACT/APPROXIMATE 两种点位，并重复提交同一批十个跨两个上海自然日的事件。每次提交后都等待 Redis consumer group 的 lag 和 pending 同时归零。它会等待 Scheduler generation，验证两日各自 exact PV=2/UV=1、approximate PV=3/UV=2，随后从已保存报表重新读取点位顺序，查询两日有值矩阵，导出并重新打开 XLSX，最后验证 Viewer 可读/导出且 POST/PATCH/DELETE 均返回 403。超时时会输出最后一组 raw/realtime/generation/aggregate 诊断。

Scheduler 默认刷新当前和前一个小时。`GO_DOT_SCHEDULER_AGGREGATE_LOOKBACK_HOURS` 可在 1～168 之间调整；验收环境设为 26，因此前一上海日同一时刻的原始事件在跨过 UTC 小时边界后仍由正常 Scheduler generation 路径聚合。报表完整性以每次查询响应时捕获的上海当前日判定：当日不完整，其他日期完整，因此跨上海零点后两个历史日都可正确验收。扩大窗口会线性增加 ClickHouse 写入和 Scheduler 每轮耗时，生产补算结束后应恢复默认 2。

`GO_DOT_E2E_BOOTSTRAP=1` 会再次运行已提交的幂等 `tests/e2e/bootstrap.sh`，用改密后的当前 admin 密码 hash 刷新专用 workflow admin、首次改密账号和 Viewer，不需要手工 SQL。浏览器会通过 UI 创建项目、Domain/Module/Scene、草稿和发布点位，验证发布后不可编辑；随后 Viewer 直接调用项目设置、key、成员、分类和点位管理写接口并逐一断言 403。Admin 还会查询、保存并下载双点位自定义报表，诊断页会验证上海格式时间。当前套件 8 项全部通过才算完整 E2E。

## 4. 故障恢复

Integration 已创建并发布 `product_detail_view`，同时生成临时 fixture：

```bash
set -a
source /tmp/go-dot-acceptance-fixture.env
set +a
export GO_DOT_FAULT_API_URL=http://127.0.0.1:18080
export GO_DOT_FAULT_COMPOSE_PROJECT="$GO_DOT_ACCEPTANCE_PROJECT"
export GO_DOT_FAULT_COMPOSE_OVERRIDE=deploy/docker-compose.acceptance.yml
export GO_DOT_FAULT_ENV_FILE=.env.acceptance
make test-fault
```

`clickhouse_recovery.sh` 会停 ClickHouse、提交带唯一 batch/event 的事件，通过该二元组定位其确切 Stream message 并确认该 message 进入 Pending，再恢复 ClickHouse，并在 90 秒内确认 `FINAL` 只有一条逻辑明细且 PV 只增加一次。`dlq.sh` 同样停 ClickHouse，定位唯一 DLQ entry，严格确认 `deliveries=5`、batch、原始 event/point payload 和 ClickHouse write failure stage，退出时恢复 ClickHouse/Worker/Scheduler。

不要在生产直接运行这些脚本。

## 5. DLQ 受控重放

平台 UI 刻意不提供一键重放。生产重放流程：

1. 导出目标 DLQ 条目和审计单，确认失败根因已修复。
2. 按 `batch_id`、项目和 failure stage 限定范围，检查原始 payload 未被篡改。
3. 使用专门审批过的运维程序，以新的重放批次 token 发布到原 source Stream；不要直接复制 Redis 字段或删除 DLQ。
4. 每次小批量重放，观察 duplicate、ClickHouse failure、Pending、queue lag。
5. `FINAL` 明细和实时 completion marker 都确认后，再归档原 DLQ；保留操作人、时间和条目 ID。

## 6. Write key 轮换

管理员在项目设置中轮换。新 key 明文只展示一次，必须立即写入客户端 Secret 配置；旧 key 在 24 小时宽限期内有效。先发布使用新 key 的客户端，确认采集量正常，再等待旧 key 自动过期。不得在日志、工单或聊天中粘贴 key。

## 7. 备份边界

- MySQL：项目、成员、分类、点位、不可变发布快照、审计记录和 key hash，是配置治理的恢复源；至少每日全量加 binlog/PITR。
- ClickHouse：30 天原始有效/无效事件与无 TTL 聚合表。原始表可按保留策略恢复，聚合表需备份或从保留期内明细重算。
- Redis：Stream、Pending/延迟重试、DLQ、72 小时幂等 marker 和实时计数。启用 AOF/副本，但不能把 Redis 备份当作 ClickHouse/MySQL 备份。
- Secret：JWT/visitor master key 和外部数据库凭据由独立 Secret Manager 备份，不能进入数据库 dump 或仓库。

恢复顺序：MySQL/ClickHouse -> Redis -> migrate -> API -> Worker -> Scheduler -> Web。恢复后先保持采集限流，检查重复 marker 的时间边界，再逐步放量。

## 8. Redis 容量与 72 小时幂等

每个事件至少产生一个 completion marker，发布批次另有 dedup marker，并伴随小时/日 PV 与 UV 结构。容量估算必须用目标事件率实测：

```text
marker 数约等于 72 小时事件总数
marker memory = MEMORY USAGE 抽样均值 × 72 小时事件数 × 安全系数 1.5
再加 Stream/Pending/重试、Set/HLL、复制/AOF 和碎片空间
```

在预发导入代表性事件后抽样：

```bash
redis-cli --scan --pattern 'rt:event:*' | head -100 | xargs -n1 redis-cli MEMORY USAGE
redis-cli INFO memory
redis-cli INFO persistence
```

不得将 `GO_DOT_QUEUE_PUBLISH_DEDUP_TTL` 配低于 72h；允许范围为 72h～168h。精确 UV Set 的内存随访客数增长，20 万 DAU 仍需按点位/平台/渠道的实际维度数压测。

## 9. ClickHouse TTL 核验

原始有效和无效表必须为 30 天 TTL；聚合表不能带 TTL：

```sql
SELECT database, table, engine, create_table_query
FROM system.tables
WHERE database = 'go_dot'
  AND table IN ('tracking_event','tracking_invalid_event','business_stat_hourly','business_stat_exact_hourly');
```

确认 `tracking_event`、`tracking_invalid_event` 包含 `TTL received_at + INTERVAL 30 DAY DELETE`，两张 aggregate 表的 `create_table_query` 不含 `TTL`。升级 ClickHouse 后重复执行。

## 10. 指标与告警

API、Worker、Scheduler 各有独立进程内注册表；Prometheus 分别抓取 `api:8080/metrics`、`worker:9091/metrics` 和 `scheduler:9091/metrics`。可用 `GO_DOT_METRICS_ADDRESS` 修改 Worker/Scheduler 监听地址。主要指标：

- `go_dot_collection_*_total`：接收、接受、拒绝、重复、失败。
- `go_dot_queue_lag` 来自 `XINFO GROUPS lag`，表示尚未投递给 consumer group 的消息；`go_dot_queue_pending` 来自 `XPENDING`，表示已投递但未 ACK 的消息。另有 `go_dot_queue_dlq`、`go_dot_queue_oldest_pending_age_seconds`。
- `go_dot_worker_consume_duration_seconds`、`go_dot_worker_retries_total`。
- `go_dot_redis_operation_duration_seconds`。
- `go_dot_clickhouse_batch_size`、`go_dot_clickhouse_batch_duration_seconds`、`go_dot_clickhouse_failures_total`。
- `go_dot_realtime_statistics_delay_seconds`；`go_dot_aggregation_delay_seconds` 表示距离最近一次成功小时聚合刷新的秒数，首次成功前为 -1，成功后随时间增长而不是固定为零。

建议对 ready 持续失败、DLQ 增长、Pending 最老年龄增长、拒绝率突增、ClickHouse failure、聚合延迟和采集量骤降告警。指标不得使用 user/event/project ID 等高基数 label。

## 11. k6 压测

`tests/load/collect.js` 每批 50 条，升至 40 request/s，即 2,000 events/s，稳态 10 分钟。只能在隔离测试环境执行：

```bash
GO_DOT_LOAD_BASE_URL=http://test-api:8080 \
GO_DOT_LOAD_WRITE_KEY='<test-write-key>' \
GO_DOT_LOAD_POINT_CODE=product_detail_view \
GO_DOT_LOAD_QUEUE_METRICS_URL=http://127.0.0.1:19092/metrics \
k6 run tests/load/collect.js
```

脚本严格解析 Prometheus 文本中的 `go_dot_queue_lag` 和 `go_dot_queue_pending`；URL 缺失、HTTP 非 200、指标缺失或值非法都会让测试失败。teardown 等待 lag 与 Pending 同时归零并记录 drain time。记录测试机 CPU/内存、accepted rate、HTTP p95、拒绝率、峰值 queue lag 和 drain time。开发笔记本结果不能声明为生产容量。

## 12. 日志泄露检查

候选发布后执行：

```bash
docker compose logs --no-color > /tmp/go-dot-compose.log
# 使用专门生成的 canary 密码/token/key/user/anonymous/property 值逐项搜索
grep -F '<canary-value>' /tmp/go-dot-compose.log
```

必须无命中。不要把真实 secret 放进 shell history。日志允许 request ID、batch ID、event ID、project ID，但禁止 password、access/refresh token、write key、user ID、anonymous ID 和 properties。

## 13. Redis 队列中间件升级

`{queue}` 是 Redis Cluster hash tag。任何 Stream override 都必须带相同 tag。由旧的无 tag 队列迁移时：冻结采集、排空 Pending 和延迟集合、归档 DLQ、停止所有旧进程、运行验证工具确认旧队列为空，再一次性切换 API/Worker/Scheduler。迁移到 Kafka 等实现必须通过 MessageQueue 合约测试，并明确 ACK、重试、顺序、幂等和 DLQ 的语义差异。
