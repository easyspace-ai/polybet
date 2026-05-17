# polybet

Polymarket prediction market trading bot with automated risk management, market sync, and a desktop dashboard.

## Architecture

```
polybet/
├── server/              Go backend (Gin HTTP server + SQLite)
│   ├── cmd/server/      Entry point
│   └── internal/        Domain packages
│       ├── app/         App lifecycle & orchestration
│       ├── config/      Env-based configuration
│       ├── db/          SQLite with auto-migration
│       ├── httpserver/  HTTP + WebSocket API
│       ├── store/       Data access layer
│       ├── sync/        Polymarket Gamma market sync
│       ├── service/     Business logic (risk, trade, routing)
│       └── ...
├── apps/
│   ├── dashboard/       Vite + React operator UI
│   └── desktop/         Electron shell wrapping Go backend
├── packages/
│   ├── core/            @polybet/core — API client, auth, stores
│   ├── types/           @polybet/types — Shared TypeScript type definitions
│   ├── ui/              @polybet/ui — Shared UI components
│   ├── views/           @polybet/views — Shared view components
│   └── ...
└── e2e/                 Playwright end-to-end tests
```

### Data Flow

```
Polymarket CLOB/Gamma API
        ↕
   Go Server (Gin)
   ├── Market sync engine
   ├── Risk management (stop-loss, close-all)
   ├── Trade execution engine
   ├── WebSocket hub → Dashboard clients
   └── Telegram bot
        ↕
 Electron Desktop / Web Dashboard
```

## Quick Start

### Prerequisites

- Go 1.25+
- Node.js 22+
- pnpm 10+

### Setup

```bash
# Install frontend dependencies
pnpm install

# Copy environment template
cp .env.example .env
# Edit .env with your Polymarket API credentials

# Start Go server (development)
make run-server

# Start dashboard (separate terminal)
pnpm dev:web
```

### Desktop Build

Release and smoke workflows run, in order: install workspace deps (`pnpm install`), embed the operator dashboard into the Go tree (`make dashboard-embed`), then package Electron (`apps/desktop/scripts/package.mjs`). Locally, `make desktop-package` runs that full chain.

```bash
# Build with embedded Go server
make desktop-package
```

## Key Configuration

All configuration is via environment variables. See `.env.example` for the full list.

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Yes | — | SQLite path (e.g. `file:./router.db`) |
| `POLYMARKET_PRIVATE_KEY` | Yes* | — | Polymarket wallet private key |
| `HOST` | No | `127.0.0.1` | HTTP listen address |
| `PORT` | No | `7633` | HTTP listen port |

## Development Commands

```bash
pnpm dev:web              # Dashboard dev server
make run-server           # Go server with hot reload
make test-go              # Run Go tests
pnpm test                 # Run all JS/TS tests
pnpm typecheck            # TypeScript type checking
pnpm lint                 # ESLint across all packages
```

## Tech Stack

- **Backend**: Go 1.25, Gin, modernc.org/sqlite (pure Go SQLite)
- **Frontend**: React 19, TanStack Router/Query, Tailwind CSS 4
- **Desktop**: Electron 39, electron-vite, electron-builder
- **Monorepo**: pnpm workspaces, Turborepo, pnpm catalog


ok