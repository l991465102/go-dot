# 打点属性统计与自定义报表设计

## 背景

当前系统支持在点位中定义属性 Schema，客户端上报的属性经过类型校验后以 `properties_json` 保存到 ClickHouse `tracking_event`。现有统计只覆盖点位 PV/UV，不支持：

- 对数值属性执行 COUNT、SUM、AVG、MIN、MAX；
- 按枚举、字符串或布尔属性值查看事件数、UV 和占比；
- 在自定义报表中选择属性并保存查询配置；
- 将属性统计按当前页面选择导出到 Excel。

原始事件 TTL 为 30 天，而自定义报表支持 1～90 个自然日查询，因此不能只依赖查询时解析 `properties_json`。

## 目标

- 在点位属性定义阶段显式声明属性是否参与统计以及允许的统计方式。
- 支持数值指标和分类维度两类属性统计。
- 在现有“自定义报表”中选择需要查询和显示的属性。
- 页面只查询、展示用户当前选择的属性和聚合方式。
- 命名报表持久化属性选择；临时查询不要求保存。
- Excel 严格跟随页面当前选择，而非已保存报表的旧配置。
- 按 `Asia/Shanghai` 自然日提供 1～90 天查询。
- 支持当前约 20 万日活规模，并为后续扩展小时、周、月粒度保留空间。

## 非目标

- 第一期不支持 DATETIME、ARRAY 属性统计。
- 第一期不支持小时、周、月粒度。
- 第一期不支持漏斗、留存、路径等用户行为分析。
- 不允许查询任意未在点位定义中启用统计的 JSON 属性。
- 不把属性统计混入 Redis 实时 PV/UV。
- 不改变现有点位 PV/UV 统计口径。

## 方案选择

### 采用方案：属性明细表加小时预聚合

Worker 只拆解点位定义中已启用统计的属性，写入属性明细。Scheduler 按小时生成数值和分类聚合，自定义报表再按 `Asia/Shanghai` 自然日合并。

```text
点位属性统计定义
        ↓
客户端上报 properties
        ↓
Worker 校验并写入
  ├─ tracking_event
  └─ tracking_property_event
        ↓
Scheduler 小时聚合
  ├─ property_metric_hourly
  ├─ property_dimension_hourly_approximate
  └─ property_dimension_hourly_exact
        ↓
自定义报表按上海自然日查询
        ↓
页面多表展示 / Excel 多工作表导出
```

采用该方案的原因：

- 原始事件只保留 30 天，预聚合可以支持 90 天查询。
- 只处理显式启用统计的属性，避免任意 JSON 属性产生不可控查询。
- 小时聚合可以正确处理迟到事件并复用现有 Scheduler generation/version 机制。
- 数值聚合和分类 UV 可以使用适合各自口径的存储结构。

### 未采用方案

1. 查询时直接解析 `properties_json`：实现简单，但只能可靠覆盖原始事件保留期，且重复扫描、解析成本高。
2. 只建立属性明细表并实时聚合：避免 JSON 解析，但 90 天分类 UV 查询仍会扫描大量明细。

## 属性统计模型

属性增加统计配置：

```json
{
  "code": "quantity",
  "name": "商品数量",
  "type": "INTEGER",
  "analytics": {
    "mode": "METRIC",
    "aggregations": ["COUNT", "SUM", "AVG", "MIN", "MAX"]
  }
}
```

分类属性示例：

```json
{
  "code": "phone_type",
  "name": "手机类型",
  "type": "ENUM",
  "enum_values": ["ios", "android", "harmony"],
  "analytics": {
    "mode": "DIMENSION"
  }
}
```

### analytics.mode

- `NONE`：不参与属性统计。
- `METRIC`：数值指标。
- `DIMENSION`：分类维度。

### 类型约束

| 属性类型 | 支持模式 | 说明 |
| --- | --- | --- |
| INTEGER | NONE、METRIC | 支持全部数值聚合 |
| DECIMAL | NONE、METRIC | 使用定点数聚合，避免金额类数据使用 Float64 |
| STRING | NONE、DIMENSION | 显式开启，并显示高基数风险提示 |
| ENUM | NONE、DIMENSION | 展示已定义的全部枚举值 |
| BOOLEAN | NONE、DIMENSION | 按 true、false 分类 |
| DATETIME | NONE | 第一期不支持 |
| ARRAY | NONE | 第一期不支持 |

### 数值聚合

- `COUNT`：成功上报该属性的事件数。
- `SUM`：属性值总和。
- `AVG`：`SUM / COUNT`，跨小时、跨日合并时不使用平均值相加。
- `MIN`：最小值。
- `MAX`：最大值。

`METRIC` 至少配置一种允许的聚合方式。

### 分类指标

分类属性固定提供：

- 事件数；
- UV；
- 占比。

占比口径：

```text
属性值事件数 / 当天该属性全部有效事件数
```

分类 UV 沿用点位的 UV 模式：

- `APPROXIMATE` 使用近似去重；
- `EXACT` 使用精确去重。

### 缺失值

- 属性未上报时不参与 COUNT、SUM、AVG、MIN、MAX。
- 属性未上报时不形成分类值。
- 空字符串是已上报的合法字符串值，页面显示为“空字符串”，不与属性缺失混合。

## 点位发布规则

- 属性统计配置进入点位发布快照。
- 已发布点位不允许直接改变属性统计口径。
- 已发布点位需要新增统计能力时，使用“复制新增”创建新点位。
- Worker 按事件对应的点位版本快照解释属性统计配置，避免新旧事件口径混用。

## MySQL 数据模型

### tracking_point_property

增加：

```text
analytics_mode          NONE / METRIC / DIMENSION
analytics_aggregations  JSON 数组
```

校验规则由领域层实现，数据库只保存已经校验的配置。

### custom_report_property

新增报表属性选择表：

```text
report_id
point_id
property_code
aggregations
sort_no
```

约束：

- `(report_id, point_id, property_code)` 唯一；
- 点位必须属于报表项目；
- 属性必须存在于点位发布快照且已启用统计；
- 选择的数值聚合必须是点位定义允许的子集；
- 删除报表时级联删除属性选择；
- 删除或移除报表点位时同步删除该点位的属性选择。

## ClickHouse 数据模型

### tracking_property_event

只保存已启用统计且本次事件实际上报的属性：

```text
event_id
project_id
point_id
point_code
point_version
property_code
property_type
value_string
value_integer
value_decimal
visitor_hash
uv_mode
statistic_at
statistic_date
version
```

设计要求：

- 使用 `ReplacingMergeTree(version)` 支持消息重试幂等。
- 按接收月份分区。
- 排序键优先包含 project、point、property、statistic_date 和 event_id。
- 属性明细 TTL 为 30 天，用于重算和诊断。
- INTEGER 使用 Int64；DECIMAL 使用足够精度的定点数，不转换为 Float64。

### property_metric_hourly

维度：

```text
hour
project_id
point_id
point_code
point_version
property_code
```

指标：

```text
value_count
value_sum
value_min
value_max
generation
```

AVG 查询时由 `value_sum / value_count` 计算。

### property_dimension_hourly_approximate

维度额外包含 `property_value`，保存：

```text
event_count
uv_state = uniqCombined64State(visitor_hash)
generation
```

### property_dimension_hourly_exact

维度额外包含 `property_value`，保存：

```text
event_count
uv_state = uniqExactState(visitor_hash)
generation
```

### 聚合保留期

- 属性小时聚合默认保留 400 天。
- 保留期通过配置控制，不能短于自定义报表最大查询范围。

## Worker 与 Scheduler

### Worker

Worker 完成点位属性 Schema 校验后：

1. 写入正常事件。
2. 读取事件点位版本中的属性统计配置。
3. 只拆解已启用统计且实际存在的属性。
4. 按类型写入属性明细。
5. 任一步失败时允许消息重试；ReplacingMergeTree 和 event/property 组合键保证幂等。

### Scheduler

Scheduler：

- 按小时读取属性明细；
- 分别生成数值、近似分类、精确分类聚合；
- 复用现有 generation/version 覆盖式聚合；
- 重算当前小时和回看窗口，纳入迟到事件；
- 聚合完成前不发布对应 generation。

## 自定义报表配置

现有报表配置扩展：

```json
{
  "point_ids": [10, 86],
  "property_queries": [
    {
      "point_id": 10,
      "property_code": "quantity",
      "aggregations": ["SUM", "AVG"]
    },
    {
      "point_id": 86,
      "property_code": "phone_type",
      "aggregations": []
    }
  ]
}
```

### 选择规则

- 选择点位后，页面只列出该点位已启用统计的属性。
- 用户必须显式勾选属性；不自动查询或展示全部属性。
- 数值属性继续勾选需要查看的聚合方式，只能选择定义时允许的方式。
- 分类属性选中后固定查询事件数、UV 和占比。
- 未选择的属性不发送到查询接口、不查询、不展示、不导出。
- 移除点位时同步移除该点位的属性选择。
- 每个报表最多选择 20 个统计属性，其中分类属性最多 5 个。

### 临时查询与命名报表

- 临时查询使用页面当前选择，不需要保存。
- 保存命名报表时保存点位顺序、属性顺序和聚合方式。
- 加载命名报表时恢复保存的属性选择。
- 用户修改已保存报表但尚未保存时，查询和导出都使用页面当前选择。

## 查询接口

扩展每日自定义报表查询请求：

```json
{
  "project_id": 1,
  "start_date": "2026-07-01",
  "end_date": "2026-07-23",
  "point_ids": [10, 86],
  "property_queries": [
    {
      "point_id": 10,
      "property_code": "quantity",
      "aggregations": ["SUM", "AVG"]
    },
    {
      "point_id": 86,
      "property_code": "phone_type"
    }
  ]
}
```

服务端必须重新校验：

- 项目权限；
- 点位归属；
- 属性是否存在并启用统计；
- 聚合方式是否被点位定义允许；
- 20 个属性、5 个分类属性限制；
- 1～90 个上海自然日限制。

响应保持点位统计主结果，并增加：

```text
point_result
metric_sections
dimension_sections
```

### 数值属性结果

每天一行，列按页面选择顺序生成：

```text
时间
dot_10010_quantity_sum
dot_10010_quantity_avg
```

日期降序。没有有效属性值时：

- COUNT、SUM 显示 0；
- AVG、MIN、MAX 显示空值，避免把“无数据”误解为数值 0。

### 分类属性结果

每个已选分类属性一个 section：

```text
时间
属性值
事件数
UV
占比
```

日期降序，同一天内部按事件数降序。

分类值规则：

- ENUM：返回定义中的全部枚举值；当天无数据的枚举值事件数和 UV 为 0。
- BOOLEAN：返回 true、false；当天无数据时为 0。
- STRING：每天返回 Top 50，其余值合并为“其他”。
- STRING 的“其他”UV必须合并剩余值的去重状态，不能用各值 UV 相加。

## 页面设计

自定义报表保留点位选择区，新增“属性统计”区域：

- 按已选点位分组展示可统计属性；
- 属性前提供显示复选框；
- 数值属性展开后显示允许的聚合方式复选框；
- 分类属性显示“事件数、UV、占比”说明；
- STRING 分类属性显示高基数和 Top 50 提示；
- 已选数量展示为 `属性 x/20，分类 y/5`。

查询结果分区：

1. 点位统计：现有点位每日 PV/UV 宽表。
2. 数值属性：只展示已选数值属性和聚合列。
3. 分类属性：每个已选分类属性单独一张表。

属性没有被选择时，不创建结果分区。

## Excel 导出

Excel 导出请求携带页面当前的 `property_queries`，不以数据库中上次保存的配置替代当前选择。

工作表：

1. `点位统计`
2. `数值属性`，仅在选择数值属性时生成
3. 每个分类属性一个工作表，例如 `分类-phone_type`
4. `报表说明`

规则：

- 页面未选择的属性不导出。
- 数值属性只导出已选聚合列。
- 列顺序和页面一致。
- 日期降序。
- 分类表只包含该属性。
- 工作表名称截断到 Excel 允许的 31 个字符并保证唯一。
- 所有用户可控文本继续防止公式注入。
- 报表说明列出点位和本次导出的属性选择快照。

## 数据完整性

- Scheduler 尚未完成某小时聚合时，涉及该小时的自然日标记为“数据未完整”。
- 查询响应为每个自然日返回 `complete` 标记。
- 页面在结果区域顶部列出所有未完整日期，不额外改变数值表和分类表的业务列。
- Excel 在“报表说明”中列出所有未完整日期。
- 不允许把尚未聚合的数据静默当作 0。
- 完整日期的缺失属性值按本设计的 0 或空值规则展示。

## 错误处理

以下情况返回 400：

- 查询未启用统计的属性；
- 数值属性选择了未允许的聚合方式；
- 类型与统计模式不匹配；
- 超过 20 个属性或 5 个分类属性；
- 日期范围无效；
- 属性不属于所选点位。

以下情况返回 403：

- 用户无项目访问权限。

下游 ClickHouse 查询或导出失败返回 503，并记录请求 ID，不在响应中暴露 SQL 或内部配置。

## 测试策略

### 领域与存储

- 属性类型和 analytics 模式组合校验。
- 数值聚合选择必须是允许集合的子集。
- 点位快照包含统计配置。
- 命名报表保存、更新、排序和删除属性选择。
- Worker 只拆解已启用且存在的属性。
- Worker 重试不会产生重复属性明细。
- INTEGER 和 DECIMAL 精度保持。

### 聚合

- COUNT、SUM、AVG、MIN、MAX 小时聚合。
- 跨小时 AVG 使用总和与总数量。
- 精确和近似分类 UV。
- Scheduler 重跑覆盖、迟到事件和 generation 切换。
- STRING Top 50 和“其他”事件数、UV。
- ENUM、BOOLEAN 缺失值补零。

### API

- 管理员和只读用户均可查询、导出。
- 只有管理员可以保存报表配置。
- 未启用属性和非法聚合拒绝。
- 20 属性、5 分类和 90 天边界。
- 临时选择覆盖已保存选择。

### 页面

- 只列出已选点位的可统计属性。
- 只查询和展示已勾选属性。
- 数值属性只显示已选聚合列。
- 移除点位同步清理属性选择。
- 保存、重新加载命名报表恢复选择。
- 未保存修改用于当前查询和导出。

### Excel

- 工作表按当前选择生成。
- 未选择的属性不出现。
- 页面与 Excel 列顺序、日期顺序和数值一致。
- 长工作表名称截断并去重。
- 公式注入防护。
- 报表说明记录本次属性选择快照。

## 验收示例

点位 `dot_10010` 定义：

```text
quantity: INTEGER / METRIC / COUNT,SUM,AVG
price: DECIMAL / METRIC / SUM,AVG
phone_type: ENUM / DIMENSION
```

当前页面只选择：

```text
quantity: SUM,AVG
phone_type: DIMENSION
```

页面和 Excel必须：

- 数值属性只出现 `dot_10010_quantity_sum`、`dot_10010_quantity_avg`；
- 不查询、不显示、不导出 `price`；
- 生成 `phone_type` 分类表；
- Excel 按当前页面选择导出，即使该选择尚未保存到命名报表。
