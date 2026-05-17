# Dev performance: Pending APIs and profiling

## Browser connection limit (Vite `:6688`)

The dashboard opens **two** WebSockets (`/ws` and `/ws/risk`). On the same host as proxied `/api/*`, Chrome’s HTTP/1.1 limit (~**6** concurrent connections per host) can leave `fetch()` stuck in **Pending** during bursts (e.g. full page refresh).

**Mitigation:** set `VITE_API_BASE_URL=http://127.0.0.1:7633` in `apps/dashboard/.env.development` (see [apps/dashboard/.env.example](apps/dashboard/.env.example)) so REST and WS use the Go port directly; Vite then only serves static assets.

## CPU profile during a slow refresh

1. Start the server with `POLYBET_ENABLE_PPROF=true` (see [.env.example](.env.example)).
2. While reproducing slowness, capture 30s CPU:

   ```bash
   go tool pprof http://127.0.0.1:7633/debug/pprof/profile?seconds=30
   ```

3. In the pprof UI, look for `ListRiskPositionsEnriched`, `SyncEngine`, `badger`, and `BroadcastJSON` hot paths.

## Runtime log volume (`risk_runtime_log`)

- `POLYBET_RUNTIME_BOOK_SUMMARY_DISABLE=true` — disables `market.book.summary_tick` emissions.
- `POLYBET_RUNTIME_BOOK_SUMMARY_MIN_GAP_MS` — per-token minimum gap in ms (**default 3000**, minimum clamp **100**).

Server-side, `risk_runtime_log` is broadcast **asynchronously** and hub logs for that type are **Debug** (hidden unless `LOG_LEVEL=debug`). Process default log level when `LOG_LEVEL` is unset is **warn** (see `config.Load` and `cmd/server/main.go`).
