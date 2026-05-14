# 实时运行日志与看板设计

本文档描述如何在 **Dashboard** 中实现面向系统可观测性的 **前端实时日志面板**，以及与后端事件模型、传输与安全策略的配套设计。目标读者：前后端实现者。

---

## 1. 目标与事件范围

### 1.1 产品目标

- 在单一面板中聚合「仓位 / 资金 / 连接 / 市场订阅 / 摘要级盘口活动」，支撑排障与审计。
- 默认 **低噪声**：盘口类事件必须经过 **脱敏与摘要**，禁止将完整 order book 每个 tick 推送到日志通道。

### 1.2 事件域（建议映射到 `category`）

| `category` | 说明 |
|------------|------|
| `position` | 检测到的仓位变化、开仓、平仓、止损触发 |
| `balance` | 余额变化（尤其在 **主动刷新** 成功之后） |
| `transport` | WebSocket 断连 / 重连：用户通道、市场通道、Dashboard 与后端的业务 WS（若与风控页相关） |
| `market_sub` | 按市场的订阅生命周期（subscribe / unsubscribe / 重订阅） |
| `market_data` | 入站市场 WS 的 **摘要** 流量（非原始全量 book） |

---

## 2. 通用事件信封（JSON Schema 概念）

所有日志行共享同一 **信封**，便于前端虚拟列表渲染、过滤与关联。

### 2.1 顶层字段（必填 / 强建议）

| 字段 | 类型 | 说明 |
|------|------|------|
| `seq` | `number` | 单调递增序号（**单连接内** 或 **单账户会话内** 全局递增），用于检测丢包与排序 |
| `ts` | `string` | RFC3339 / ISO8601 UTC，如 `2026-05-14T12:34:56.789Z` |
| `type` | `string` | 见 §3 枚举示例；稳定字符串，勿用自由文本 |
| `category` | `string` | `position` \| `balance` \| `transport` \| `market_sub` \| `market_data` \| `system` |
| `severity` | `string` | `debug` \| `info` \| `warn` \| `error` |
| `accountId` | `string` \| `null` | 多租户或匿名连接可为 `null` |
| `marketId` | `string` \| `null` | Polymarket `condition_id` 或内部 `market` 主键；不适用为 `null` |
| `tokenId` | `string` \| `null` | CLOB `token_id`；不适用为 `null` |
| `correlationId` | `string` | 跨服务/跨 WS 帧关联；HTTP 请求可与 `X-Correlation-Id` 对齐 |
| `detail` | `object` | **类型相关** 载荷（见 §3）；禁止在此塞原始大数组 |

### 2.2 示例信封

```json
{
  "seq": 104829,
  "ts": "2026-05-14T12:34:56.789Z",
  "type": "position.stop_loss_triggered",
  "category": "position",
  "severity": "warn",
  "accountId": "acc_01H...",
  "marketId": "0xabc...",
  "tokenId": "12345...",
  "correlationId": "9f3c1b2a-4d5e-6789-abcd-ef0123456789",
  "detail": {
    "triggerPrice": "0.42",
    "highWaterMark": "0.51",
    "thresholdPct": 15
  }
}
```

---

## 3. `type` 枚举示例与 `detail` 约定

以下为 **建议** 的稳定 `type` 前缀命名：`域.动作`，便于过滤与埋点。

### 3.1 `position`（`category: position`）

| `type` | `severity` 典型值 | `detail` 建议字段 |
|--------|-------------------|-------------------|
| `position.detected_change` | `info` | `changeKind`（`size` \| `price` \| `merged`）, `before`, `after`（小对象或哈希摘要，避免整行持仓 dump） |
| `position.opened` | `info` | `source`（`sync` \| `user` \| `api`）, `notional`（可选） |
| `position.closed` | `info` | `reason`（`user` \| `stop_loss` \| `liquidation` \| `unknown`） |
| `position.stop_loss_armed` | `info` | `thresholdPct`, `referencePrice` |
| `position.stop_loss_triggered` | `warn` | `triggerPrice`, `highWaterMark`, `thresholdPct`, `queuedTaskId`（若有） |

### 3.2 `balance`（`category: balance`）

| `type` | 说明 | `detail` 示例 |
|--------|------|---------------|
| `balance.refreshed` | **主动刷新** 成功后的快照差异 | `asset`, `before`, `after`, `delta`, `provider` |

### 3.3 `transport`（`category: transport`）

| `type` | 说明 | `detail` 示例 |
|--------|------|---------------|
| `ws.user.disconnected` / `ws.user.reconnected` | 用户私有通道（订单/仓位推送等） | `closeCode`, `reason`, `attempt`, `backoffMs` |
| `ws.market.disconnected` / `ws.market.reconnected` | 市场 / 官方盘口类通道 | 同上 |
| `ws.dashboard.disconnected` / `ws.dashboard.reconnected` | 浏览器与本服务 Dashboard 业务 WS | `sessionId`, `lastSeq` |

### 3.4 `market_sub`（`category: market_sub`）

| `type` | 说明 | `detail` 示例 |
|--------|------|---------------|
| `market.subscription.started` | 开始订阅某 `tokenId` | `channel`, `depth` |
| `market.subscription.stopped` | 取消订阅 | `reason`（`position_closed` \| `nav_away` \| `error`） |
| `market.subscription.resubscribed` | 重订阅 | `cause`（`reconnect` \| `token_change`） |

### 3.5 `market_data`（`category: market_data`）— **必须摘要化**

| `type` | 说明 | `detail` 示例（禁止原始全 book） |
|--------|------|----------------------------------|
| `market.book.summary_tick` | 降采样后的盘口摘要 | `bestBid`, `bestAsk`, `spread`, `bidDepthTopNHash`, `askDepthTopNHash`, `checksum` |
| `market.book.spread_crossed` | 仅当价差越过阈值 | `prevSpread`, `spread` |
| `market.trade.print` | 可选：成交流摘要（若业务需要） | `side`, `sizeBucket`（分桶）, `price` |

---

## 4. 后端设计

### 4.1 存储形态选型

| 方案 | 适用场景 | 备注 |
|------|----------|------|
| **进程内环形缓冲区（ring buffer）** | 单实例、低延迟、仅「最近 N 条」回放 | 固定容量（如 5k～50k 条），按 `seq` 覆盖写；新订阅连接先 **快照 ring** 再 **实时 tail** |
| **结构化事件总线（in-proc pub/sub）** | 多模块（`risksvc`、WS 网关、book cache）统一产线 | 生产者只 `Publish(envelope)`；消费者之一写入 ring，另一路可选落盘/指标 |
| **组合（推荐起步）** | 中小流量 Dashboard | **总线解耦生产者** + **单 writer 写入 ring**；避免多协程无锁竞争同一切片 |

持久化（审计库）可作为第二阶段：异步批量写入 SQLite/列存，**不阻塞** 实时通道。

### 4.2 传输方式对比

| 方案 | 优点 | 缺点 |
|------|------|------|
| **专用 topic：`system:log`（独立子协议或子 URL）** | 职责清晰、可独立限流与鉴权 | 多一条连接或 multiplex 复杂度 |
| **复用现有 Dashboard WS，统一 typed message** | 一条连接、前端状态简单 | 需严格 `messageType` 区分业务推送与日志，避免与高频市场数据互相阻塞 |

**推荐**：复用 **现有 Dashboard WebSocket**，增加一类 **`messageType: "system.log"`**（或 `op: "log"`）载荷即为 §2 信封。若日志量极大，再拆 `system:log` 子通道并对该通道单独 **rate limit**。

### 4.3 鉴权（Auth）

- 与 Dashboard WS 一致：**建立连接时** 完成会话校验（Cookie / Bearer / 短期 ticket）。
- **日志事件本身不重复携带密钥**；`accountId` 仅服务端在验权后填充，**禁止** 客户端伪造他人 `accountId` 的写入接口。
- 若支持多账户切换，信封中的 `accountId` 必须与当前会话绑定账户一致，否则丢弃并记 `severity: error` 内部事件（不返回给用户）。

### 4.4 速率限制（Rate limits）

| 维度 | 建议 |
|------|------|
| 每连接 egress | 如 **≤ 50 条/秒** 滑动窗口；超出合并为 `system.log_throttled`（`detail.skipped`） |
| 每 `accountId` 全局 | 防止异常循环；与 per-connection 取 min |
| `market_data` | **单独更严**：如 **≤ 5 条/秒 / tokenId**（与采样一致） |

### 4.5 盘口更新采样与摘要规则

1. **时间采样**：同一 `tokenId` 最多每 `T` ms（如 200～1000ms）发一条 `market.book.summary_tick`。
2. **变化阈值**：仅当 `bestBid`/`bestAsk` 或 `spread` 变化超过 `ε` 才允许突破时间窗额外发送一条。
3. **内容**：只发 **best + spread + 可选 top-N 哈希**，不发 levels 全量；N 与哈希算法版本写入 `detail.schemaVersion`。
4. **断连期**：重连后先发一条 `market_sub` + 一条 `market.book.summary_tick`，便于 UI 对齐。

---

## 5. 前端设计

### 5.1 展示形态

- **主视图**：**虚拟化列表**（如 `@tanstack/react-virtual` 或等价），单行高度固定或近似固定，避免大 DOM。
- **辅助**：可选 **表格模式**（列：`ts`、`severity`、`type`、`marketId`、`摘要`）；移动端仍以列表为主。

### 5.2 过滤与检索

- **按 `category`**：多选 chips。
- **按 `severity`**：默认隐藏 `debug`。
- **按文本**：对 `type`、`detail` 的字符串化视图做 debounce 本地过滤；大数据量时改由服务端查询（二期）。

### 5.3 重连横幅（Reconnect banner）

- 监听 Dashboard WS：`onclose` / `onerror` 显示 **非阻塞横幅**（「已断开，正在重连…」）。
- `ws.*.reconnected` 事件到达或 `onopen` 后：**清除横幅**，并可选 **高亮** 自 `lastSeq` 以降的第一条日志，提示「以下为重连后」。

### 5.4 关联 `correlationId`

- 每条日志若存在 `correlationId`，渲染为 **可点击链接**：
  - 打开侧栏「关联追踪」：列出同 ID 的 HTTP（若上报）、WS 帧、后端任务 ID；或
  - 复制到剪贴板 / 深链到内部排障页 `?cid=...`（若已有）。

### 5.5 与风控页的关系

风控页已有 `riskWsBus` / `polyDirectBus`；运行日志应来自 **后端权威事件**（仓位同步、止损队列、订阅管理），与前端直连官方 WS 的日志 **合并展示时需去重**（优先采用服务端 `seq` 与 `correlationId`）。

---

## 6. 实施检查清单

- [ ] 定义并实现稳定 `type` 字符串与 `detail` JSON 约束（可加 JSON Schema 校验）。
- [ ] 实现 ring +（可选）总线；单 writer `seq` 递增。
- [ ] Dashboard WS 增加 `system.log` 类型与限流。
- [ ] 前端虚拟列表 + `category` / `severity` 过滤 + 重连横幅 + `correlationId` 链接。
- [ ] 压测：`market_data` 在高频行情下仍不超过配置带宽。

---

## 7. 相关文档

- [风控页面逻辑梳理](./risk-control-logic.md) — 仓位、订单簿订阅与止损与实时通道的关系。
