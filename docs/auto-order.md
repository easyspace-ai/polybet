# Auto-order

Automated moneyline (`betType` **12**) buy orders on Polymarket sports markets, driven by dashboard-configured groups and a background ticker.

## Policy summary

- **Side**: buy the **popular** outcome (higher implied odds between the two teams).
- **Global floor**: popular price must be **> 50¢** (`outcomePolicy.minImpliedOdds`, default `0.50`).
- **Per-group price gate**: popular price must fall in `[priceGate.minCents, priceGate.maxCents]` inclusive.
- **Sizing**: matching `oddsBands` entry → `stakePct` × group daily remaining budget.
- **Triggers**: within `minutesBeforeStart` of tip-off, and `eventVolume >= minEventVolumeUsd`.
- **Idempotency**: at most one auto order per **group + event + NY calendar day**.

## Configuration

Stored in `bot_config`:

| Key | Description |
|-----|-------------|
| `autoOrderConfig` | JSON policy (groups, daily pool, outcome policy) |
| `autoOrderDryRun` | `true` (default) = log + Telegram simulate only; `false` = live trades |
| `autoOrderTickSec` | Scheduler interval seconds (default `45`, min `15`) |
| `autoOrderLedger` | Internal daily spend + idempotency (managed by engine) |
| `autoOrderRuns` | Recent attempt log (managed by engine) |

### HTTP API

- `GET /api/auto-order/config` — full config + `dryRun`, `tickSec`, `readOnlyMode`
- `PUT /api/auto-order/config` — validate and save (403 in read-only mode)
- `GET /api/auto-order/runs?limit=30` — recent attempts
- `GET /api/teams?league=nba` — Gamma teams list (1h cache)

### Daily pool modes

- `percent_balance`: `wallet USDC × value / 100`
- `fixed_usd`: fixed USD per **America/New_York** calendar day

Group budget = daily pool × `budgetPct / 100`. Enabled groups' `budgetPct` must sum ≤ 100%.

## Execution path

1. App ticker (`autoOrderTicker`) calls `autoorder.Engine.Tick` every `autoOrderTickSec`.
2. Match active `12` markets to enabled groups (league + team list).
3. Evaluate triggers, popular side, price gate; size from odds bands.
4. Respect `readOnlyMode` and `autoOrderDryRun`.
5. Live path: `routersvc.BuildAllocationPlan` → `tradesvc.ExecutePlan` (same as manual trade) + risk gates + Telegram notify.

## Enable live trading

1. Configure groups in **自动下单** (left sidebar) and set master switch **on**.
2. Ensure Polymarket account is active and risk gates allow opens.
3. Flip dry run off — either:
   - Dashboard: disable **模拟模式 (dry-run)** and save, or
   - `PUT /api/config/autoOrderDryRun` with body `{"value":"false"}`, or
   - Edit `~/.polybet/bot-settings.json`: `"autoOrderDryRun": "false"`.

**Keep `autoOrderDryRun=true` until you have verified matches and sizing in `/api/auto-order/runs`.**

## Tests

```bash
cd server && go test ./internal/autoorder/...
```
