# go-dot 接入工具

这两个 SDK 支持正式和测试采集地址。202 表示采集接口已接收并返回逐条结果；不能据此判断正式事件已经入库。请检查每条 `status` 和错误码。

## Go

```go
import godot "github.com/lzw7758/go-dot/sdk/go"

client, err := godot.NewClient(godot.Options{BaseURL: "https://tracking.example.com", WriteKey: writeKey, MaxRetries: 3})
if err != nil { return err }
events := []godot.Event{{PointCode: "product_detail_view", Platform: "server", UserID: "123", Properties: map[string]any{"product_id": "42"}}}
results, err := client.Send(ctx, events)
// events 中补充的 event_id 和 event_time 应随事件持久化，失败重试必须复用。
// err 非空时 results 包含已经完成的批次；按其 index 识别剩余事件。
```

`Prepare` 可在持久化队列时预先生成 ID 和时间。`Send` 每批最多 100 条，对网络错误、429 和 5xx 按配置退避重试；其他 HTTP 错误直接返回。调用方管理持久队列、重试上限和被拒事件。上下文可取消请求。

## Web

```js
import { Client } from '@go-dot/client'
const client = new Client({ baseURL: 'https://tracking.example.com', writeKey, test: false })
client.track({ point_code: 'product_detail_view', platform: 'web', anonymous_id: installationId, properties: { product_id: '42' } })
const results = await client.flush()
```

`track` 将事件加入内存队列并返回 ID；`pending()` 返回带 ID 和时间的快照，调用方可存储并在重建后逐条 `track` 恢复。`flush` 对网络错误、429 和 5xx 重试；校验拒绝的事件返回给调用方，不会自动重复发送。遇到失败时未完成批次留在队列；异常对象的 `completed` 包含此前批次结果。刷新页面或关闭浏览器会丢失未持久化的队列，不提供自动持久化或关闭页面时的送达保证。Web 的 write key 是受限项目标识，不能作为秘密。

## 从已发布定义生成类型

Node.js 24 下设置管理 API 的 `GO_DOT_ACCESS_TOKEN`，再运行：

```sh
node sdk/generate.mjs https://tracking.example.com 1 ts > tracking-types.ts
node sdk/generate.mjs https://tracking.example.com 1 go > tracking_types.go
```

生成器只读取已发布点位，分页读取后输出 TypeScript 的 `TrackingEvents` 映射或 Go 的 `Point<ID>` 常量和 `Properties<ID>` 结构体。生成结果是一个时间点的快照，发布点位变化后需要重新生成。生成器不写入 token，也不修改项目。生成类型不代替服务端校验。
