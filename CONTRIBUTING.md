# Contributing to polybet

## Git Workflow

### Branch Naming

```
feature/<description>    # New features
fix/<description>        # Bug fixes
refactor/<description>   # Code refactoring
chore/<description>      # Build/config changes
docs/<description>       # Documentation
```

Examples: `feature/stop-loss-ui`, `fix/sidecar-restart`, `refactor/router-split`

### Commit Messages

Use conventional commits:

```
<type>(<scope>): <description>

[optional body]
```

**Types**: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `perf`, `ci`

**Scopes**: `server`, `dashboard`, `desktop`, `core`, `ui`, `views`, `types`, `e2e`, `deps`

Examples:
```
feat(server): add trailing stop-loss for open positions
fix(desktop): sidecar crash not triggering restart
refactor(server): split router.go into domain handlers
test(server): add store and handler unit tests
chore(deps): migrate shared deps to pnpm catalog
```

### Pull Request Process

1. Create a branch from `main`
2. Make your changes with descriptive commits
3. Ensure all checks pass:
   - `pnpm typecheck`
   - `pnpm lint`
   - `pnpm test`
   - `cd server && go test ./...`
4. Open a PR against `main`
5. Request review from a maintainer

### Release Process

Releases are triggered by annotated tags matching `v*.*.*`:

```bash
make release-tag-push TAG=v0.2.0 MSG="feat: add automated stop-loss"
```

This triggers GitHub Actions to:
1. Verify the tag is valid semver
2. Run Go tests
3. Build CLI binaries via GoReleaser
4. Build and publish Desktop installers via electron-builder

## Code Standards

### Go

- Use `slog` for structured logging (not `log.Printf`)
- Use `context.Context` as first argument for all database/service calls
- Each HTTP handler should be a method on `httpserver.Handler`, not an inline closure
- Pure Go SQLite (`modernc.org/sqlite`) — no CGO dependency
- Error handling: return typed errors, use `errors.Is`/`errors.As`

### TypeScript

- Use `zod` for runtime API response validation
- Use `catalog:` for shared dependency versions (see `pnpm-workspace.yaml`)
- Prefer `type` imports over `interface` where possible
- Domain types belong in `@polybet/types`, not in individual packages

## Project Structure

### Backend (`server/`)

| Package | Responsibility |
|---|---|
| `app` | Application lifecycle, service wiring, background tickers |
| `config` | Environment variable loading and validation |
| `db` | SQLite connection, migration runner |
| `httpserver` | HTTP router, WebSocket relay, handler structs |
| `store` | SQLite data access methods |
| `sync` | Polymarket Gamma API market data sync |
| `service/risksvc` | Position monitoring, stop-loss, close logic |
| `service/tradesvc` | Trade execution via CLOB API |
| `service/routersvc` | Trade allocation planning |
| `polywiring` | Polymarket CLOB WebSocket client |
| `wsrelay` | In-process WebSocket hub for dashboard clients |

### Frontend (`packages/`)

| Package | Responsibility |
|---|---|
| `@polybet/types` | TypeScript type definitions (zero dependencies) |
| `@polybet/core` | API client, auth, state stores, React Query hooks |
| `@polybet/ui` | Shared UI primitives (shadcn-style) |
| `@polybet/views` | Shared view components (issue, chat, agent, editor) |

## Testing

### Go Tests

```bash
cd server && go test ./... -count=1
```

Test files use `testing.T` with in-memory SQLite for store tests. No external dependencies required.

### Frontend Tests

```bash
pnpm test            # Run all workspace tests
pnpm test --filter @polybet/core   # Single package
```

### E2E Tests

```bash
# Start services first, then:
pnpm exec playwright test
```
