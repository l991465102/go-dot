# go-dot 业务埋点统计平台

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

go-dot 是基于 GoFrame v2 的埋点治理与 PV/UV 统计平台。它把“点位定义、客户端上报、异步消费、实时统计、历史聚合和数据诊断”放在一个可独立部署的仓库中。

```text
Web / App -> API -> Redis Stream -> Worker -> ClickHouse 明细
                         |              -> Redis 实时 PV/UV
                         +-> Scheduler -> ClickHouse 小时聚合、Pending 回收、DLQ
管理后台 -> API -> MySQL 点位定义与账号权限
Prometheus <- API :8080/metrics
           <- Worker :9091/metrics
           <- Scheduler :9091/metrics
```

## 运行要求

推荐直接使用 Docker Compose。项目固定的主要版本如下：

- Go 1.26.5、GoFrame 2.10.2
- Node.js 24 LTS
- MySQL 8.4、Redis 8、ClickHouse 26.3
- Vue 3.5、Vite 8、Playwright 1.61

本机开发还需 `make`、npm；执行压测需单独安装 k6。生产环境至少准备 32 字节随机 JWT 密钥和 visitor master key。

## Docker 一键启动

```bash
cp .env.example .env
# 必须同时替换 MySQL root 密码/DSN、JWT secret 和 visitor master key
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:8080/health/ready
```

访问地址：

- 管理后台：<http://127.0.0.1:5173>
- API：<http://127.0.0.1:8080>
- Prometheus：<http://127.0.0.1:9090>

首次登录账号为 `admin`，密码为 `admin`。系统强制首次登录立即修改密码；示例和集成测试使用的新密码是 `Admin-Changed-2026!`，生产环境请换成自己的强密码。

停止服务不会删除数据：

```bash
docker compose down
```

只有确认需要清空所有本地数据时才执行 `docker compose down -v`。

## 本机运行

先启动依赖并执行迁移：

```bash
docker compose up -d mysql redis clickhouse
export GO_DOT_MYSQL_DSN='root:<.env中的MySQL密码>@tcp(127.0.0.1:3306)/go_dot?parseTime=true'
export GO_DOT_REDIS_ADDRESS='127.0.0.1:6379'
export GO_DOT_CLICKHOUSE_DSN='clickhouse://default:@127.0.0.1:9000/go_dot'
export GO_DOT_JWT_SECRET='replace-with-at-least-32-random-bytes'
export GO_DOT_VISITOR_MASTER_KEY='replace-with-an-independent-random-key'
export GO_DOT_SECURE_COOKIES=false
make migrate
```

分别启动三个后端进程和前端：

```bash
make run-api
GO_DOT_METRICS_ADDRESS=:9091 make run-worker
GO_DOT_METRICS_ADDRESS=:9092 make run-scheduler
npm --prefix web install
npm --prefix web run dev
```

API、Worker、Scheduler 必须使用相同的数据库、Redis Stream 名称、consumer group 和密钥配置。API 不能代替 Worker 或 Scheduler。

升级已有部署时，先备份 MySQL 和 ClickHouse，再用与目标版本完全相同的配置执行迁移；`0002_custom_reports.sql` 会创建自定义报表配置表，MySQL `0003_property_analytics.sql` 与 ClickHouse `0007_property_analytics.sql` 会增加属性统计配置、明细和小时聚合表。迁移完成后需重启 API、Worker、Scheduler，并重新构建/发布 Web，不能只刷新浏览器静态文件：

```bash
make migrate
docker compose up -d --build api worker scheduler web
docker compose ps
curl -fsS http://127.0.0.1:8080/health/ready
```

## 从点位定义到上报

### 测试上报

历史版本创建的需求与验收数据表会保留，系统不再提供独立的需求管理功能；迁移 0008 解除历史任务对点位删除的外键限制。删除原点位后，历史任务中保留的点位 ID 可能无法关联到现有点位。

测试上报使用与正式上报相同的请求体和 write key，将地址改为 `/api/v1/collect/test-events`。该接口复用已发布点位的校验规则，同步记录脱敏的点位编码、平台、应用版本、校验状态和错误码；不会将测试事件发送到正式事件队列，因此不会进入正式实时统计、历史聚合或报表。点位详情可手动查询最近七天的测试记录。测试接口的 202 表示校验结果已记录，不表示生产链路已接收或入库。记录不包含上报的身份、会话或属性值；应用版本等元数据仍由调用方提供，不应填入敏感信息。七天前记录在后续测试写入时分批清理；没有新写入时物理删除可能延迟。

该测试通道只验证已发布定义及采集端校验，不模拟正式队列、Worker、ClickHouse 或生产报表的完整链路。升级时需执行 MySQL `0004` 至 `0008` 迁移并更新 API 和 Web。

管理后台中的标准流程是：

1. 登录并创建项目，安全保存页面仅展示一次的 write key。
2. 创建三级分类：`DOMAIN -> MODULE -> SCENE`。
3. 在 SCENE 下新增点位，填写触发时机、统计口径、平台和属性 Schema。
4. 发布点位。发布后的核心定义不可修改；不兼容变化应复制成新点位，旧点位只能废弃。
5. 前端/App 使用发布的 `point_code` 和项目 write key 上报。

write key 只用于采集，不能调用管理接口。轮换 write key 后旧 key 有 24 小时宽限期，明文新 key仍只展示一次。

### cURL 上报

事件 ID 必须是 UUIDv7 或 ULID；一次请求支持 1～100 条事件。

```bash
curl -i http://127.0.0.1:8080/api/v1/collect/events \
  -H 'Content-Type: application/json' \
  -H 'X-Write-Key: <项目创建或轮换时得到的write-key>' \
  -H 'X-Request-ID: order-page-0190' \
  --data '{
    "events": [{
      "event_id": "018f22a0-2c5a-7bb0-8c1a-4f15b2ad0001",
      "point_code": "product_detail_view",
      "event_time": 1784073600000,
      "anonymous_id": "browser-installation-id",
      "platform": "web",
      "channel": "official",
      "properties": {"product_id": "123"}
    }]
  }'
```

HTTP `202` 表示批次已经进入接收流程，不表示 ClickHouse 聚合已经可见。响应中的 `accepted`、`rejected` 和逐事件结果必须由客户端检查。

### JavaScript

```js
await fetch('/api/v1/collect/events', {
  method: 'POST',
  headers: {'Content-Type': 'application/json', 'X-Write-Key': writeKey},
  body: JSON.stringify({events: [{
    event_id: uuidv7(), // 使用已审核的 UUIDv7/ULID 库；不要使用 UUIDv4
    point_code: 'product_detail_view',
    event_time: Date.now(),
    anonymous_id: installationId,
    platform: 'web',
    properties: {product_id: '123'},
  }]}),
  keepalive: true,
})
```

### Go

```go
payload := strings.NewReader(`{"events":[{"event_id":"018f22a0-2c5a-7bb0-8c1a-4f15b2ad0001","point_code":"product_detail_view","event_time":1784073600000,"user_id":"10086","platform":"android","properties":{"product_id":"123"}}]}`)
req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/collect/events", payload)
req.Header.Set("Content-Type", "application/json")
req.Header.Set("X-Write-Key", writeKey)
res, err := http.DefaultClient.Do(req)
```

客户端应本地批量、指数退避重试，并在重试时保持相同 event ID。不要把 write key、用户身份或 properties 写入客户端日志。

Go 和 Web SDK、失败恢复方式以及从已发布点位生成类型的命令见 [SDK 接入文档](sdk/README.md)。

## 统计口径

- PV 是通过校验且首次完成实时计数的事件数。
- UV 使用 `user_id`，未登录时使用 `anonymous_id`，服务端只保存项目隔离的不可逆哈希。
- `EXACT` 使用集合/精确聚合；`APPROXIMATE` 使用 HyperLogLog/近似聚合。
- 小时 UV、日 UV不能直接相加。查询必须在目标周期重新合并去重状态。
- Redis 提供当前窗口实时结果；ClickHouse 提供明细和已提交的历史聚合。页面会明确标记实时/聚合延迟与精确/近似 UV。
- 客户端时间超出允许窗口或点位选择 `SERVER` 时，以服务端时间统计。

## 自定义报表

### 转化漏斗

统计分析页面支持 2～5 个不同点位的顺序漏斗。首步发生时间必须在指定的半开区间 `[start, end)`；后续事件须在某次首步后的转化窗口内严格按毫秒时间顺序发生，同一毫秒不视为先后。每位访客每一步最多计数一次；重复上报按项目和事件 ID 合并。平台和渠道筛选作用于所有步骤，转化率以相邻上一步人数为分母。

为降低明细过期造成的误读，首步区间限制为最近 21 天内、最长 7 天，转化窗口为 1～168 小时。明细默认按接收时间保留约 30 天，客户端时间校正、队列延迟、数据补发和 TTL 实际执行时刻仍会影响结果；窗口未结束的结果标记为未完整。漏斗按现有项目内访客哈希精确计数：登录前匿名 ID 与登录后用户 ID 不会自动合并，跨身份的行为不能组成同一条漏斗。测试上报不参与漏斗。

统计分析页面还支持选择 2～5 个点位对比 PV 或 UV 趋势，沿用页面的时间范围、粒度、分类、平台和渠道条件。每个点位独立查询并标明 UV 的精确或近似口径；没有返回数据的时间桶显示为空，不视为零。

趋势对比和漏斗均可保存为项目图表。管理员可保存或删除，项目成员可加载；加载后需要重新查询。保存的是固定时间范围和筛选条件，不保存查询结果；漏斗时间超出最近 21 天时需调整后查询。每种图表列出最近 200 条。

进入管理后台后，从左侧项目导航选择“自定义报表”。未选择“已保存报表”时是临时报表：日期、点位、名称等编辑仅存在于当前页面，不会覆盖已保存配置；Admin 使用“另存为”创建命名报表，加载已有报表后再用“保存配置”显式更新。Viewer 可以加载、查询和导出，但创建、更新、删除均由后端返回 HTTP 403。

- 新建临时报表默认最近 30 个上海自然日；一次查询/保存最多 90 个自然日、100 个点位，至少选择 1 个已发布或已废弃点位。
- 日期边界固定为 `Asia/Shanghai`。当天数据标为“未完整”，Scheduler 尚未提交的新小时可能暂未显示；不要把它解读为最终日结值。
- 事件、聚合和 API 中的绝对时间仍以 UTC 存储/传输；后台和 Excel 的绝对时间统一显示为 `Asia/Shanghai` 的 `YYYY-MM-DD HH:mm:ss`。
- 点位属性 Schema 中，`INTEGER`、`DECIMAL` 可开启“数值指标”并配置 `COUNT/SUM/AVG/MIN/MAX`；`STRING`、`ENUM`、`BOOLEAN` 可开启“分类维度”，固定查看事件数、UV 和占比；`DATETIME`、`ARRAY` 第一期不支持属性统计。
- 报表只查询、显示和导出当前勾选的属性及聚合方式。一次最多选择 20 个统计属性，其中分类维度最多 5 个；字符串维度每天显示前 50 个值，其余合并为“其他”。
- 导出的 XLSX 始终包含 `点位统计`、`报表说明`；选择数值属性时增加 `数值属性`，每个分类属性增加一个 `分类-...` 工作表。日期按降序输出，无数据的 PV/UV 与 COUNT/SUM 为 0，AVG/MIN/MAX 无有效值时留空；用户文本会阻止以 `= + - @` 开头的内容成为 Excel 公式。
- 属性小时聚合默认保留 400 天，可通过 `GO_DOT_PROPERTY_AGGREGATE_RETENTION_DAYS` 调整（最少 90 天）；Scheduler 启动时会同步 ClickHouse TTL。

先登录取得 access token，再可直接调用查询接口：

```bash
export GO_DOT_ACCESS_TOKEN='<登录响应中的access_token>'
curl -fsS http://127.0.0.1:8080/api/v1/statistics/daily-by-point/query \
  -H "Authorization: Bearer $GO_DOT_ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{
    "project_id": 1,
    "start_date": "2026-07-01",
    "end_date": "2026-07-30",
    "point_ids": [101, 102],
    "property_queries": [
      {
        "point_id": 101,
        "property_code": "amount",
        "aggregations": ["COUNT", "SUM", "AVG"]
      },
      {
        "point_id": 102,
        "property_code": "phone_type"
      }
    ]
  }'
```

导出使用相同查询条件，并补充报表名称和说明；服务端返回 XLSX 下载：

```bash
curl -fS http://127.0.0.1:8080/api/v1/statistics/daily-by-point/export \
  -H "Authorization: Bearer $GO_DOT_ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{
    "project_id": 1,
    "start_date": "2026-07-01",
    "end_date": "2026-07-30",
    "point_ids": [101, 102],
    "property_queries": [
      {
        "point_id": 101,
        "property_code": "amount",
        "aggregations": ["SUM", "AVG"]
      }
    ],
    "report_name": "月度经营报表",
    "report_description": "精确与近似 UV 点位合并展示"
  }' \
  --output custom-report.xlsx
```

## 账号与权限

- 系统账号需要先登录获取 access token；refresh token 只保存在 HttpOnly Cookie。
- 项目 `ADMIN` 可以管理分类、点位、成员、项目设置和 write key。
- 项目 `VIEWER` 只能读取。后端会对管理写接口返回 HTTP 403，不能依赖前端隐藏按钮实现权限。
- 发布点位不可直接编辑或物理删除，确保历史数据可解释。

生产 HTTPS 部署必须设置 `GO_DOT_SECURE_COOKIES=true`。JWT secret、visitor master key、数据库密码和 write key 应由 Secret Manager 注入，不能提交到仓库、写入镜像或输出到日志。两个 master key 必须相互独立。visitor master key 是统计口径的一部分，应在备份和灾备中永久保存：常规发布不得轮换；丢失或紧急轮换会让轮换后的访客哈希与历史 UV 无法连续去重，必须记录切换时刻并在报表中标注口径断点。紧急轮换 JWT secret 会立即使现有登录令牌失效，需要所有账号重新登录。项目 write key 应通过项目设置的轮换接口更换，旧 key 仅保留 24 小时宽限，不要直接修改数据库哈希。

## 健康检查与指标

```bash
curl -fsS http://127.0.0.1:8080/health/live   # 只证明进程存活
curl -fsS http://127.0.0.1:8080/health/ready  # 2 秒内只读检查 MySQL/Redis/ClickHouse
curl -fsS http://127.0.0.1:8080/metrics
```

不要把 `/health/live` 当作流量准入条件；依赖故障时它仍返回 200，负载均衡应使用 `/health/ready`。所有 HTTP 响应都会传播合法 `X-Request-ID` 或生成新值。结构化日志会屏蔽密码、token、write key、user/anonymous ID 和 properties。

## 测试

不依赖 Docker 的验证：

```bash
make test
go test -race ./...
npm --prefix web run typecheck
npm --prefix web run build
docker compose config
```

完整验收使用独立 Compose project，从全新 volumes 开始，不删除已有本地数据：

```bash
cp deploy/acceptance.env.example .env.acceptance
# 若固定验收端口被占用，先检查占用者并避免影响已有服务。
export GO_DOT_ACCEPTANCE_PROJECT="go-dot-acceptance-$(date +%Y%m%d%H%M%S)-$$"
acceptance_compose=(docker compose --env-file .env.acceptance -p "$GO_DOT_ACCEPTANCE_PROJECT" -f docker-compose.yml -f deploy/docker-compose.acceptance.yml)
"${acceptance_compose[@]}" up -d --build
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
# 若不继续执行 fault/load 验收，此时清理本次唯一 project。
"${acceptance_compose[@]}" down -v --remove-orphans
```

时间戳加 PID 的 Compose project 名确保每次得到新的 project-scoped volumes；结束命令只删除本次唯一 project。第一次 `test-e2e-bootstrap` 在全新栈中创建 Viewer 账号，Integration 再通过公开成员 API 把它加入新项目并验证读/导出与 POST/PATCH/DELETE 403。后面的 `GO_DOT_E2E_BOOTSTRAP=1` 会在 Admin 改密后幂等刷新浏览器账号密码。验收配置把 `GO_DOT_SCHEDULER_AGGREGATE_LOOKBACK_HOURS` 设为 26，使 Scheduler 通过正常 generation 提交流程刷新最近 26 个小时；多出的一小时余量保证验收跨过 UTC 小时边界后，前一上海自然日同时段的数据仍在窗口内。Integration 会在相邻两个上海自然日真实上报数据并验证两日均有值。生产默认仍为 2，可配置范围为 1～168；扩大窗口会线性增加每轮聚合开销，只应在补算窗口明确时使用。Integration 还会把故障测试所需的一次性 fixture 写入权限为 `0600` 的临时文件。按 [docs/operations.md](docs/operations.md) source 该文件后运行 `make test-fault`。压测还需设置 `GO_DOT_LOAD_WRITE_KEY` 和可访问的 Scheduler `/metrics` 地址后执行 `make load-test`。

## 生产部署

1. 使用外部持久化 MySQL、Redis 8 Cluster 和 ClickHouse 26.3；先备份再运行 `go-dot migrate`。
2. API、Worker、Scheduler 使用同一版本镜像，先迁移，再滚动 API/Worker，最后 Scheduler。
3. API 可水平扩展；Worker 通过 Redis consumer group 扩展；Scheduler 通过 Redis lease 保证同一时间桶单实例执行。
4. 配置 TLS、`SecureCookies=true`、Origin 白名单、独立密钥、Redis/ClickHouse 监控和备份。
5. 部署后检查 ready、metrics、队列 Pending/DLQ、ClickHouse TTL 和管理写权限。

### 队列升级注意事项

默认 Stream 为 `tracking:{queue}:events`、`tracking:{queue}:invalid`、`tracking:{queue}:dead`。花括号中的 Redis hash tag 用于保证多键 Lua 在 Redis Cluster 同一 slot。旧版本若使用无 tag 或不同 tag 的队列名，不能直接改配置：先停止写入、排空旧 Stream/Pending/延迟队列，确认 DLQ 已归档，再切换所有进程；否则新版本会启动失败或遗留消息不可见。队列端口由 `ports.MessageQueue` 抽象，升级到 Kafka 等中间件时应保持 Publish/Consume/Ack/Retry/DLQ 合约，并制定迁移双写/排空方案。

## 常见问题

- `GO_DOT_* is required`：三个进程缺少 DSN 或密钥；检查 `.env` 是否被 Compose 加载。
- ready 返回 503：查看响应中的依赖名以及 `docker compose ps`，不要重启并清空 volumes。
- 登录后一直要求改密码：必须用当前密码完成 `/api/v1/auth/change-password`。
- 上报 401：write key 无效、已过宽限期或项目停用。管理 access token 不能代替 write key。
- 上报 202 但统计暂未出现：检查 Worker、Redis Pending/延迟重试和 Scheduler 聚合延迟。
- `POINT_NOT_PUBLISHED`/属性错误：客户端点位编码或属性 Schema 与已发布快照不一致。
- Redis Cluster 报 CROSSSLOT：三个 Stream override 未使用同一个非空 hash tag。
- Cookie 在 HTTP 本地环境不工作：本地设置 `GO_DOT_SECURE_COOKIES=false`；生产 HTTPS 必须为 true。

## 当前验证边界

单元、静态配置、前端类型和构建可在开发机直接验证。集成、浏览器 E2E、ClickHouse 恢复、DLQ 与 2,000 events/s k6 结果只在实际执行并记录后才算通过；本仓库不把脚本存在等同于生产容量证明。当前机器/执行记录见 operations 文档。

## 开源许可

本项目基于 [MIT License](LICENSE) 开源。你可以使用、复制、修改、合并、发布和分发本项目，但需要保留原始版权与许可声明。
