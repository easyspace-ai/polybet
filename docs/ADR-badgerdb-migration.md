# ADR: Polybet 存储层重构 — SQLite → BadgerDB + JSON

> **状态**: Draft
> **日期**: 2026-05-17
> **作者**: AI Assistant
> **影响范围**: `server/internal/store/`, `server/internal/service/risksvc/`, `server/internal/sync/`, `server/internal/app/`

---

## 1. 现状诊断

### 1.1 核心瓶颈

**不是数据量大，而是多 goroutine 抢同一个 SQLite 写锁。**

| 维度 | 现状 | 问题 |
|------|------|------|
| 引擎 | `modernc.org/sqlite`, WAL 模式, `MaxOpenConns=4`, `busy_timeout=10s` | WAL 只改善读写并发，**写事务仍然串行** |
| 风控 ticker | 每 3s `ProcessRiskTasksOnce()`，默认 `closeTaskConcurrency=10` | 10 个并发 close task 同时写 `risk_positions` + `risk_tasks` + `trade_quality` |
| 持仓对账 | ~20s 一次 `SyncPositionsFromDataAPI()`，用 `sync.Mutex` 防重叠 | 可能和风控平仓同时更新同一批 `risk_positions` |
| 市场同步 | `SyncEngine.Once()` 每个市场一个事务 | 市场多时持续占用写通道 |
| trade_quality | 每次成交 INSERT 一行 | 追加型审计数据，和风控状态抢同一个 writer |
| bot_config | 已迁移到 `~/.polybet/bot-settings.json` | 但文档和 schema 仍残留 `bot_config` 表 |

### 1.2 设计不合理之处

#### 1.2.1 过度关系化

Polybet 的风控路径本质上是 **KV 操作**，却用了关系数据库：

- `risk_positions` 和 `risk_position_configs` 被拆成两张表，每次读取都要 `LEFT JOIN` + `COALESCE`。这两个实体生命周期完全一致，没有独立的查询需求。
- `risk_tasks` 用 `next_run_at` 做范围扫描，但实际只需要按时间排序取出前 N 个 pending/failed 任务。
- `risk_applied_clob_trades` 纯粹是去重 KV（tradeID → accountID），用关系表存是杀鸡用牛刀。

#### 1.2.2 账户管理过重

`polymarket_accounts` 表只有 1-3 行数据，却用了完整的 CRUD + 事务 + FK。账户的增删改极其低频（几个月一次），用 SQLite 表存储增加了不必要的复杂度。

#### 1.2.3 trade_quality 不适合关系表

- 纯追加写入，从不 UPDATE/DELETE
- 查询模式只有两种：最近 N 行、按时间窗口聚合
- 有 `RealizedPnLByEvent` 需要 JOIN outcomes→markets→events 解析 slug，但这是离线分析需求，不应该影响热路径
- 数据会随时间无限增长，需要定期清理

#### 1.2.4 市场数据同步路径冗长

Gamma API → `UpsertPolyMarketQuote()` → events/markets/canonical_bets/outcomes 四张表 → `ListActiveMarketsFlat()` 做 JOIN 构建快照 → 广播给 UI。

实际上 UI 只需要一个 **市场快照**，中间的关系结构只是为了"规范化"，但市场数据是只读的、定期全量替换的，规范化没有收益。

#### 1.2.5 配置管理双轨制

- `config.Config` 从环境变量加载（进程级）
- `bot_config` 表在 schema 里但实际不用
- `~/.polybet/bot-settings.json` 是真正的运行时配置
- `wsconfig` 包又从 `bot_config` 读取 WS 相关配置
- `homesettings` 包还有第三套文件配置

三层配置源，开发者和维护者都容易困惑。

#### 1.2.6 无存储抽象层

`store.Store` 直接持有 `*sql.DB`，所有方法都是 SQL 调用。没有接口抽象，无法替换存储引擎，也无法做 mock 测试。

#### 1.2.7 时间字符串解析不一致

SQLite 存的时间格式混用 `datetime('now')` 和 `time.RFC3339Nano`，读取时用 `parseSQLiteTime()` 做兼容解析。迁移到 BadgerDB 后统一用 Unix 纳秒时间戳。

---

## 2. 技术选型

### 2.1 为什么选 BadgerDB v4

| 维度 | BadgerDB v4 | bbolt | Pebble | LMDB |
|------|-------------|-------|--------|------|
| 语言 | 纯 Go | 纯 Go | 纯 Go | C 库 (cgo) |
| 并发写 | ✅ SSI 事务 | ❌ 单写者 | ✅ 但无事务 | ❌ 单写者 |
| ACID | ✅ | ✅ | ❌ | ✅ |
| LSM 结构 | ✅ 高吞吐写入 | ❌ B+树 | ✅ | ❌ |
| 嵌入式 | ✅ | ✅ | ✅ | ✅ |
| 事务隔离 | Snapshot Isolation | 无 | 无 | 快照读 |
| 适合场景 | **多 goroutine 并发写 KV** | 读多写少 | 高吞吐无事务 | 读密集型 |

**核心决策因素**：当前瓶颈是"多个后台并发路径抢同一个 SQLite 写锁"。bbolt 和 LMDB 都是"多读单写"，会把问题换个名字带回来。Pebble 无事务，无法保证"持仓 + 配置 + task index"的原子更新。BadgerDB v4 是唯一同时满足 **纯 Go + 并发 ACID 事务 + LSM 高吞吐写入** 的选择。

### 2.2 BadgerDB v4 关键特性

- **TxnManager**: 支持并发读写事务，使用 Snapshot Isolation
- **LSM-Tree**: 写入先写 WAL 再进 memtable，后台 compact，写入延迟稳定
- **TTL**: 原生支持 key 过期，适合 trade_quality 自动清理
- **Stream**: 高效的批量扫描，适合市场快照构建
- **纯 Go**: 无 cgo 依赖，交叉编译友好

---

## 3. 目标数据模型

### 3.1 Key 编码规范

统一使用 `:` 作为分隔符，前缀按业务域划分：

```
risk/position/{id}                    → 持仓主文档（含 config 合并）
risk/open/{account}/{token}/{side}    → 开放持仓索引 → positionID
risk/closed/{account}/{closedAt}/{id} → 已平仓索引 → positionID
risk/task/{id}                        → 任务主文档
risk/task/due/{nextRun}/{id}          → 待执行任务索引 → taskID
risk/applied/{account}/{tradeID}      → 去重标记（CLOB trade）
risk/hidden/{account}/{token}/{side}  → 隐藏持仓

market/event/{id}                     → 事件文档
market/market/{id}                    → 市场文档
market/outcome/{id}                   → 结果文档
market/active/{startTime}/{id}        → 活跃市场索引
market/outcomeByMarket/{marketID}/{id}→ 市场下的结果索引
market/tokenLookup/{externalID}       → token → outcome 映射

trade/quality/{account}/{ts}/{id}     → 成交质量（追加写）
trade/quality/bucket/{account}/{hour} → 小时聚合桶
trade/quality/bucket/{account}/{day}  → 天聚合桶

account/{id}                          → 账户文档
account/active                        → 活跃账户标记 → accountID

config/bot                            → 全局 bot 配置（单 key，JSON）
config/ws                             → WS 配置（单 key，JSON）
```

### 3.2 数据文档结构

#### 3.2.1 risk/position/{id}

将 `risk_positions` + `risk_position_configs` 合并为一个文档：

```json
{
  "id": "uuid",
  "platform": "polymarket",
  "accountId": "uuid",
  "outcomeId": "uuid|null",
  "tokenId": "hex-string",
  "title": "Will Team A beat Team B?",
  "sideLabel": "YES",
  "polyEventSlug": "nba-2026-lakers-warriors",
  "polyMarketSlug": "nba-2026-lakers-warriors-moneyline",
  "avgEntryCents": 45.0,
  "sizeShares": 100.0,
  "costUsd": 45.0,
  "highWaterCents": 52.0,
  "stopLossPct": 20.0,
  "source": "bot",
  "status": "open",
  "realizedPnlUsd": null,
  "closedAt": null,
  "createdAt": "2026-05-17T10:00:00Z",
  "updatedAt": "2026-05-17T10:05:00Z"
}
```

**设计理由**：
- 原设计分两张表是因为"配置可能不存在"，但实际每个持仓创建时都会写入 config
- 合并后减少一次 JOIN，减少一次事务，减少锁竞争
- `highWaterCents` 和 `stopLossPct` 直接放在主文档里，更新时只改一个 key

#### 3.2.2 risk/open/{account}/{token}/{side} → positionID

```
Key:   risk/open/{accountId}/{tokenId}/{sideLabel}
Value: {positionID}
```

**设计理由**：
- 替代 `idx_risk_positions_open_key` 部分唯一索引
- 用于快速检查"某账户在某 token 上是否已有开放持仓"
- 创建持仓时先查此索引，存在则走 upsert 路径
- 平仓时删除此 key

#### 3.2.3 risk/closed/{account}/{closedAt}/{id} → positionID

```
Key:   risk/closed/{accountId}/{closedAtNano}/{positionID}
Value: {positionID}
```

**设计理由**：
- 用于 kill switch 的 `AccountRealizedPnLSince` 查询
- `closedAtNano` 作为排序键，支持按时间范围扫描
- 替代 `WHERE status='closed' AND closed_at >= ?` 的全表扫描

#### 3.2.4 risk/task/{id}

```json
{
  "id": "uuid",
  "type": "close_position",
  "positionId": "uuid",
  "status": "pending",
  "attempts": 0,
  "lastError": null,
  "reason": "stop_loss",
  "lastAttemptDetail": null,
  "nextRunAt": "2026-05-17T10:03:00Z",
  "createdAt": "2026-05-17T10:00:00Z",
  "updatedAt": "2026-05-17T10:00:00Z"
}
```

#### 3.2.5 risk/task/due/{nextRun}/{id} → taskID

```
Key:   risk/task/due/{nextRunNano}/{taskID}
Value: {taskID}
```

**设计理由**：
- 替代 `WHERE status IN ('pending','failed') AND next_run_at <= now ORDER BY next_run_at`
- 任务状态变更时，删除旧的 due key，写入新的（如果状态变为 pending/failed）
- 扫描时从 `risk/task/due/0` 开始 prefix scan，取到当前时间为止的 key

#### 3.2.6 risk/applied/{account}/{tradeID}

```
Key:   risk/applied/{accountId}/{tradeID}
Value: {"applied": true}
```

**设计理由**：
- 替代 `risk_applied_clob_trades` 表
- 纯去重用途，`INSERT OR IGNORE` 语义
- 可设置 TTL 自动清理（如 30 天后过期）

#### 3.2.7 account/{id}

```json
{
  "id": "uuid",
  "name": "Main Account",
  "apiKey": "...",
  "secret": "...",
  "passphrase": "...",
  "privateKey": "...",
  "funderAddress": "0x...",
  "isActive": true,
  "createdAt": "2026-01-01T00:00:00Z",
  "updatedAt": "2026-01-01T00:00:00Z"
}
```

**设计理由**：
- 账户数据变化极少（月级别），用 JSON 文件存储即可
- 但为了和 BadgerDB 统一访问模式，也放入 BadgerDB
- `account/active` 作为二级索引指向活跃账户

#### 3.2.8 config/bot

```
Key:   config/bot
Value: {"pollingInterval":"60", "maxTradeSize":"100", ...}
```

**设计理由**：
- 替代 `~/.polybet/bot-settings.json`
- 单 key 存整个配置 map，保持现有的 `get/set/list` 语义
- 写入时原子替换整个 value（BadgerDB 事务保证）
- 内存中维护一份副本，读取走内存，写入同时更新内存和磁盘

#### 3.2.9 trade/quality/{account}/{ts}/{id}

```json
{
  "id": "uuid",
  "accountId": "uuid",
  "side": "sell",
  "orderType": "FOK",
  "tokenId": "hex",
  "expectedOdds": 0.45,
  "fillOdds": 0.44,
  "limitOdds": 0.43,
  "bestBid": 0.44,
  "bestAsk": 0.46,
  "slippageBps": 222,
  "size": 100,
  "submitLatencyMs": 45,
  "tradeId": "uuid",
  "riskTaskId": "uuid",
  "notes": "",
  "realizedPnlUsd": -5.0,
  "createdAt": "2026-05-17T10:00:00Z"
}
```

**设计理由**：
- Key 中包含 `accountId` 和 `ts`（纳秒时间戳），支持按账户+时间范围扫描
- 设置 TTL 自动清理（如 90 天）
- 聚合数据维护预计算 bucket，避免每次全量扫描

#### 3.2.10 trade/quality/bucket/{account}/{hour}

```json
{
  "window": "2026-05-17T10:00:00Z",
  "count": 15,
  "avgSlippageBps": 180,
  "maxSlippageBps": 500,
  "buyCount": 8,
  "sellCount": 7,
  "buyAvgBps": 150,
  "sellAvgBps": 210,
  "realizedPnlUsd": -25.0
}
```

**设计理由**：
- 每次写入 trade_quality 时，原子更新对应小时桶
- 查询聚合数据时直接读桶，无需扫描所有质量行
- 天桶由小时桶聚合而成，可延迟计算

#### 3.2.11 market/event/{id}, market/market/{id}, market/outcome/{id}

```json
// market/event/{id}
{
  "id": "uuid",
  "sport": "NBA",
  "league": "NBA",
  "homeTeam": "Lakers",
  "awayTeam": "Warriors",
  "startTime": "2026-05-17T19:00:00Z",
  "status": "active",
  "sxEventId": "",
  "polyEventId": "",
  "polySlug": "nba-2026-lakers-warriors",
  "createdAt": "2026-05-17T08:00:00Z"
}

// market/market/{id}
{
  "id": "uuid",
  "eventId": "uuid",
  "platform": "polymarket",
  "externalId": "12345",
  "startTime": "2026-05-17T19:00:00Z",
  "betType": "1x2",
  "line": null,
  "mainLine": true,
  "status": "active",
  "createdAt": "2026-05-17T08:00:00Z"
}

// market/outcome/{id}
{
  "id": "uuid",
  "marketId": "uuid",
  "label": "Lakers",
  "externalId": "hex-token-id",
  "currentOdds": 0.45,
  "liquidityDepth": 5000.0,
  "liquidityLevels": [...],
  "canonicalBetId": "uuid",
  "lastUpdated": "2026-05-17T10:00:00Z"
}
```

#### 3.2.12 市场二级索引

```
market/active/{startTimeNano}/{marketId}  → {marketId}
market/outcomeByMarket/{marketId}/{outcomeId} → {outcomeId}
market/tokenLookup/{externalId}           → {outcomeId}
```

**设计理由**：
- `market/active` 替代 `WHERE status='active' ORDER BY start_time`
- `market/tokenLookup` 替代 `SELECT id FROM outcomes WHERE external_id = ?`
- 市场同步时批量重建这些索引

---

## 4. 存储接口设计

### 4.1 核心接口

```go
// Storage 是存储层的统一抽象，屏蔽 BadgerDB 和 SQLite 的差异。
type Storage interface {
    // 生命周期
    Close() error

    // 事务
    Update(fn func(Tx) error) error
    View(fn func(Tx) error) error

    // KV 操作（底层）
    Get(key []byte) ([]byte, error)
    Set(key, value []byte) error
    Delete(key []byte) error
    Exists(key []byte) (bool, error)

    // 范围扫描
    Scan(prefix []byte, fn func(key, value []byte) error) error
    ScanRange(start, end []byte, fn func(key, value []byte) error) error
}

// Tx 是事务接口，在 Update/View 回调中使用。
type Tx interface {
    Get(key []byte) ([]byte, error)
    Set(key, value []byte) error
    Delete(key []byte) error
    Exists(key []byte) (bool, error)
}
```

### 4.2 业务接口（按域拆分）

```go
// RiskStore 风控相关操作。
type RiskStore interface {
    // 持仓
    GetPosition(ctx context.Context, id string) (*RiskPosition, error)
    ListOpenPositions(ctx context.Context, accountID string) ([]RiskPosition, error)
    ListOpenOrClosingPositions(ctx context.Context, accountID string) ([]RiskPosition, error)
    ListOpenPositionsByToken(ctx context.Context, tokenID, accountID string) ([]RiskPosition, error)
    ListOpenPositionsMinShares(ctx context.Context, accountID string, minShares float64) ([]RiskPosition, error)
    CountOpenPositionsMinShares(ctx context.Context, accountID string, minShares float64) (int64, error)
    ListOpenPositionTokenIDs(ctx context.Context) ([]string, error)
    ListOpenPositionTokenIDsForAccount(ctx context.Context, accountID string) ([]string, error)

    // 创建/更新（原子操作）
    UpsertPosition(ctx context.Context, p *RiskPosition) error
    UpdatePositionStatus(ctx context.Context, id, status string) error
    UpdatePositionSharesCost(ctx context.Context, id string, shares, cost float64) error
    ClosePosition(ctx context.Context, id string, realizedPnLUSD float64) error
    UpdateHighWater(ctx context.Context, id string, hw float64) error
    UpdateStopLoss(ctx context.Context, id string, stopLossPct *float64, highWaterCents *float64) error
    UpdatePolySlugs(ctx context.Context, id, eventSlug, marketSlug string) error
    NormalizeDust(ctx context.Context, dust float64) error

    // 聚合
    AccountOpenExposure(ctx context.Context, accountID string) (float64, error)
    AccountRealizedPnLSince(ctx context.Context, accountID string, since time.Time) (float64, error)
    MarketOpenExposure(ctx context.Context, accountID, polyEventSlug string) (float64, error)

    // 任务
    InsertTask(ctx context.Context, t *RiskTask) error
    ListDueTasks(ctx context.Context, limit int) ([]RiskTask, error)
    ListRecentTasks(ctx context.Context, limit int) ([]RiskTask, error)
    ListTasksByReason(ctx context.Context, taskType, reason string, limit int) ([]RiskTask, error)
    FindPendingCloseTask(ctx context.Context, positionID string) (bool, error)
    SetTaskRunning(ctx context.Context, id string) error
    SetTaskFailed(ctx context.Context, id string, attempts int, lastErr string, nextRun time.Time) error
    SetTaskSucceeded(ctx context.Context, id string) error
    SetTaskCancelled(ctx context.Context, id, reason string) error
    CancelOtherCloseTasks(ctx context.Context, positionID, exceptTaskID string) error
    UpdateTaskAttemptDetail(ctx context.Context, id, detailJSON string) error
    DeleteTerminalTasks(ctx context.Context) (int64, error)

    // 去重
    MarkAppliedTrade(ctx context.Context, accountID, tradeID string) (bool, error)

    // 隐藏
    UpsertHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error
    ListHiddenPositions(ctx context.Context, accountID string) ([]HiddenPosition, error)
    DeleteHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error
}

// MarketStore 市场数据相关操作。
type MarketStore interface {
    // 事件
    GetEvent(ctx context.Context, id string) (*Event, error)
    UpsertEvent(ctx context.Context, e *Event) error

    // 市场
    GetMarket(ctx context.Context, id string) (*Market, error)
    UpsertMarket(ctx context.Context, m *Market) error
    ListActiveMarkets(ctx context.Context) ([]Market, error)

    // 结果
    GetOutcome(ctx context.Context, id string) (*Outcome, error)
    UpsertOutcome(ctx context.Context, o *Outcome) error
    ListOutcomesByMarket(ctx context.Context, marketID string) ([]Outcome, error)
    FindOutcomeByToken(ctx context.Context, tokenID string) (*Outcome, error)

    // 快照（替代 ListActiveMarketsFlat 的 JOIN）
    GetActiveMarketsSnapshot(ctx context.Context) ([]MarketRow, []OutcomeRow, error)

    // 索引
    PolyEventSlugForToken(ctx context.Context, tokenID string) string
}

// AccountStore 账户管理。
type AccountStore interface {
    GetAccount(ctx context.Context, id string) (*PolymarketAccount, error)
    GetActiveAccount(ctx context.Context) (*PolymarketAccount, error)
    GetSingletonAccount(ctx context.Context) (*PolymarketAccount, error)
    ListAccounts(ctx context.Context) ([]PolymarketAccount, error)
    InsertAccount(ctx context.Context, a *PolymarketAccount) error
    ActivateAccount(ctx context.Context, id string) error
    DeactivateAllAccounts(ctx context.Context) error
    DeleteAccount(ctx context.Context, id string) error
    CountAccounts(ctx context.Context) (int, error)
}

// ConfigStore 配置管理。
type ConfigStore interface {
    GetConfig(ctx context.Context, key string) (string, bool, error)
    GetConfigFloat(ctx context.Context, key string, def float64) float64
    GetConfigInt(ctx context.Context, key string, def int) int
    SetConfig(ctx context.Context, key, value string) error
    InsertConfigDefault(ctx context.Context, key, value string) error
    ListConfig(ctx context.Context) ([]struct{ Key, Value string }, error)
    SeedDefaults(ctx context.Context) error
}

// TradeQualityStore 成交质量。
type TradeQualityStore interface {
    Insert(ctx context.Context, q *TradeQuality) error
    ListRecent(ctx context.Context, accountID string, limit int) ([]TradeQuality, error)
    Aggregate(ctx context.Context, accountID string, since time.Time) (TradeQualityAggregate, error)
    RealizedPnLByEvent(ctx context.Context, accountID string, since time.Time, limit int) ([]EventRealizedPnL, error)
}

// TeamAliasStore 队伍别名。
type TeamAliasStore interface {
    UpsertAlias(ctx context.Context, alias *TeamAlias) error
    FindCanonical(ctx context.Context, platform, alias, league string) (string, error)
}
```

### 4.3 实现结构

```
server/internal/storage/
├── storage.go          # Storage 接口 + BadgerDB 实现
├── tx.go               # Tx 接口 + BadgerDB txn 包装
├── key.go              # Key 编码/解码辅助函数
├── encode.go           # JSON 序列化辅助
├── risk.go             # RiskStore 实现
├── market.go           # MarketStore 实现
├── account.go          # AccountStore 实现
├── config.go           # ConfigStore 实现
├── trade_quality.go    # TradeQualityStore 实现
└── team_alias.go       # TeamAliasStore 实现

server/internal/store/  # 保留，作为 SQLite 实现（双写期）
```

---

## 5. 迁移路径

### Phase 0: 接口抽象层（1-2 天）

**目标**: 不动任何业务逻辑，只在 `store.Store` 外面包一层接口。

1. 定义 `Storage` 接口（如上 4.1 节）
2. 定义各业务域接口（如上 4.2 节）
3. 创建 `SQLiteStorage` 实现，内部委托给现有 `store.Store` 方法
4. 修改 `app.App` 结构体，将 `Store *store.Store` 改为 `Storage Storage`
5. 所有 service 层改为依赖接口而非具体实现

**验证**: 所有现有测试通过，行为无变化。

### Phase 1: BadgerDB 基础设施（1-2 天）

**目标**: BadgerDB 可以正常运行，但还不替换任何数据。

1. 添加 `github.com/dgraph-io/badger/v4` 依赖
2. 实现 `BadgerStorage` 结构体：
   - `Open(path string) (*BadgerStorage, error)`
   - `Close() error`
   - `Update(fn) / View(fn)` 事务封装
   - Key 编码/解码辅助
   - JSON 序列化辅助
3. 编写 BadgerDB 基础测试：
   - 并发读写测试
   - 事务原子性测试
   - 崩溃恢复测试

**验证**: BadgerDB 可以独立运行，通过基础测试。

### Phase 2: 配置 + 账户迁移（1-2 天）

**目标**: 把变化最少的数据先迁过来，验证读写路径。

1. 实现 `ConfigStore` 的 BadgerDB 版本：
   - `config/bot` 单 key 存整个 map
   - 内存缓存 + 原子写入
   - 从 `~/.polybet/bot-settings.json` 一次性导入
2. 实现 `AccountStore` 的 BadgerDB 版本：
   - `account/{id}` 存 JSON 文档
   - `account/active` 存活跃账户 ID
   - 从 SQLite 一次性导入
3. 在 `app.New()` 中同时打开 SQLite 和 BadgerDB
4. 配置和账户读取走 BadgerDB，写入双写

**验证**: 配置和账户的读写都走 BadgerDB，SQLite 作为备份仍然可查。

### Phase 3: 市场快照迁移（2-3 天）

**目标**: Gamma 同步直接写 BadgerDB，UI 读内存快照。

1. 实现 `MarketStore` 的 BadgerDB 版本：
   - `market/event/{id}`, `market/market/{id}`, `market/outcome/{id}` 存 JSON
   - `market/active/{start}/{id}` 二级索引
   - `market/tokenLookup/{externalId}` 快速查找
2. 修改 `SyncEngine.Once()`:
   - 写入 BadgerDB
   - 同时写入 SQLite（双写）
   - 直接在内存中构建 `marketsSnapshot`
3. 修改 `marketsvc.BuildMarketsPayload()`:
   - 优先从内存快照读取
   - fallback 到 BadgerDB 扫描
4. `ListActiveMarketsFlat` 改为从 BadgerDB 扫描

**验证**: 市场同步正常工作，UI 能收到快照，SQLite 仍有数据。

### Phase 4: 风控热路径迁移（3-5 天）⭐ 最关键

**目标**: 把最可能立刻变快的部分迁到 BadgerDB。

1. 实现 `RiskStore` 的 BadgerDB 版本：
   - `risk/position/{id}` 合并文档
   - `risk/open/{account}/{token}/{side}` 索引
   - `risk/closed/{account}/{closedAt}/{id}` 索引
   - `risk/task/{id}` + `risk/task/due/{nextRun}/{id}`
   - `risk/applied/{account}/{tradeID}` 去重
2. 关键操作的原子性保证：
   - `UpsertPosition`: 在单个 BadgerDB Txn 中完成 position + open index + config
   - `ClosePosition`: 在单个 Txn 中完成 position 更新 + open index 删除 + closed index 创建
   - `UpdateHighWater`: 直接更新 position 文档的 `highWaterCents` 字段
3. 修改 `risksvc.Service`:
   - 所有风控操作走 `RiskStore` 接口
   - 双写 SQLite（保留一段时间）
4. 并发测试：
   - 10 个 goroutine 同时 close position
   - 验证无数据竞争、无丢失更新

**验证**: 风控路径性能显著提升，SQLite 写锁竞争消失。

### Phase 5: trade_quality 迁移（2-3 天）

**目标**: 追加型数据走 BadgerDB + 预聚合桶。

1. 实现 `TradeQualityStore` 的 BadgerDB 版本：
   - `trade/quality/{account}/{ts}/{id}` 存 JSON
   - `trade/quality/bucket/{account}/{hour}` 预聚合
   - TTL 设置（90 天自动清理）
2. 聚合查询优化：
   - `Aggregate()` 直接读小时桶
   - `RealizedPnLByEvent()` 需要关联 market slug，暂时保留 SQLite JOIN 或维护 `trade/quality/eventPnL/{account}/{slug}` 索引
3. 从 SQLite 导入历史数据

**验证**: 成交质量记录正常，聚合查询结果一致。

### Phase 6: 清理 SQLite（1-2 天）

**目标**: 确认无回退需求后，删除 SQLite 依赖。

1. 移除所有双写逻辑
2. 移除 `store.Store` 和所有 SQLite 相关代码
3. 删除 `server/internal/db/` 目录
4. 删除 migration 文件
5. 更新文档

**验证**: 项目不再依赖任何 SQL 库，纯 BadgerDB 运行。

---

## 6. 风险清单

### 6.1 高风险

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| BadgerDB 数据损坏 | 所有数据丢失 | 每次写入后调用 `db.Sync()`；定期备份数据目录 |
| 迁移过程中数据不一致 | 双写期间 SQLite 和 BadgerDB 数据不同步 | 双写期间以 BadgerDB 为准，SQLite 仅做备份；迁移完成后对比校验 |
| 并发事务冲突 | BadgerDB SSI 事务冲突导致 retry | 实现指数退避重试；监控冲突率 |
| 内存占用增加 | BadgerDB 的 memtable + block cache 占用内存 | 配置 `badger.Options` 限制内存：`WithValueLogFileSize`、`WithMemTableSize` |

### 6.2 中风险

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Key 设计不合理导致扫描慢 | 市场快照构建变慢 | Phase 3 后做性能基准测试 |
| trade_quality 聚合不准确 | 预聚合桶和原始数据不一致 | 每次写入在同一个 Txn 中更新桶；定期校验 |
| 崩溃恢复后索引不一致 | open/closed 索引和主文档不同步 | 启动时做一致性检查，修复不一致 |
| BadgerDB v4 API 变化 | 升级时 API 不兼容 | 锁定版本号，编写适配层 |

### 6.3 低风险

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 磁盘空间增长 | BadgerDB 的 value log 文件增长 | 定期 `RunValueLogGC()`；配置 `WithDiscardRatio` |
| 冷启动慢 | BadgerDB 需要 replay WAL | 正常场景 < 1s；可接受 |
| 交叉编译体积增加 | BadgerDB 增加二进制大小 | 纯 Go 依赖，增加约 5MB，可接受 |

---

## 7. 性能预期

### 7.1 写入性能

| 操作 | SQLite (当前) | BadgerDB (预期) | 改善 |
|------|--------------|-----------------|------|
| 单条 position 更新 | ~2-5ms (可能阻塞) | ~0.5-1ms | 2-5x |
| 10 并发 close task | ~50-200ms (串行) | ~5-10ms (并行) | 10-20x |
| trade_quality 插入 | ~1-3ms | ~0.3-0.8ms | 3-5x |
| 市场同步 (100 markets) | ~500ms-2s | ~100-300ms | 2-5x |

### 7.2 读取性能

| 操作 | SQLite (当前) | BadgerDB (预期) | 改善 |
|------|--------------|-----------------|------|
| 获取单个 position | ~1ms | ~0.2ms | 5x |
| 扫描所有 open positions | ~5-10ms | ~1-3ms | 3-5x |
| 扫描 due tasks | ~2-5ms | ~0.5-1ms | 4-10x |
| 市场快照构建 | ~50-200ms (JOIN) | ~10-30ms (prefix scan) | 3-7x |

### 7.3 内存占用

| 组件 | SQLite | BadgerDB | 差异 |
|------|--------|----------|------|
| 连接池 | ~4MB (4 connections) | ~0 | -4MB |
| Block cache | ~0 | ~64MB (可配置) | +64MB |
| Memtable | ~0 | ~64MB (可配置) | +64MB |
| 内存缓存 (现有) | ~10MB | ~10MB | 不变 |
| **总计** | ~14MB | ~138MB | +124MB |

对于交易机器人场景，128MB 的额外内存占用是可以接受的。可以通过 `WithMemTableSize(32<<20)` 和 `WithBlockSize(4<<20)` 进一步降低。

---

## 8. 配置变更

### 8.1 新增配置项

```go
type BadgerConfig struct {
    DataDir         string        // BadgerDB 数据目录，默认 ~/.polybet/badger
    MemTableSize    int           // memtable 大小，默认 64MB
    BlockCacheSize  int           // block cache 大小，默认 64MB
    ValueLogFileSize int          // value log 文件大小，默认 256MB
    SyncWrites      bool          // 每次写入后 sync，默认 true（安全优先）
    GCInterval      time.Duration // ValueLog GC 间隔，默认 1h
    TradeQualityTTL time.Duration // trade_quality 过期时间，默认 90d
}
```

### 8.2 环境变量

```
POLYBET_BADGER_DIR=~/.polybet/badger
POLYBET_BADGER_SYNC_WRITES=true
POLYBET_BADGER_MEM_TABLE_SIZE=67108864
POLYBET_TRADE_QUALITY_TTL=7776000  # 90 days in seconds
```

---

## 9. 启动时一致性检查

BadgerDB 启动后执行以下检查：

```go
func (s *BadgerStorage) CheckConsistency(ctx context.Context) error {
    // 1. 检查 open index 和 position 文档的一致性
    //    每个 risk/open/{account}/{token}/{side} 指向的 position 必须存在且 status=open/closing
    //    每个 status=open/closing 的 position 必须有对应的 open index

    // 2. 检查 due task index 和 task 文档的一致性
    //    每个 risk/task/due/{nextRun}/{id} 指向的 task 必须存在且 status=pending/failed
    //    每个 status=pending/failed 的 task 必须有对应的 due index

    // 3. 检查 closed index 和 position 文档的一致性
    //    每个 risk/closed/{account}/{closedAt}/{id} 指向的 position 必须存在且 status=closed

    // 4. 检查 trade_quality bucket 和原始数据的一致性（抽样检查）
    //    随机抽取几个小时桶，重新计算聚合值对比

    return nil
}
```

---

## 10. 回滚方案

如果迁移过程中发现不可接受的问题：

1. **Phase 0-1**: 无数据变更，直接回滚代码
2. **Phase 2-3**: 配置和市场数据双写期间，SQLite 仍是权威源，可切回
3. **Phase 4**: 风控数据双写期间，保留 SQLite 完整数据，可切回
4. **Phase 5**: trade_quality 双写期间，SQLite 仍有完整历史
5. **Phase 6**: 已删除 SQLite，无法回滚（此阶段前必须充分验证）

---

## 11. 测试策略

### 11.1 单元测试

- 每个 Store 方法的单元测试
- Key 编码/解码测试
- JSON 序列化/反序列化测试
- 事务原子性测试

### 11.2 集成测试

- 并发写入测试（模拟 10 个 close task 同时执行）
- 崩溃恢复测试（写入中途 kill 进程，重启后检查一致性）
- 双写一致性测试（SQLite 和 BadgerDB 数据对比）

### 11.3 性能基准

```go
func BenchmarkRiskPositionUpsert(b *testing.B)
func BenchmarkListOpenPositions(b *testing.B)
func BenchmarkListDueTasks(b *testing.B)
func BenchmarkTradeQualityInsert(b *testing.B)
func BenchmarkMarketSnapshot(b *testing.B)
```

### 11.4 压力测试

- 模拟生产环境负载运行 24 小时
- 监控内存、磁盘、GC 暂停
- 对比 SQLite 和 BadgerDB 的写锁等待时间

---

## 12. 监控指标

```go
type Metrics struct {
    // BadgerDB 内部指标
    BadgerMemtableSize    int64
    BadgerBlockCacheHits  int64
    BadgerBlockCacheMiss  int64
    BadgerLSMGets         int64
    BadgerLSMBloomHits    int64
    BadgerTxnConflict     int64  // 事务冲突次数

    // 业务指标
    RiskPositionUpsertMs  float64  // position upsert 延迟
    RiskTaskListMs        float64  // due task 扫描延迟
    TradeQualityInsertMs  float64  // quality 插入延迟
    MarketSnapshotMs      float64  // 快照构建延迟

    // 一致性指标
    OpenIndexMismatch     int64    // open index 不一致数量
    DueIndexMismatch      int64    // due index 不一致数量
}
```

---

## 13. 时间估算

| Phase | 内容 | 预估时间 |
|-------|------|----------|
| Phase 0 | 接口抽象层 | 1-2 天 |
| Phase 1 | BadgerDB 基础设施 | 1-2 天 |
| Phase 2 | 配置 + 账户迁移 | 1-2 天 |
| Phase 3 | 市场快照迁移 | 2-3 天 |
| Phase 4 | 风控热路径迁移 | 3-5 天 |
| Phase 5 | trade_quality 迁移 | 2-3 天 |
| Phase 6 | 清理 SQLite | 1-2 天 |
| **总计** | | **11-19 天** |

---

## 14. 总结

当前架构的根本问题是**用关系数据库解决 KV 问题**。Polybet 的风控路径本质上是高并发的 KV 操作（持仓状态更新、任务队列、去重标记），却被迫在 SQLite 的写锁下排队。

BadgerDB v4 提供了：
1. **并发 ACID 事务**: 多个 goroutine 可以同时写不同的 key，不会互相阻塞
2. **LSM 结构**: 写入延迟稳定，不会因 compact 操作突然变慢
3. **纯 Go**: 无 cgo 依赖，部署简单
4. **TTL**: 自动清理过期数据，适合 trade_quality
5. **嵌入式**: 无需额外进程，和现有架构兼容

迁移策略采用**渐进式双写**，每个阶段都可以独立验证和回滚，最大程度降低风险。
