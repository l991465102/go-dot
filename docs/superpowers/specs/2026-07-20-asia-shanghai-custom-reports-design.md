# Asia/Shanghai 时间展示与自定义点位日报设计

## 1. 背景与目标

当前数据诊断页面直接展示 API 返回的 RFC3339 UTC 时间，例如
`2026-07-20T02:51:03Z`，与中国用户本地看到的北京时间相差 8 小时。同时，
现有统计分析只能把多个点位合并为一个结果，不能按“日期 + 点位”查看每天的
PV/UV，也不能保存常用点位组合或导出 Excel。

本次改动包含两个相互独立但在页面口径上统一的能力：

1. 所有面向用户展示的绝对时间固定按 `Asia/Shanghai` 格式化。
2. 新增“自定义报表”页面，支持临时选择点位查询、保存命名报表，以及把当前
   查询结果导出为 XLSX。

后端、Redis 和 ClickHouse 继续使用 UTC 存储及传输。本次不重写历史数据。

## 2. 时间口径

### 2.1 绝对时间展示

前端提供唯一的共享时间格式化函数，固定使用 IANA 时区
`Asia/Shanghai`，输出格式为：

```text
YYYY-MM-DD HH:mm:ss
```

例如：

```text
2026-07-20T02:51:03Z -> 2026-07-20 10:51:03
```

以下当前已展示的绝对时间统一使用该函数：

- 最近事件接收时间；
- 无效事件接收时间；
- 死信首次失败时间；
- 项目写入密钥的旧密钥失效时间；
- 自定义报表的创建、更新时间和 Excel 导出时间。

无效、空或无法解析的时间显示为 `-`。时间列或页面说明明确标注
`Asia/Shanghai`，不依赖浏览器本地时区。

### 2.2 统计日期

自定义报表的日界线固定按 `Asia/Shanghai`：每天从北京时间 00:00:00 开始，
到次日 00:00:00 结束。请求使用闭开区间 `[start_date, end_date + 1 day)`。
如果结果包含北京时间当天，该行标记为“未完整”；历史日期标记为“完整”。

现有趋势图继续遵循项目统计时区，因为它展示的是项目聚合桶，不是普通绝对
时间。改变项目统计时区不在本次范围内。

## 3. 方案选择

采用服务端保存配置、ClickHouse 聚合、服务端生成 Excel 的方案：

```text
自定义报表页面
  |-- 临时选择点位与日期
  |-- 加载或保存命名报表
  `-- 导出当前查询
          |
          v
GoFrame API
  |-- MySQL: 报表定义与点位顺序
  |-- ClickHouse: 日期 + 点位 PV/UV
  `-- XLSX: 数据明细 + 报表说明
```

不采用浏览器本地保存，因为无法跨设备共享和实施角色权限；不采用异步导出与
对象存储，因为 90 天乘 100 个点位最多 9000 行，同步流式导出足够。

## 4. 权限模型

沿用项目成员角色：

| 能力 | 管理员 | 只读用户 |
|---|---:|---:|
| 临时查询 | 是 | 是 |
| 查看命名报表 | 是 | 是 |
| 导出当前结果 | 是 | 是 |
| 新建命名报表 | 是 | 否 |
| 修改命名报表 | 是 | 否 |
| 删除命名报表 | 是 | 否 |

所有接口先校验操作者对 `project_id` 的访问权。报表和点位必须属于同一项目，
禁止通过报表 ID 或点位 ID 跨项目读取或修改数据。

## 5. 命名报表数据模型

### 5.1 `custom_report`

```sql
CREATE TABLE custom_report (
    id            BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    project_id    BIGINT UNSIGNED NOT NULL,
    name          VARCHAR(128) NOT NULL,
    description   VARCHAR(1000) NOT NULL DEFAULT '',
    default_days  SMALLINT UNSIGNED NOT NULL DEFAULT 30,
    created_by    BIGINT UNSIGNED NOT NULL,
    updated_by    BIGINT UNSIGNED NOT NULL,
    created_at    DATETIME(3) NOT NULL,
    updated_at    DATETIME(3) NOT NULL,
    UNIQUE KEY uk_custom_report_project_name (project_id, name),
    KEY idx_custom_report_project (project_id, id)
);
```

`default_days` 范围为 1 至 90。

### 5.2 `custom_report_point`

```sql
CREATE TABLE custom_report_point (
    report_id   BIGINT UNSIGNED NOT NULL,
    point_id    BIGINT UNSIGNED NOT NULL,
    sort_no     SMALLINT UNSIGNED NOT NULL,
    PRIMARY KEY (report_id, point_id),
    UNIQUE KEY uk_custom_report_point_order (report_id, sort_no),
    KEY idx_custom_report_point (point_id)
);
```

报表保存 1 至 100 个点位及其显示顺序。可选择已发布或已废弃点位，不能选择
草稿点位。点位废弃后仍保留在已有报表中，以便查询历史数据。

删除报表只删除这两张表中的配置，不删除点位、原始事件或聚合结果。保存和
更新报表时，主表与点位关系表在同一个 MySQL 事务内完成。

## 6. API 设计

### 6.1 报表配置

```text
GET    /api/v1/projects/{projectId}/reports
POST   /api/v1/projects/{projectId}/reports
GET    /api/v1/projects/{projectId}/reports/{reportId}
PATCH  /api/v1/projects/{projectId}/reports/{reportId}
DELETE /api/v1/projects/{projectId}/reports/{reportId}
```

创建和更新请求：

```json
{
  "name": "核心转化日报",
  "description": "产品详情与下单核心点位",
  "default_days": 30,
  "point_ids": [12, 18, 21]
}
```

列表返回轻量摘要；详情返回有序点位列表、点位编码、名称、定义状态、UV 模式
和创建/更新时间。项目内重名返回 HTTP 409，非法点位或跨项目点位返回统一的
安全错误，不泄露其他项目是否存在对应资源。

### 6.2 日报查询

```text
POST /api/v1/statistics/daily-by-point/query
```

请求：

```json
{
  "project_id": 1,
  "start_date": "2026-07-01",
  "end_date": "2026-07-20",
  "point_ids": [12, 18, 21]
}
```

约束：

- 日期必须是 `YYYY-MM-DD`；
- 开始日期不能晚于结束日期；
- 最多 90 个自然日；
- 点位数量为 1 至 100；
- 点位 ID 去重后仍保留用户指定的首次出现顺序；
- 日期不能晚于当前 `Asia/Shanghai` 日期。

响应：

```json
{
  "timezone": "Asia/Shanghai",
  "start_date": "2026-07-01",
  "end_date": "2026-07-20",
  "point_count": 3,
  "day_count": 20,
  "total_pv": 126580,
  "items": [
    {
      "date": "2026-07-20",
      "complete": false,
      "point_id": 12,
      "point_code": "dot_10086",
      "point_name": "示例点位",
      "pv": 120,
      "uv": 85,
      "uv_mode": "EXACT"
    }
  ]
}
```

结果按日期倒序、请求点位顺序排列。没有事件的“日期 + 点位”组合也返回一行，
PV/UV 均为 0，确保表格和 Excel 的日期矩阵完整。

`total_pv` 可以安全相加。接口不返回 `total_uv`，页面也不汇总 UV，避免把不同
日期或不同点位去重结果错误相加。

### 6.3 Excel 导出

```text
POST /api/v1/statistics/daily-by-point/export
```

导出请求复用日报查询模型，并允许附带当前报表展示信息：

```json
{
  "project_id": 1,
  "start_date": "2026-07-01",
  "end_date": "2026-07-20",
  "point_ids": [12, 18, 21],
  "report_name": "核心转化日报",
  "report_description": "产品详情与下单核心点位"
}
```

查询与导出调用同一个应用服务，避免页面与文件出现不同统计结果。响应使用
XLSX MIME 类型和安全的 `Content-Disposition` 文件名，例如：

```text
核心转化日报_20260701_20260720.xlsx
```

工作簿包含：

1. `数据明细`：日期、数据状态、点位编码、点位名称、PV、UV、UV口径；
2. `报表说明`：报表名称、说明、查询范围、统计时区、点位清单、导出时间、
   导出用户。

Excel 使用流式写入。导出结果不落盘，不在服务端长期保存。

## 7. ClickHouse 查询口径

复用现有小时聚合表和 generation 提交标记，不直接扫描原始事件：

- 把请求点位按 MySQL 中不可变的 UV 模式分成精确组和近似组；
- 精确组查询 `business_stat_exact_hourly`，使用 `uniqExactMerge`；
- 近似组查询 `business_stat_hourly`，使用 `uniqCombined64Merge`；
- 两组都把小时按 `Asia/Shanghai` 转换为自然日，并按日期、`point_id`、
  `point_code` 分组；
- 每个点位独立计算 UV，因此同一张报表可以同时包含精确和近似点位，不会把
  两种 UV 状态合并；
- PV 对同一点位同一天的已提交小时值求和；
- 只读取 generation 已提交的小时版本，保持与现有历史统计相同的约一分钟
  延迟和重算一致性；
- 点位名称和当前状态来自 MySQL 点位定义，不依赖 ClickHouse 保存展示文案。

应用层合并两种 UV 模式的结果，并补齐无数据日期，避免在 ClickHouse 中生成
笛卡尔积。查询参数全部绑定，不拼接用户输入。

## 8. 前端页面

侧边栏新增“自定义报表”。页面包括：

### 8.1 报表工具栏

- 切换“临时查询”或已有命名报表；
- 管理员显示新建、保存配置、另存为、删除；
- 只读用户仅显示打开、查询和导出。

### 8.2 查询条件

- 日期范围默认最近 30 天，允许 1 至 90 天；
- 点位多选支持按编码、名称和业务分类搜索；
- 最多选择 100 个点位，并支持调整显示顺序；
- 打开命名报表时加载默认天数和点位顺序；
- 临时调整不会自动覆盖报表，管理员必须主动点击“保存配置”。

### 8.3 结果表格

表格列为日期、数据状态、点位编码、点位名称、PV、UV、UV口径。页面显示选中
点位数、日期数和总 PV，并明确展示“统计时区：Asia/Shanghai”。当前日期使用
醒目的“未完整”标签。

“导出 Excel”始终导出页面当前日期和点位条件，而不是命名报表最初保存的条件。

## 9. 错误处理与安全

- 没有查询权限返回 403；资源不存在或对当前项目不可见返回 404；
- 重名和并发修改冲突返回 409；
- 日期、点位数量或参数不合法返回 400；
- Excel 生成失败返回 500 且不发送半个工作簿；
- 前端展示可操作的中文错误信息，并保留已有统一鉴权刷新行为；
- 报表名称、说明和 Excel 单元格内容按普通文本写入。以 `=`, `+`, `-`, `@`
  开头的用户文本进行公式注入防护；
- 文件名移除路径和控制字符，同时提供兼容 UTF-8 的下载文件名。

## 10. 测试与验收

按测试驱动方式实施：

1. 时间工具测试 UTC 到 `Asia/Shanghai` 的转换、非法值和浏览器时区无关性；
2. 数据诊断、死信和密钥过期时间组件测试不再显示原始 `Z` 时间；
3. MySQL 仓储测试报表增删改查、排序、重名、事务和项目隔离；
4. 服务测试管理员/只读用户权限、90 天和 100 点位边界；
5. ClickHouse 测试日期边界、零值补齐、PV、精确 UV、近似 UV与多点位隔离；
6. 控制器测试查询与导出请求校验、状态码及下载响应头；
7. 前端测试临时查询、打开报表、管理员保存、只读隐藏管理按钮和导出；
8. 使用 XLSX 读取器重新打开导出文件，验证两个工作表、表头、数据、时区说明
   和公式注入防护；
9. 运行 Go 单元测试及竞态检查、前端测试、类型检查和生产构建；
10. 在本地集成环境中创建命名报表，查询至少两个点位和两个日期，并核对页面、
    ClickHouse 与 Excel 三方数值一致。

## 11. 非目标

本次不实现：

- 跨点位或跨日期的总 UV；
- 定时邮件、异步导出、对象存储或下载历史；
- 图表编排、公式指标、漏斗和留存分析；
- 报表公开链接或项目外分享；
- 修改历史事件、Redis/ClickHouse 的 UTC 存储约定；
- 自动迁移已有项目的统计时区。

## 12. 验收标准

- 诊断页 UTC `02:51:03Z` 稳定显示为北京时间 `10:51:03`；
- 管理员能创建、修改、另存和删除命名报表；只读用户只能查看、查询和导出；
- 临时查询不需要保存报表，且不会意外覆盖已有配置；
- 最多 100 个点位、90 天的数据按日期和点位独立返回 PV/UV；
- 当天数据标记为未完整，无数据组合显示 0；
- 页面和 Excel 数值一致，Excel 含“数据明细”和“报表说明”；
- 所有新增接口实施项目隔离和输入限制；
- 原有统计、诊断、登录、点位管理与查询参数序列化测试不回退。
