# 下拉枚举中文化实施计划

## 目标

按已确认的设计，将管理后台所有固定枚举下拉框改为中文标签，同时保持表单、筛选条件和 API 请求中的英文协议值不变。

设计文档：`docs/superpowers/specs/2026-07-22-dropdown-enum-localization-design.md`

## Task 1：建立集中枚举选项模块

**新增文件**

- `web/src/constants/enum-options.ts`
- `web/src/constants/enum-options.spec.ts`

**步骤**

1. 先编写失败测试，覆盖定义状态、开发状态、行为类型、分类层级、平台、属性类型、UV 模式、时间口径、成员角色和时区的中文标签与英文值。
2. 实现只读、带类型约束的 `{ value, label }` 选项数组。
3. 提供仅在组件确有需要时使用的查找函数；不增加全局状态或后端依赖。
4. 运行枚举模块测试和 TypeScript 类型检查。

## Task 2：点位管理筛选和点位详情下拉框

**修改文件**

- `web/src/views/PointWorkbenchView.vue`
- `web/src/views/PointWorkbenchView.spec.ts`
- `web/src/views/PointDetailView.vue`

**新增文件**

- `web/src/views/PointDetailView.spec.ts`

**步骤**

1. 为点位列表定义状态、开发状态、行为类型、平台筛选添加中文选项测试。
2. 验证选择中文选项后传给 `listPoints` 的仍是英文枚举值。
3. 替换点位管理页面内联常量数组。
4. 为点位详情开发状态下拉编写测试，验证中文标签和英文更新值。
5. 替换详情页开发状态下拉内联常量。

## Task 3：点位向导下拉框

**修改文件**

- `web/src/components/points/BasicDefinitionStep.vue`
- `web/src/components/points/TriggerStatisticsStep.vue`
- `web/src/components/points/PropertySchemaStep.vue`
- `web/src/components/points/PointWizard.spec.ts`

**按需新增测试文件**

- `web/src/components/points/BasicDefinitionStep.spec.ts`
- `web/src/components/points/TriggerStatisticsStep.spec.ts`
- `web/src/components/points/PropertySchemaStep.spec.ts`

**步骤**

1. 先覆盖行为类型、UV 模式、时间口径、属性类型和数组元素类型的中文标签。
2. 模拟选择事件，断言向导模型仍保存英文协议值。
3. 将组件内固定选项替换为集中映射。
4. 平台复选框不属于本次下拉范围，不改变现有展示和交互。

## Task 4：分类、项目和成员相关下拉框

**修改文件**

- `web/src/views/CategoryManagementView.vue`
- `web/src/views/ProjectOnboardingView.vue`
- `web/src/views/ProjectSettingsView.vue`
- `web/src/views/ProjectSettingsView.spec.ts`

**按需新增测试文件**

- `web/src/views/CategoryManagementView.spec.ts`
- `web/src/views/ProjectOnboardingView.spec.ts`

**步骤**

1. 为分类层级下拉添加中文标签和英文值测试。
2. 为项目时区下拉添加“上海时区（UTC+8）”“协调世界时（UTC）”测试。
3. 将成员角色下拉接入统一字典，并保持 `ADMIN`、`VIEWER` 请求值。
4. 动态项目、分类、点位、报表选择器继续展示业务数据名称，不改造为枚举字典。

## Task 5：完整扫描与验证

**检查范围**

- `web/src/**/*.vue`
- `web/src/**/*.ts`

**步骤**

1. 使用 `rg` 扫描所有 `<select>`、`<option>`、`<el-select>`、`<el-option>`，确认没有固定英文枚举直接作为用户标签。
2. 对已有中文统计范围、粒度等选项确认英文值显式绑定。
3. 运行：

   ```bash
   cd web
   npm test -- --run
   npm run typecheck
   npm run build
   ```

4. 运行 `git diff --check`，但不修改或提交任务开始前已有的 `docker-compose.yml` 工作区改动。
5. 审查所有改动，确认仅改变下拉标签，不改变后端协议、列表和详情中的非下拉展示。

## 完成标准

- 所有固定枚举下拉框仅显示中文标签。
- 选择、筛选和保存时仍使用原英文协议值。
- 集中枚举字典覆盖设计约定的全部枚举。
- 前端单元测试、类型检查和生产构建通过。
- 未覆盖用户已有的工作区修改。
