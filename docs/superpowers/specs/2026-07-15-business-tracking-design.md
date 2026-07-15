# Go Dot 业务埋点与 PV/UV 统计系统设计

日期：2026-07-15
状态：已确认，待实施计划
技术栈：GoFrame v2、Vue 3、TypeScript、Element Plus、MySQL、Redis、ClickHouse

## 1. 背景与目标

Go Dot 是面向产品、Web、App、测试和数据人员的业务埋点协作与统计系统。产品和开发人员先在管理端定义业务分类、点位、触发规则和属性 Schema，客户端只能按照已发布定义上报。系统接收事件后异步统计 PV 和 UV，并提供实时概览、历史趋势、无效事件和死信诊断。

一期目标：

1. 支持多个项目或应用的数据隔离。
2. 支持三级业务分类和不可变的已发布点位定义。
3. 支持 Web、iOS、Android 和小程序批量上报。
4. 实时 PV/UV 在 5 秒内可见，ClickHouse 明细和历史趋势在 1 分钟内可见。
5. 以约 20 万日活、1000 万事件/天、峰值 2000 events/s 作为设计验证目标。
6. 原始事件保留 30 天，聚合结果长期保留。
7. 一期使用 Redis Stream，业务代码通过消息队列端口隔离，后续可增加 Kafka 等适配器。
8. 通过 Docker Compose 一键启动和验收全部组件。

## 2. 一期范围

### 2.1 包含

- 本地账号密码登录、首次改密、项目级管理员和只读角色。
- 项目、成员、写入 Key、来源白名单和统计时区管理。
- 三级业务分类树。
- 点位草稿、新增、编辑、发布、复制和废弃。
- 点位属性 Schema、触发规则、统计规则、协作状态和接入示例。
- 批量上报、同步基础校验、异步消费、事件幂等、重试和死信。
- Redis 实时 PV/UV。
- ClickHouse 原始事件、无效事件和长期可合并聚合。
- 项目概览、点位工作台、统计分析和数据诊断页面。
- 健康检查、结构化日志、Prometheus 指标、自动化测试和 k6 压测脚本。

### 2.2 不包含

- 登录前和登录后的跨设备身份合并。
- 漏斗、留存、路径、分群和实验分析。
- 任意自定义属性的实时聚合。
- 已发布点位的兼容性在线编辑。
- 多机 ClickHouse 集群和 Kafka 实现；一期只预留端口与配置能力。
- 自助注册、企业 SSO 和复杂组织级 RBAC。

## 3. 架构

采用模块化单体、单仓库、多进程运行：

```text
Web / App SDK
      |
      | POST /api/v1/collect/events
      v
API 进程 ---- MySQL 元数据
      |
      | MessageQueue.PublishBatch
      v
Redis Stream ---- Worker 进程 ---- ClickHouse 原始事件与聚合
                       |
                       +---- Redis 实时 PV/UV

Vue 管理端 ---- API 进程 ---- MySQL / Redis / ClickHouse

Scheduler 进程 ---- Pending 回收、死信、Key 过期、聚合校验和告警
```

运行命令：

```text
go-dot api
go-dot worker
go-dot scheduler
```

代码内部定义以下端口：

- `MessageQueue`：`PublishBatch`、`Consume`、`Ack`、`Retry`、`MoveToDLQ`、`ReclaimStale`。
- `EventStore`：批量保存有效事件、无效事件以及查询明细。
- `RealtimeCounter`：幂等更新实时 PV/UV 和读取实时结果。
- `MetadataRepository`：账号、项目、分类、点位和审计记录。

Redis Stream、ClickHouse、Redis 计数和 MySQL 是端口适配器。未来增加 Kafka 时，不修改上报、消费、点位校验和统计领域逻辑。

## 4. 领域模型

### 4.1 项目和成员

`Project` 是数据隔离边界，拥有：

- 项目名称和项目编码。
- 当前写入 Key、上一个写入 Key及其失效时间。
- 允许的 Web Origin 列表。
- 统计时区，默认 `Asia/Shanghai`。
- 用于访客标识 HMAC 的项目级盐。

账号通过 `ProjectMember` 访问一个或多个项目。角色按项目分配：

- `ADMIN`：管理项目设置、成员、分类、点位、统计和诊断。
- `VIEWER`：只读查看项目概览、点位、接入示例、统计和诊断。

### 4.2 业务分类

分类固定为三级：

1. 业务域 `DOMAIN`。
2. 业务模块 `MODULE`。
3. 业务场景 `SCENE`。

点位只能挂在第三级叶子分类。分类编码在项目内唯一。分类被已发布点位引用后，编码、层级和父级不可修改；名称、描述和排序可以修改。停用分类后不能创建或发布新点位，但历史点位和数据仍可查询。

### 4.3 点位

点位包含：

- 项目、三级分类和项目内唯一编码。
- 中文名称、描述和行为类型。
- 触发页面、触发时机、触发次数和异常触发规则。
- 是否统计 PV、是否统计 UV、UV 模式和统计时间口径。
- 支持平台、渠道、产品负责人、开发负责人和需求链接。
- 属性 Schema。

点位编码格式为 `^[a-z][a-z0-9_]{2,127}$`，推荐使用“模块_场景_动作”。行为类型为：

- `PAGE_VIEW`
- `ELEMENT_EXPOSE`
- `ELEMENT_CLICK`
- `BUSINESS_ACTION`
- `BUSINESS_RESULT`
- `CUSTOM`

### 4.4 点位定义状态

定义状态不可逆：

```text
DRAFT -> PUBLISHED -> DEPRECATED
```

- `DRAFT`：可编辑、删除和发布。
- `PUBLISHED`：生成不可变完整快照，不能编辑或删除，只能复制新增或废弃。
- `DEPRECATED`：保留历史定义，记录废弃原因、时间和可选替代点位。

不兼容变化必须复制为新点位，包括改变触发时机、统计含义、已有属性类型、必填要求或 UV 口径。点位编码发布后永远不能被其他点位复用。

协作状态与定义状态分离，可独立流转：

```text
PENDING -> DEVELOPING -> TESTING -> ONLINE
```

已发布点位可以同时处于“测试中”。协作状态变化不改变上报契约。

### 4.5 属性 Schema

属性定义包含编码、中文名称、数据类型、是否必填、枚举值、示例、说明和排序。支持类型：

- `string`
- `integer`
- `decimal`
- `boolean`
- `datetime`
- `enum`
- `array`

一期对未知属性采用严格模式：出现未定义属性时该事件校验失败。自定义属性只保存到 30 天原始明细，不进入实时聚合。

## 5. 账号登录与安全

系统不开放注册。首次启动幂等创建初始管理员：

```text
用户名：admin
初始密码：admin
must_change_password：true
```

密码使用 Argon2id 哈希。首次登录后，改密前只允许访问当前账号、退出和修改密码接口。会话使用短期 Access Token 和 HttpOnly Refresh Cookie。Refresh Cookie 使用 `SameSite=Lax`，生产环境启用 `Secure`。

写入 Key 只拥有事件上报权限，不能访问管理 API。Web 端的写入 Key 被视为公开但受限的项目标识，配合 Origin 白名单和按项目、IP、设备标识的 Redis 限流。App 同样携带写入 Key。管理令牌与写入 Key 完全隔离。

管理员可旋转写入 Key。旧 Key 默认保留 24 小时宽限期，之后自动失效。所有管理写操作记录审计日志。

## 6. 上报协议

### 6.1 请求

```http
POST /api/v1/collect/events
X-Write-Key: <project-write-key>
Content-Type: application/json
```

单次包含 1 至 100 条事件，请求体不超过 256KB。项目由写入 Key 推导，客户端不提交可信的 `project_id`。

```json
{
  "events": [
    {
      "event_id": "01J2EXAMPLEULID",
      "point_code": "product_detail_view",
      "event_time": 1784073600123,
      "user_id": "10086",
      "anonymous_id": "device-123",
      "session_id": "session-abc",
      "platform": "android",
      "app_version": "3.2.1",
      "channel": "huawei",
      "properties": {
        "product_id": "123",
        "source_page": "search"
      }
    }
  ]
}
```

`event_id` 必填，使用 UUIDv7 或 ULID；客户端重试同一个业务事件时必须保持不变。`user_id` 和 `anonymous_id` 至少存在一个。

### 6.2 API 同步处理

API 按以下顺序处理：

1. 根据写入 Key 识别项目并检查宽限期。
2. 校验 Origin、请求大小和限流。
3. 校验请求结构。
4. 从本地缓存或 Redis 缓存加载已发布点位快照。
5. 校验点位、平台、必填属性、属性类型、枚举和未知属性。
6. 将合法事件和不可变的点位快照 ID 写入一个队列批次。
7. 将属于已识别项目的非法事件写入无效事件流，供 Worker 保存到 ClickHouse。
8. 返回每条事件的结果以及汇总的 `accepted` 和 `rejected`。

`accepted` 表示事件已持久写入队列，不表示统计已经完成。点位在入队后被废弃，不影响已经通过校验的事件；Worker 按消息中的不可变快照版本处理。

### 6.3 时间口径

同时保存客户端事件时间和服务端接收时间。客户端时间允许过去 7 天、未来 10 分钟。超出范围时使用服务端时间参与统计，并保存原始时间及 `time_corrected=true`。统计日期和小时按项目时区计算。

### 6.4 访客标识

```text
登录：u:{user_id}
未登录：a:{anonymous_id}
```

系统使用项目级盐执行 HMAC-SHA256，并截取稳定哈希供 Redis 和 ClickHouse 使用。统计存储不保留原始用户或设备标识。一期不合并登录前后的身份，点位文档必须明确该 UV 口径。

## 7. 队列与消费可靠性

### 7.1 Redis Stream

一期使用：

- 有效事件流：`tracking:{queue}:events`
- 无效事件流：`tracking:{queue}:invalid`
- 死信流：`tracking:{queue}:dead`
- 消费者组：`tracking-workers`

三个流必须使用同一个非空 Redis Cluster hash tag（默认 `{queue}`）。发布去重键、延迟重试 ZSET/HASH 也从源流提取该 tag，确保发布、重试调度、到期提升和 DLQ 转移的多键 Lua 脚本始终位于同一 hash slot；缺失或不一致的 tag 在启动配置校验和适配器调用前置校验中直接拒绝。

一次客户端请求形成一个稳定 `batch_id` 的 Stream 消息，包含最多 100 条事件。API 使用 Pipeline 写入，成功后才返回 accepted。流按时间和长度策略裁剪，但未确认的 Pending 消息不得因裁剪丢失。

### 7.2 Worker 处理顺序

1. 从消费者组读取批次。
2. 按 `batch_id` 获取有过期时间的处理租约。
3. 按不可变点位快照执行防御性校验。
4. 使用 `batch_id` 作为稳定的 ClickHouse 插入去重令牌，批量写入有效事件或无效事件。
5. 对每个有效事件执行 Redis Lua：检查事件完成标记，原子更新 PV、UV 和完成标记。
6. 全部成功后 ACK Stream 消息。

Redis Lua 使重复消息不会重复增加实时 PV/UV。ClickHouse 版本固定为 26.3 LTS；写入显式启用异步插入、稳定去重令牌和可靠确认。原始表仍使用 `ReplacingMergeTree` 作为第二层防御，逻辑明细查询按 `event_id` 读取最新记录。

### 7.3 错误分类

- 业务校验失败：写入无效事件表，记录错误码、错误信息和原始数据，标记完成并 ACK，不重试。
- MySQL、Redis 或 ClickHouse 暂时性错误：保留 Pending，指数退避重试。
- 消费者失联：Scheduler 回收超过 60 秒未处理的 Pending 消息。
- 同一消息累计失败超过 5 次：移入死信流，记录失败阶段、原因和次数，并 ACK 原消息。

死信默认保留 7 天。管理端一期只提供查看和复制诊断信息，不提供任意重放按钮，避免误操作造成再次写入；运维可在修复原因后使用受控命令重放指定死信。

## 8. Redis 实时统计

Redis 保存最近 3 天的实时指标。

PV 同时维护小时和日计数：

```text
rt:pv:hour:{project}:{yyyyMMddHH}:{point}:{platform}:{channel}
rt:pv:day:{project}:{yyyyMMdd}:{point}:{platform}:{channel}
```

UV 同时维护小时和日去重结构：

- 默认 `APPROXIMATE`：HyperLogLog，使用 `PFADD` 和 `PFCOUNT`。
- 特定点位 `EXACT`：Redis Set，使用 `SADD` 和 `SCARD`。

小时 UV 和日 UV 独立计算，不能把小时 UV 相加得到日 UV。Lua 脚本在一个原子操作中完成 event_id 去重、PV 增量、UV 去重、TTL 和完成标记。

Redis 实时接口只用于今天总览和当前小时趋势，不与 ClickHouse 的当前小时数据拼接，避免重叠计数。

## 9. ClickHouse 数据模型

### 9.1 原始事件

`tracking_event` 使用 `ReplacingMergeTree(version)`：

- `event_id`
- `batch_id`
- `project_id`
- `point_id`、`point_code`、点位版本
- 三级分类 ID 和编码快照
- `visitor_hash`
- 平台、渠道、App 版本和 Session 哈希
- 客户端时间、服务端接收时间、统计时间和 `time_corrected`
- `properties_json`
- 插入版本

按接收月份分区：

```sql
PARTITION BY toYYYYMM(received_at)
```

排序键以项目、点位、统计日期、统计时间和 event_id 开头，支持主要查询路径。原始事件设置：

```sql
TTL received_at + INTERVAL 30 DAY DELETE
```

### 9.2 无效事件

`tracking_invalid_event` 保存项目、批次、event_id、点位编码、平台、错误码、错误信息、原始 JSON 和接收时间，同样保留 30 天。

### 9.3 聚合表

Scheduler 每分钟从 `tracking_event FINAL` 重算当前小时和上一个小时的快照。`FINAL` 先按 event_id 消除客户端跨批次重试产生的逻辑重复，再计算精确 PV 和 UV 聚合状态。该方式不依赖物化视图对重复事件的处理顺序，保证 Redis Stream 批次重试和客户端 event_id 重试都不会放大历史 PV。

- `business_stat_hourly`：`ReplacingMergeTree(version)`，每个小时和维度组合保存精确 `pv`、`uniqCombined64State` UV 状态及版本。
- `business_stat_exact_hourly`：只保存 `uv_mode=EXACT` 的点位快照，使用 `uniqExactState` UV 状态。

聚合键包括小时、项目、三级分类、点位、平台和渠道。查询聚合表时先使用 `FINAL` 读取每个组合的最新快照。聚合结果不设置 TTL。跨小时 PV 直接相加；日、周和任意范围 UV 合并状态，共同用户只计算一次，禁止相加各小时最终 UV 数字。

### 9.4 查询路由

- `/statistics/realtime`：Redis，今天总览和当前小时，目标 5 秒内可见。
- `/statistics/summary` 和 `/statistics/trend`：ClickHouse 聚合表，目标 1 分钟内可见。
- `/diagnostics/recent-events`：ClickHouse 原始事件，仅限近 30 天。
- 点位、分类、负责人和权限：MySQL。

固定聚合维度为项目、三级分类、点位、平台、渠道和时间。自定义 properties 仅支持近 30 天原始明细查询。

## 10. MySQL 元数据模型

MySQL 至少包含：

- `user_account`
- `refresh_token`
- `project`
- `project_member`
- `tracking_category`
- `tracking_point`
- `tracking_point_property`
- `tracking_point_snapshot`
- `audit_log`

所有业务表使用显式唯一约束和软停用字段。已发布点位由领域服务和数据库约束共同保护，不提供通用更新或删除接口。

## 11. 管理端设计

采用 Vue 3、TypeScript 和 Element Plus。

### 11.1 页面

1. 登录和首次修改密码。
2. 项目概览：首页卡片展示实时 PV/UV、ClickHouse 趋势、无效事件、死信和长期无上报点位。
3. 点位工作台：左侧三级分类树，右侧搜索、筛选和点位列表。
4. 点位详情：定义、触发规则、属性 Schema、负责人、变更记录、最近上报和 JavaScript、Go、cURL 示例。
5. 业务分类：三级分类树管理。
6. 统计分析：时间、分类、点位、平台和渠道筛选，展示汇总和趋势。
7. 数据诊断：无效事件、死信和最近上报。
8. 项目设置：成员、角色、写入 Key、Origin 白名单和时区。

### 11.2 点位向导

新增和编辑草稿使用四步向导：

1. 基础定义：分类、名称、编码、行为类型和说明。
2. 触发与统计：触发规则、平台、PV/UV 和 UV 模式。
3. 属性 Schema：类型、必填、枚举、示例和说明。
4. 协作与发布：负责人、需求链接、请求预览和完整性校验。

每一步可以保存草稿。只有管理员可以发布。已发布详情页不显示编辑入口。

## 12. API 边界

### 12.1 认证

- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/me`
- `POST /api/v1/auth/change-password`

### 12.2 项目

- `GET /api/v1/projects`
- `POST /api/v1/projects`
- `GET /api/v1/projects/{id}`
- `PATCH /api/v1/projects/{id}/settings`
- `GET|POST|DELETE /api/v1/projects/{id}/members`
- `POST /api/v1/projects/{id}/rotate-write-key`

### 12.3 分类和点位

- `GET /api/v1/projects/{id}/categories/tree`
- `POST /api/v1/projects/{id}/categories`
- `PATCH /api/v1/projects/{id}/categories/{categoryId}`
- `POST /api/v1/projects/{id}/categories/{categoryId}/disable`
- `GET|POST /api/v1/projects/{id}/points`
- `GET /api/v1/projects/{id}/points/{pointId}`
- `PATCH /api/v1/projects/{id}/points/{pointId}/draft`
- `DELETE /api/v1/projects/{id}/points/{pointId}/draft`
- `POST /api/v1/projects/{id}/points/{pointId}/publish`
- `POST /api/v1/projects/{id}/points/{pointId}/deprecate`
- `POST /api/v1/projects/{id}/points/{pointId}/copy`

### 12.4 统计和诊断

- `GET /api/v1/projects/{id}/statistics/realtime`
- `GET /api/v1/projects/{id}/statistics/summary`
- `GET /api/v1/projects/{id}/statistics/trend`
- `GET /api/v1/projects/{id}/diagnostics/invalid-events`
- `GET /api/v1/projects/{id}/diagnostics/dead-letters`
- `GET /api/v1/projects/{id}/diagnostics/recent-events`

## 13. 工程结构

```text
api/
├── auth/v1
├── projects/v1
├── tracking/v1
├── statistics/v1
└── collect/v1
internal/
├── cmd/                 # api、worker、scheduler
├── controller/
├── domain/              # auth、project、tracking、statistics
├── ports/               # queue、eventstore、counter、repository
├── logic/
└── adapter/             # mysql、redis、clickhouse
migrations/
├── mysql/
└── clickhouse/
web/
deploy/
docs/
```

GoFrame YAML 保存非敏感默认配置。MySQL、Redis 和 ClickHouse DSN、JWT 密钥、项目盐以及密码通过环境变量注入。Docker Compose 启动 Web、API、Worker、Scheduler、MySQL、Redis 和 ClickHouse，并通过幂等迁移完成初始化。

## 14. 可观测性

JSON 结构化日志必须包含适用的 `request_id`、`batch_id`、`event_id` 和 `project_id`。API 提供：

- `/health/live`
- `/health/ready`
- `/metrics`

Prometheus 指标至少包括：

- 接收、接受、拒绝、重复和失败事件总量。
- Redis Stream 积压、Pending、死信数量和最老消息年龄。
- 消费耗时和重试次数。
- Redis 操作耗时。
- ClickHouse 批次大小、写入耗时和失败次数。
- 实时统计延迟和聚合可见延迟。

项目概览展示业务数据异常；基础设施指标通过 `/metrics` 暴露给运维系统。

## 15. 测试策略

### 15.1 单元测试

- 点位状态机和不可变规则。
- 分类层级和引用约束。
- 属性 Schema 校验。
- visitor_hash、时间归一化和项目时区。
- UV 模式和统计 Key 构造。
- 首次改密和项目级权限判断。

### 15.2 适配器契约测试

- Redis Stream 发布、消费、ACK、Pending 回收和 DLQ。
- Redis Lua 幂等计数。
- MySQL 仓储唯一约束和事务。
- ClickHouse 稳定批次去重、ReplacingMergeTree 逻辑去重、TTL 定义和小时快照状态合并。

每个消息队列适配器必须通过同一组 `MessageQueue` 契约测试，保证将来新增 Kafka 后语义一致。

### 15.3 集成测试

- 登录、刷新、首次改密和 RBAC。
- 项目、分类和点位完整生命周期。
- 从上报到 Redis 实时统计和 ClickHouse 趋势的完整链路。
- 重复 event_id、重复批次和 Worker 重启。
- ClickHouse 停机、Pending 保留、恢复消费和不重复统计。
- 连续失败 5 次进入死信。

### 15.4 Web E2E

- `admin/admin` 首次登录和强制改密。
- A 型点位工作台布局、分类筛选和搜索。
- 四步点位向导、保存草稿、发布和发布后不可编辑。
- 只读账号不显示写入口且服务端返回 403。
- 项目概览、统计趋势和数据诊断。

### 15.5 容量验证

提供 k6 脚本，以峰值 2000 events/s 运行至少 10 分钟，记录 API p95、错误率、队列积压、消费吞吐和停止压测后的恢复时间。容量结论必须注明测试硬件和配置，不把开发机结果视为生产承诺。

## 16. 一期验收标准

1. 从空数据卷执行 `docker compose up --build` 后，所有健康检查通过，MySQL 和 ClickHouse 迁移完成。
2. 使用 `admin/admin` 登录后必须修改密码，改密后可创建项目和成员。
3. 管理员可创建三级分类和点位草稿；发布后不能编辑或删除，只能复制或废弃。
4. 只读用户的全部管理写请求返回 403。
5. 客户端使用写入 Key 批量上报；未知点位、错误平台、缺失属性、错误类型、非法枚举和未知属性均有明确错误，并能在诊断页查询。
6. 同一 event_id 或同一队列批次重复投递时，PV 只增加一次，UV 不重复。
7. Redis 实时结果在 5 秒内可见，ClickHouse 明细与趋势在 1 分钟内可见。
8. 停止 ClickHouse 后消息留在 Pending；恢复后自动完成消费且统计不重复。
9. 同一消息失败超过 5 次进入死信流，并在项目概览和诊断页可见。
10. 日、周和任意范围 UV 通过聚合状态合并，验证共同用户只计算一次。
11. 原始有效和无效事件配置 30 天 TTL，聚合结果无 TTL。
12. 单元、适配器契约、集成和 Web E2E 测试全部通过。

## 17. 参考依据

- ClickHouse 官方建议批量写入，或使用异步写入并等待可靠确认：<https://clickhouse.com/resources/engineering/high-concurrency-sizing-user-analytics>
- ClickHouse 官方分区实践强调分区主要用于数据生命周期管理，避免按天或高基数字段过度分区：<https://clickhouse.com/blog/10-best-practice-tips>
- ClickHouse 26.1 起强化了异步插入的一致去重能力，设计固定使用 26.3 LTS：<https://clickhouse.com/blog/clickhouse-release-26-01>
