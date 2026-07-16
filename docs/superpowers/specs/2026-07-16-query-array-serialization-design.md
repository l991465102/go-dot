# 前端数组查询参数序列化修复设计

## 问题

前端把 `pointCodes: string[]` 交给 Axios 默认序列化，生成
`point_code[]=android_uv`。Go 后端只读取名为 `point_code` 的查询参数，
因此实时统计接口认为点位编码缺失并返回 HTTP 400。

## 方案

在共享 Axios 实例中统一配置数组参数使用“重复键、不带方括号”的格式：

```text
point_code=android_uv
point_code=a&point_code=b
```

统计、项目概览和诊断模块继续传递数组，不各自拼接 URL。后端保持现有
`point_code` 合约，不额外接受 `point_code[]`，避免维护两套参数格式。

## 验收

- 单点位 URL 包含 `point_code=android_uv`，不包含 `point_code[]`。
- 多点位 URL 使用重复的 `point_code` 键。
- 统计和诊断 API 客户端共享该行为。
- 原有登录刷新、请求重放和其他查询参数行为不回退。
- 前端单测、类型检查和生产构建通过。

## 测试策略

先在共享 API 客户端测试中使用 Axios 的真实 `getUri` 生成 URL，确认测试在
当前默认序列化下因出现 `%5B%5D` 而失败；再加入统一 serializer，使测试转绿，
最后运行完整前端测试和构建。
