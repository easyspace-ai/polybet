# Polybet 止损逻辑与数据流转全面分析

## 一、止损逻辑深度分析

### 1.1 核心架构

止损系统由三个核心组件协作完成：

```
StopLossEngine (协调层) → risksvc (评估层) → polyexec (执行层)
```

### 1.2 移动止损（Trailing Stop）机制

**触发链路**：
```
CLOB WebSocket 推送 → bookcache 更新 → debounce(120ms) → RiskEvaluateTokenAfterBookUpdate
```

**核心逻辑** (`risksvc.go:245-276`):
- **高水位（High Water Mark）**: 使用 `max(bid, ask)` 更新，跟踪持仓期间的最高盘口价格
- **止损线**: `trail = highWater * (1 - stopLossPct/100)`
- **触发价**: 优先使用 `bestBid`（可执行价格），无买盘时 fallback 到 `max(bid, ask)`
- **默认止损**: 20%，支持按价格区间配置 (`priceStopLossRanges`)

**关键设计亮点**:
- 高水位只升不降（`maxCentsRatchet`），确保移动止损只向前推进
- 无买盘时仍能触发止损（fallback 到 ask/mark price），防止流动性枯竭时无法止损
- 120ms 防抖避免频繁评估

### 1.3 平仓任务队列系统

**任务状态机**: `pending → running → succeeded/failed/cancelled`

**重试策略** (`risksvc.go:176-198`):
- 指数退避：`400 * 2^(n-1)` ms，前6次；之后 `2000 * 2^(n-6)` ms
- 最大退避 60 秒
- 每次重试自动增加 extra ticks（最多 +8），逐步放宽限价

**中止条件** (`close_abort.go`):
- `aborted:position_closed` — 持仓已关闭
- `aborted:not_monitored` — 用户标记不再监控
- `aborted:market_ended` — 市场已结束（含 300 秒冷却期防止重复创建任务）

### 1.4 三种平仓执行模式

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| `fok_sell` (默认) | Fill-or-Kill 卖单，限价 = bestBid - extraTicks | 流动性充足时快速平仓 |
| `fak_sell` | Fill-and-Kill 卖单，限价 = 配置的 worstPrice (默认 1¢) | 流动性不足时尽量成交 |
| `hedge_fok_buy` | 买入对手方 token 对冲，原仓位保留 | 无法直接卖出时的对冲退出 |

**FOK Sell 限价策略** (`orders.go:127`):
```go
floor = max(tick, bestBid - extraTicks * tick)
```
重试时 extraTicks 递增，逐步扩大滑点容忍度。

---

## 二、数据流转全流程

### 2.1 市场数据流

```
Gamma API (HTTP, 60min)
    ↓
SyncEngine.Once() → SQLite (markets/outcomes/events)
    ↓
HTTP API → marketsvc.BuildMarketsPayload → Dashboard
```

### 2.2 实时价格流

```
Polymarket CLOB WebSocket
    ↓
MarketStream (handleBook/handlePriceChange/handleBestBidAsk)
    ↓
bookcache.ReplaceBook() (内存缓存)
    ├──→ Hub.BroadcastJSON(polyBookUpdate) → Dashboard
    └──→ debounce(120ms) → risksvc.RiskEvaluateTokenAfterBookUpdate()
            ↓
        更新高水位 + 判断是否触发止损
            ↓
        触发 → InsertRiskTask(close_position)
```

### 2.3 持仓同步流

```
Data API GET /positions (定时 45s + 交易后触发 + 账户切换时)
    ↓
SyncPositionsFromDataAPI()
    ├── 新持仓 → CreateRiskPosition (highWater=entryPrice, stopLoss=resolveStopLossPct)
    ├── 已有持仓 → UpdateRiskPositionSharesCost
    └── 官方无持仓 → CloseRiskPosition (本地关闭)
```

### 2.4 止损执行流

```
riskTicker (3s 轮询)
    ↓
ProcessRiskTasksOnce() → 拉取到期任务 (最多 20 个, 并发 10)
    ↓
runClosePosition() → 按模式执行 (FOK/FAK/Hedge)
    ↓
    ├── 成功 → CloseRiskPosition + CancelOtherCloseTasks + SetRiskTaskSucceeded
    ├── 失败 → SetRiskTaskFailed + 指数退避重试
    └── 中止 → SetRiskTaskCancelled + 设置冷却期
```

### 2.5 定时任务汇总

| 任务 | 间隔 | 功能 |
|------|------|------|
| riskTicker | 3s | 处理风控任务队列 |
| StopLossEngine | 实时 WS | 监听盘口变化 |
| restTradesTicker | 45s | REST 同步持仓 |
| positionsReconcileTicker | 动态 (有持仓更频繁) | Data API 对账 |
| syncTicker | 60min | Gamma 市场同步 |

---

## 三、专业交易员视角的优化建议

### P0 — 关键风险

#### 1. 止损触发价仅依赖 BestBid，在流动性枯竭时可能严重偏离真实成交价

- 当前逻辑：`triggerCents = bidCents > 0 ? bidCents : max(bid, ask)`
- 问题：薄市场中 BestBid 可能只有极小量（如 0.01 shares），触发止损后 FOK 卖单可能无法成交
- 建议：评估时加入**盘口深度检查**，BestBid 的 size 必须 ≥ 持仓 shares 的一定比例才视为有效触发价

#### 2. 高水位使用 max(bid, ask) 但止损触发用 bestBid，逻辑不一致

- `risksvc.go:247`: 高水位用 `max(bid, ask)` 更新
- `risksvc.go:261`: 止损比较用 `stopTriggerReferenceCents(bidCents, askCents)` 即优先 bid
- 问题：ask 飙升会抬高水位，但 bid 可能很低，导致止损线被不合理地抬高
- 建议：高水位也应只用 `bestBid`，或至少在有足够深度时才用 ask 更新

#### 3. 无止损触发前的"预检"机制

- 当前：触发止损 → 入队 → 3 秒后执行 → FOK 可能失败 → 重试
- 问题：在快速下跌行情中，3 秒延迟 + FOK 失败重试可能导致更大的滑点
- 建议：止损触发时立即尝试一次 FOK（不等 3 秒 ticker），失败后再走队列重试

### P1 — 重要改进

#### 4. 止损百分比一刀切，缺乏动态调整

- 当前：默认 20%，支持按价格区间配置
- 建议：
  - 根据**市场剩余时间**动态调整（临近结算应缩窄止损）
  - 根据**波动率**调整（高波动市场放宽止损）
  - 根据**持仓盈亏比**调整（盈利后可收紧保护利润）

#### 5. 缺乏最大亏损保护（Hard Stop）

- 当前只有移动止损，没有基于入场价的固定止损
- 建议：增加 `maxLossPct` 配置，即使高水位未更新，跌破入场价一定比例也强制止损

#### 6. FOK 模式下缺乏阶梯式降价策略

- 当前：FOK 失败后只增加 extraTicks，但下次重试仍是单次 FOK
- 建议：实现**阶梯平仓**（ladder close），分批以不同价格挂 FOK，提高成交概率

#### 7. 对冲模式（hedge_fok_buy）不关闭原持仓

- 当前：对冲成功后原仓位仍在，只是标记为 hidden
- 问题：
  - 对冲成本 + 原持仓成本可能超过直接卖出的损失
  - 对冲后双边持仓锁定资金
- 建议：对冲后计算**综合盈亏**，如果对冲成本过高应报警或拒绝执行

#### 8. 缺乏滑点监控和预警

- 当前：FOK 限价 = bestBid - extraTicks，但不知道实际成交价
- 建议：记录**预期成交价 vs 实际成交价**的偏差，超过阈值时报警

### P2 — 体验优化

#### 9. 仓位同步依赖 Data API 轮询，延迟较高

- 当前：45 秒轮询 + User WS 事件
- 建议：增加 CLOB trade event 的实时处理，成交后立即更新本地持仓

#### 10. 缺乏止盈机制（Take Profit）

- 当前只有止损，没有止盈
- 建议：增加 `takeProfitPct` 或 `takeProfitPrice`，达到目标价自动平仓

#### 11. 任务队列缺乏优先级

- 当前：按 `next_run_at` 排序，止损任务和手动平仓任务同等优先级
- 建议：止损任务应高于手动平仓，紧急止损应插队

#### 12. 缺乏组合风险视角

- 当前：每个持仓独立止损，没有全局风险视图
- 建议：
  - 计算**总仓位风险敞口**
  - 设置**单日最大亏损限额**
  - 关联市场的相关性风险（同一比赛的多個投注）

#### 13. 无市场流动性评分

- 建议：在创建平仓任务前评估市场流动性（买卖价差、深度），流动性过低时：
  - 切换到 FAK 模式
  - 或延迟执行等待流动性恢复
  - 或触发对冲模式

#### 14. 缺少止损触发前的预警

- 建议：当价格接近止损线（如 80% 距离）时发送预警通知，给操作者手动干预的机会

### P3 — 代码质量

#### 15. SQLite 在高并发写入时可能成为瓶颈

- 平仓任务执行 + 持仓同步 + 高水位更新同时写入
- 建议：增加 WAL 模式优化，或考虑批量写入

#### 16. 错误处理中部分路径吞掉了错误

- 多处使用 `_ = s.st.XXX()` 忽略返回值
- 建议：至少记录日志

#### 17. 缺少单元测试覆盖核心路径

- 已有部分测试，但缺少端到端的止损触发→执行→完成的集成测试
