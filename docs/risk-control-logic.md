# 风控页面逻辑梳理文档

## 1. 架构概览
风控系统采用前后端分离架构，通过 WebSocket 实现实时的持仓监控、盘口数据更新及止损触发。

### 1.1 前端架构
- **路由**: `/risk` ([risk.tsx](file:///Users/leven/space/leven/polybet/apps/dashboard/src/routes/risk.tsx))
- **数据流控制**: [useRiskControlCache.ts](file:///Users/leven/space/leven/polybet/apps/dashboard/src/hooks/useRiskControlCache.ts) 维护全局单例缓存。
- **实时通信**: 
    - `riskWsBus`: 监听后端推送的仓位变更及状态。
    - `polyDirectBus`: 直接订阅 Polymarket 官方 WebSocket 获取盘口。

### 1.2 后端架构
- **服务层**: [risksvc](file:///Users/leven/space/leven/polybet/server/internal/service/risksvc/risksvc.go) 处理核心逻辑。
- **持久化**: SQLite 存储 `risk_positions` 和 `risk_position_configs`（止损配置）。
- **同步机制**: `SyncPositionsFromDataAPI` 定期从官方同步持仓。

---

## 2. 核心逻辑处理

### 2.1 仓位展示与去重
- **逻辑**: 前后端均实现了基于 `tokenId` + `sideLabel` (Yes/No) 的去重。
- **修复说明**: 
    - 后端在 `ListRiskPositionsEnriched` 中增加了强校验，防止数据库中出现冗余记录时影响前端显示。
    - 前端优化了 `posMap` 的合并逻辑，并统一了 `tokenId` 的标准化处理。

### 2.2 实时订单簿 (Orderbook) 数据获取
- **双保险订阅**: 
    - 系统会为每一个处于 `open` 状态的仓位自动订阅盘口。
    - 订阅路径 1: 前端 -> 后端中转 -> 官方 WS。
    - 订阅路径 2: 前端 -> 官方 WS 直连。
- **修复说明**: 
    - 修复了新开仓位时 `useEffect` 依赖项未正确触发订阅的问题。
    - 确保了 `openTokens` 的计算使用了标准化后的 ID，防止订阅冲突。

### 2.3 订单簿档位显示
- **排序规则**:
    - **Bids (买盘)**: 按价格从高到低排序，显示离盘口最近的 5 档。
    - **Asks (卖盘)**: 按价格从低到高排序，显示离盘口最近的 5 档。
- **修复说明**: 
    - 在 `handlePolyBook` 中增加了显式的 `sort` 和 `slice(0, 5)` 操作，确保 UI 展示的准确性。

---

## 3. 止损逻辑
- **最高水位 (High Water Mark)**: 系统记录持仓期间出现的最高买一价。
- **移动止损价**: `最高水位 * (1 - 止损%)`。
- **触发机制**: 
    - 后端实时监控盘口，一旦价格破位立即插入 `close_position` 任务到队列。
    - 前端在 `updatePositionsFromBook` 中进行冗余校验，若发现破位且后端未及时处理，会主动发起平仓请求（双保险）。

---

## 4. 已修复问题列表
1. **仓位重复**: 通过在前后端双重引入基于 `TokenID + Side` 的去重 Map 解决。
2. **新仓位无数据**: 修正了 `useRiskControlCache` 中订阅副作用的触发时机。
3. **档位显示错误**: 实现了盘口数据的正确排序与前五档截取。

---

## 5. 相关文档

- [实时运行日志与看板设计](./runtime-observability.md) — 可观测性事件模型、后端 ring/总线、WS 传输与前端日志面板方案。
