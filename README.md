# go-dot 业务埋点统计平台

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
# 必须先替换 .env 中两个 local-*-change-before-production 密钥
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
export GO_DOT_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/go_dot?parseTime=true'
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
make run-worker
make run-scheduler
npm --prefix web install
npm --prefix web run dev
```

API、Worker、Scheduler 必须使用相同的数据库、Redis Stream 名称、consumer group 和密钥配置。API 不能代替 Worker 或 Scheduler。

## 从点位定义到上报

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

## 统计口径

- PV 是通过校验且首次完成实时计数的事件数。
- UV 使用 `user_id`，未登录时使用 `anonymous_id`，服务端只保存项目隔离的不可逆哈希。
- `EXACT` 使用集合/精确聚合；`APPROXIMATE` 使用 HyperLogLog/近似聚合。
- 小时 UV、日 UV不能直接相加。查询必须在目标周期重新合并去重状态。
- Redis 提供当前窗口实时结果；ClickHouse 提供明细和已提交的历史聚合。页面会明确标记实时/聚合延迟与精确/近似 UV。
- 客户端时间超出允许窗口或点位选择 `SERVER` 时，以服务端时间统计。

## 账号与权限

- 系统账号需要先登录获取 access token；refresh token 只保存在 HttpOnly Cookie。
- 项目 `ADMIN` 可以管理分类、点位、成员、项目设置和 write key。
- 项目 `VIEWER` 只能读取。后端会对管理写接口返回 HTTP 403，不能依赖前端隐藏按钮实现权限。
- 发布点位不可直接编辑或物理删除，确保历史数据可解释。

生产 HTTPS 部署必须设置 `GO_DOT_SECURE_COOKIES=true`。JWT secret、visitor master key、数据库密码和 write key 应由 Secret Manager 注入，不能提交到仓库、写入镜像或输出到日志。两个 master key 必须相互独立；丢失 visitor key 会改变后续 UV 身份哈希口径。

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
docker compose -p go-dot-acceptance -f docker-compose.yml -f deploy/docker-compose.acceptance.yml up -d --build
GO_DOT_INTEGRATION_BASE_URL=http://127.0.0.1:18080 \
GO_DOT_REDIS_TEST_ADDRESS=127.0.0.1:16379 \
GO_DOT_CLICKHOUSE_TEST_DSN='clickhouse://default:@127.0.0.1:19001/go_dot' \
make test-integration
GO_DOT_E2E_BASE_URL=http://127.0.0.1:15173 make test-e2e
```

故障测试需要预先创建并发布 `product_detail_view`，然后设置 `GO_DOT_FAULT_WRITE_KEY`、`GO_DOT_FAULT_PROJECT_ID` 和 `GO_DOT_FAULT_POINT_ID` 后运行 `make test-fault`。压测还需设置 `GO_DOT_LOAD_WRITE_KEY` 后执行 `make load-test`。详细步骤与记录模板见 [docs/operations.md](docs/operations.md)。

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
