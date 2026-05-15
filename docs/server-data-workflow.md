# Server 数据工作流文档

## 概述

Polybet Server 是一个基于 Go 的后端服务，负责与 Polymarket 进行交互，管理市场数据、交易执行、持仓风险和止损逻辑。

---

## 核心模块架构

### 1. 入口与启动 (cmd/server/main.go → internal/app/app.go)

**启动流程：**
1. 加载配置文件 (`config.Load`, `config.LoadEnvFile`)
2. 初始化数据库连接 (`db.Open`)
3. 创建 App 实例，包含所有核心组件
4. 启动后台 goroutine：
   - InitService - 初始化检查
   - SyncEngine.Once(ctx, force) - 启动阶段市场同步（force=false）
   - riskTicker (3秒) - 风险任务处理
   - syncTicker (可配置,默认30秒) - 定时市场同步
   - restTradesTicker (45秒) - REST交易同步
   - StopLoss.Run - 止损引擎
   - polyUserWSLoop - 用户WebSocket循环
   - Telegram通知

---

### 2. HTTP 服务器 (internal/httpserver)

**主要端点：**
- `GET /api/health` - 健康检查
- `GET /api/balances` - 余额查询
- `GET /api/config` - 配置查询
- `PUT /api/config/:key` - 配置更新
- `GET/POST /api/markets` - 市场数据
- `GET /api/sports` - 体育项目
- `POST /api/trade/preview` - 交易预览
- `POST /api/trade/execute` - 交易执行
- `GET /api/trades` - 交易历史
- `GET/POST /api/accounts` - 账户管理
- `GET /api/risk/positions` - 风险持仓
- `POST /api/risk/refresh` - 风险刷新
- `POST /api/risk/position/:id/close` - 平仓
- `POST /api/risk/close-all` - 一键平仓

---

### 3. 市场数据流 (internal/sync)

**数据路径：**

```
Gamma API (events)
       ↓
   Engine.Once()
       ↓
parseTagListJSON() → 读取 eventClassificationTags 配置
       ↓
leaguesFromTags() → 映射到 Polymarket series_id
       ↓
fetchGammaEvents() → GET https://gamma-api.polymarket.com/events
       ↓
quoteFromMoneyline12() → 解析 moneyline 市场
       ↓
store.UpsertPolyMarketQuote() → 写入 SQLite
       ↓
/api/markets 读取 → marketsvc.BuildMarketsPayload
```

**关键配置：**
- `eventClassificationTags` - JSON 数组，如 `["nba","nhl"]`
- `pollingInterval` - 市场（Gamma）同步轮询间隔，单位 **分钟**，默认 **60**（即每小时）

---

### 4. 数据存储 (internal/store)

**核心表结构：**

| 表名 | 用途 |
|------|------|
| `markets` | 市场信息 |
| `outcomes` | 投注选项 |
| `events` | 事件/比赛 |
| `trades` | 交易记录 |
| `risk_positions` | 持仓/风险 |
| `risk_position_configs` | 持仓配置(止损/高水位) |
| `risk_tasks` | 风控任务 |
| `risk_applied_clob_trades` | 已应用交易去重 |
| `bot_config` | 系统配置 |
| `polymarket_accounts` | Polymarket 账户 |
| `risk_hidden_positions` | 隐藏持仓 |

**关键查询：**
- `ListActiveMarketsFlat` - 列出活跃市场+选项
- `ListTrades` - 分页交易列表
- `ListOpenRiskPositionsMinShares` - 持仓查询
- `ListDueRiskTasks` - 待处理风控任务

---

### 5. 订单簿缓存 (internal/bookcache)

**功能：**
- 内存缓存订单簿数据
- 支持多个 token 的深度数据
- 提供最优买卖价查询

**数据来源：**
1. StopLossEngine WebSocket 实时推送
2. HTTP REST 轮询 (polymarket.com/book)

---

### 6. 风险管理服务 (internal/service/risksvc)

**核心功能：**

#### 6.1 持仓同步 (SyncPositionsFromDataAPI)
```
Data API (Positions)
       ↓
遍历官方持仓
       ↓
对比本地 risk_positions
  - 新增 → CreateRiskPosition (高水位=入场价)
  - 已有 → UpdateRiskPositionSharesCost
  - 不存在 → CloseRiskPosition
       ↓
更新数据库
```

#### 6.2 风险任务处理 (ProcessRiskTasksOnce)
- **close_position**: 单持仓平仓 (FOK Sell)
- **close_all**: 一键平仓 (为所有持仓创建 close_position 任务)
- 并发控制：`closeTaskConcurrency` (默认10)

#### 6.3 止损评估 (RiskEvaluateTokenAfterBookUpdate)
```
订单簿更新
       ↓
debounce (120ms)
       ↓
查询 token 对应的持仓
       ↓
UpdateHighWaterAndMaybeQueueStop:
  - 如果当前价 > 高水位 → 更新高水位
  - 如果当前价 <= 跟踪止损线 → 创建平仓任务
       ↓
触发 Telegram 通知
```

---

### 7. 止损引擎 (internal/stoplossengine)

**功能：** 实时监控持仓价格，自动触发止损

**数据流：**
```
Market WebSocket (CLOB)
       ↓
marketstream.MarketStream
       ↓
handleBook/handlePriceChange/handleBestBidAsk
       ↓
bookcache.ReplaceBook() → 更新内存缓存
       ↓
debounce.Trigger() → 120ms 防抖
       ↓
risksvc.RiskEvaluateTokenAfterBookUpdate()
       ↓
如触发止损 → 创建 risk_tasks
```

**订阅管理：**
- 根据 `ListOpenRiskPositionsMinShares` 动态订阅
- 最多并发订阅50个 token
- 账户切换时自动重建订阅

---

### 8. 缓存系统 (internal/memcache)

进程内 **`BalanceCache` / `RiskCache`**（`sync.RWMutex` + TTL，无 Rediska）：

**BalanceCache:**
- TTL: 1小时（内存快照）
- 异步刷新；`rebuild` 路径上对余额拉取 **120ms 防抖** 后广播 `balance_update`
- 首次 `GetWithRefresh` 可走同步拉取并回填缓存

**RiskCache:**
- TTL: 1小时
- 存储持仓列表 + 元数据
- 支持 `GetWithRefresh` 自动回填

详见 [data-layer.md](./data-layer.md)。

---

### 9. 交易服务 (internal/service/tradesvc)

**执行流程 (ExecutePlan):**
```
POST /api/trade/execute
       ↓
routersvc.BuildAllocationPlan() → 路由分配
       ↓
polysession.ResolveAuthedCLOB() → 获取认证客户端
       ↓
遍历 Allocations:
  1. 创建 pending trade 记录
  2. polyexec.ExecuteFOKBuy() → 执行 FOK 买单
  3. 更新 trade 状态 (filled/failed)
  4. 触发 Telegram 通知
  5. 异步刷新订单簿缓存
  6. 异步同步持仓 (仅买入)
       ↓
返回 TradeResponse
```

---

### 10. 初始化服务 (internal/service/initsvc)

**启动检查流程：**
1. **ConfigCheck** - 验证必填配置
2. **ProxyCheck** - 检测代理/地理封锁
3. **BalanceCache** - 预加载余额
4. **PositionCache** - 预加载持仓

---

### 11. WebSocket 中继 (internal/wsrelay)

**广播消息类型：**
- `marketsSnapshot` - 市场快照
- `polyBookUpdate` - 订单簿更新
- `poly_status` - 连接状态
- `position_update` - 持仓更新

---

## 数据流向图

### 市场数据流
```
[Gamma API] → [SyncEngine.Once(force)] → [Store.UpsertPolyMarketQuote] → [SQLite]
                                                                        ↓
[HTTP API] ← [marketsvc.BuildMarketsPayload] ← [Store.ListActiveMarketsFlat]
```

### 交易执行流
```
[Client] → [Handler.handleTradeExecute] → [routersvc.BuildAllocationPlan]
                                                      ↓
                                          [tradesvc.ExecutePlan]
                                                      ↓
                              [polyexec.ExecuteFOKBuy] → [CLOB API]
                                                      ↓
                                          [Store.MarkTradeFilled]
                                                      ↓
                              [risksvc.SyncPositionsFromDataAPI]
```

### 持仓/风控流
```
[Data API Positions] → [risksvc.SyncPositionsFromDataAPI] → [Store.risk_positions]
                                                                        ↓
[StopLossEngine WS] → [bookcache] → [debounce] → [risksvc.RiskEvaluateTokenAfterBookUpdate]
                                                                        ↓
                                                        [Store.InsertRiskTask (close_position)]
                                                                        ↓
[risksvc.ProcessRiskTasksOnce] → [polyexec.ExecuteFOKSell] → [CLOB API]
                                                                        ↓
                                                    [Store.CloseRiskPosition]
```

### 实时价格流
```
[Market WebSocket] → [marketstream] → [bookcache.ReplaceBook]
                                            ↓
                                  [Hub.BroadcastJSON (polyBookUpdate)]
                                            ↓
                                  [debounce.Trigger]
                                            ↓
                                  [risksvc.RiskEvaluateTokenAfterBookUpdate]
```

---

## 关键配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `pollingInterval` | 60（分钟） | 市场（Gamma）同步间隔，默认 60 分钟 = 每小时 |
| `minOpenRiskShares` | 1 | 最小持仓阈值 |
| `polymarketFokBuyExtraTicks` | 5 | 买单滑点容忍 |
| `polymarketFokSellExtraTicks` | 5 | 卖单滑点容忍 |
| `closeTaskConcurrency` | 10 | 平仓任务并发数 |
| `closeAllConcurrency` | 20 | 一键平仓并发数 |
| `eventClassificationTags` | - | 关注的联赛标签 |

---

## 定时任务汇总

| 任务 | 间隔 | 功能 |
|------|------|------|
| riskTicker | 3s | 处理风控任务 |
| syncTicker | 可配（`pollingInterval`，**分钟**，默认 60） | 同步市场数据 |
| restTradesTicker | 45s | 从REST同步持仓 |
| StopLoss.Run | 实时 | 监听价格/触发止损 |
| InitService.Run | 启动时 | 初始化检查 |

---

## 数据库关键操作

### 交易记录
- `CreatePendingTrade` → 创建待处理交易
- `MarkTradeFilled` → 标记成交
- `MarkTradeFailed` → 标记失败
- `ListTrades` → 分页查询

### 持仓管理
- `CreateRiskPosition` → 新建持仓
- `GetOpenRiskPositionByToken` → 按token查询
- `UpdateRiskPositionSharesCost` → 更新份额
- `CloseRiskPosition` → 关闭持仓
- `UpdateRiskPositionHighWater` → 更新高水位

### 风控任务
- `InsertRiskTask` → 插入任务
- `ListDueRiskTasks` → 查询到期任务
- `SetRiskTaskRunning/Failed/Succeeded` → 任务状态更新