# Data layer: SQLite, memory snapshots, sync

This document describes what lives in **SQLite** versus **in-process memory**, how the dashboard should treat HTTP and WebSocket data, and the main sync intervals. It complements [risk-control-logic.md](./risk-control-logic.md) and [runtime-observability.md](./runtime-observability.md).

---

## 1. SQLite scope

Persistent storage is used for:

- **Historical / operational data** — e.g. orders, trades, risk tasks, applied CLOB trade ids, bot config.
- **Risk domain** — `risk_positions`, `risk_position_configs`, and related tables.
- **Event / market catalog** — Gamma-backed **event list** and moneyline quotes ingested into `events`, `markets`, `outcomes`, etc.

SQLite is **not** used as a hot cache for live balance or denormalized “current positions JSON” for the HTTP risk list; those are served from **process memory** (see below) with TTL, then refreshed from upstream APIs.

---

## 2. No Rediska / no embedded KV for hot paths

Balance and risk **snapshots** are stored in **`internal/memcache`** (`BalanceCache`, `RiskCache`):

- Backed by **`sync.RWMutex`** on the snapshot value (plus a small mutex serializing **background refresh** goroutines, matching the previous `RefreshAsync` contract).
- **Single writer** per cache type for async refresh: only one outstanding background refresh at a time per cache; concurrent readers use `RLock`.
- **TTL** in memory (currently one hour per snapshot); `Invalidate` clears the entry immediately.

There is **no** separate in-process SQLite (Rediska/redka) for these keys.

Optional future: a third snapshot **`eventsSnapshot`** (WS-first event list) is **not** implemented yet; today the event list for the UI is still **DB + `marketsSnapshot` WS** after Gamma sync.

---

## 3. Event list (Gamma) sync

| Mechanism | Default | Notes |
|-----------|---------|--------|
| **Ticker** | **1 hour** | Driven by `bot_config.pollingInterval` in **minutes** (seed default **60** = 1h). Minimum clamp **1** minute. If you previously stored **seconds** (e.g. `3600`), change the config to **`60`** for the same hourly behavior. |
| **Startup** | One `SyncEngine.Once` | Runs in background after HTTP listens; can take minutes. |
| **Hard refresh** | `POST /api/markets/refresh` | **Bypasses** the “previous sync still running, skip” throttle when **force** is true (default). |

**Force query (hard refresh throttle bypass):**

- `POST /api/markets/refresh` — **`force` defaults to on** (empty `?force=` → wait behind lock, same as `?force=1` / `true` / `yes`).
- Opt out of bypass (rare): `?force=0`, `false`, or `no` — uses non-blocking `TryLock` behavior like the periodic ticker.

The sync engine still uses a **single global mutex** per run so two full Gamma passes never overlap; **force** means “wait for the lock” instead of “skip if busy”.

---

## 4. Balance: server memory + WS + HTTP contract

**Server**

- Latest summary is kept in **`BalanceCache`** with `UpdatedAt` / TTL semantics (`UpdatedAt` exposed for observability).
- After position-changing paths, balance is **debounced** (same `debounce` package as the book bridge, key `balance_rebuild`, **120 ms**) before `balancesvc.Fetch` + `Set` + **`balance_update`** on `Hub` / `RiskHub`.
- **`InvalidateAndRebuildCache`** still fetches balance **immediately** (no debounce), for explicit user/cache refresh flows.

**WebSocket**

- Payload: `{"type":"balance_update","data":<balancesvc.Summary JSON>}` (see [runtime-observability.md](./runtime-observability.md) §3.2).

**Frontend contract**

1. **Prefer cache first**: after initial `GET /api/balances`, treat the JSON as authoritative until a **`balance_update`** WS arrives or the user triggers refresh.
2. **Hydrate from WS** when connected so multiple tabs stay aligned with the server snapshot.

**TODO (optional, low coupling)**

- Persist last balance in **`sessionStorage`** on successful fetch/WS for faster first paint; not implemented in the dashboard yet.

---

## 5. Positions freshness

| Source | Role |
|--------|------|
| **User CLOB WebSocket** | Primary stream for fills; after a new trade, calls **`SyncPositionsFromDataAPI`** then **`rebuildAndBroadcastCache`** (positions + debounced balance). |
| **Periodic Data API reconcile** | **`positionsReconcileTicker`** in `app`: **~20 s** when the active account has at least one **open** risk row at or above `minOpenRiskShares`, else **60 s**. **Single-flight** (`sync.Mutex.TryLock`): if a reconcile is still running, the next tick is skipped. |
| **REST trades ticker** | Existing slower path (`SyncRiskFromRESTTrades`) remains. |
| **Manual** | `POST /api/risk/refresh` → `InvalidateAndRebuildCache`. |

Debounce for book-driven risk evaluation remains separate (stop-loss path).

**Risk runtime logs** — Structured, in-memory-only trail: `internal/riskruntime` ring (**400** entries by default), streamed on **`/ws/risk`** as `risk_runtime_log` / `risk_runtime_log_snapshot`, plus **`GET /api/risk/runtime-logs?limit=`** (cap **500**) for backfill. Field contract: [runtime-observability.md](./runtime-observability.md) §2 and §4.2.1.

---

## 6. 开发计划 / checklist

- [x] Replace Rediska with `internal/memcache` (`BalanceCache`, `RiskCache`).
- [x] Gamma / event list default interval **1h** (`pollingInterval` in **minutes**, seed + ticker default **60**, min **1** min).
- [x] `POST /api/markets/refresh` **force** default + query override.
- [x] Positions periodic **Data API** reconcile with **single-flight** + dynamic interval (20s / 60s).
- [x] Balance **debounced** refresh on rebuild path; immediate on full invalidation.
- [ ] Dashboard: optional **`sessionStorage`** balance hydrate (TODO).
- [ ] Optional: in-memory **`eventsSnapshot`** if we want WS-first events without hitting SQLite read path every time.

---

## 7. Related code (quick index)

| Area | Package / file |
|------|----------------|
| Memory caches | `server/internal/memcache/` |
| Gamma sync mutex + force | `server/internal/sync/engine.go` |
| Market ticker + broadcast | `server/internal/app/app.go` (`syncTicker`, `SyncAndBroadcastMarkets`) |
| Positions ticker | `server/internal/app/app.go` (`positionsReconcileTicker`) |
| User WS → DB + cache | `server/internal/app/poly_user_ws.go` |
| HTTP risk + balance | `server/internal/httpserver/handler.go` |
