# Polybet / polybet monorepo — build orchestration
#
# Layout (important paths):
#   apps/dashboard/     Vite + React operator UI (talks to /api and /ws on the Go server)
#   apps/desktop/       Electron shell (electron-vite + electron-builder)
#   server/             Go module github.com/easyspace-ai/polybet (HTTP API + embedded UI)
#   polymarket-go-sdk/  Local replace for the Go module
#
# .env: cmd/server calls LoadEnvFile() then ApplyHomePolybetProjectJSON() (SPORTS_ROUTER_ENV_FILE →
# ~/.polybet/.env → .env beside binary → cwd .env; JSON fills empty env from ~/.polybet/polybet-project.json).
# `make run-server` sets SPORTS_ROUTER_ENV_FILE to the repo root `.env` so that wins when present.
#
# Electron embedded Go: ~/.polybet/polybet-project.json only (no server.env mirror).
# Dev: child cwd is ~/.polybet/embedded (default SQLite absolute path there).
# Packaged: legacy userData `.env` may migrate into JSON once; cwd stays userData.
#
# Electron + embedded server flow:
#   1) `make dashboard-embed` copies Vite `dist/` into server/internal/webui/dashboard-dist
#      (compiled into the Go binary via go:embed).
#   2) `apps/desktop/scripts/bundle-cli.mjs` cross-builds ./cmd/server → polybet and
#      copies it to apps/desktop/resources/bin/ (unpacked from asar at runtime).
#   3) Electron starts polybet if that binary exists, waits for /api/health,
#      then loads http://HOST:PORT/ (dashboard from the Go process). Otherwise it keeps
#      the legacy cloud `polybet` daemon behaviour.

SERVER := $(CURDIR)/server

.PHONY: help install deps dashboard-build dashboard-embed go-build-server run-server test-go lint-dashboard desktop-resources desktop-build desktop-package desktop-package-win clean-dashboard-embed all-desktop release-tag-push

help:
	@echo "Polybet Makefile"
	@echo ""
	@echo "  make install              - pnpm install at repo root"
	@echo "  make dashboard-build      - build apps/dashboard (Vite → dist/)"
	@echo "  make dashboard-embed      - dashboard-build + copy dist into Go webui embed dir"
	@echo "  make go-build-server      - embed + go build ./cmd/server → server/bin/polybet"
	@echo "  make run-server           - embed + go run with repo .env"
	@echo "  make test-go              - go test in server module"
	@echo "  make desktop-resources    - dashboard-embed + bundle-cli (Go binary into Electron resources)"
	@echo "  make desktop-build        - desktop-resources + electron-vite build"
	@echo "  make desktop-package      - desktop-build + electron-builder (see apps/desktop/scripts/package.mjs)"
	@echo "  make desktop-package-win  - desktop-build + Windows x64 + arm64 installers only (--publish never)"
	@echo "  make all-desktop          - desktop-package (convenience)"
	@echo ""
	@echo "  GitHub Release (triggers .github/workflows/release.yml)"
	@echo "  make release-tag-push TAG=v0.1.2              - annotated tag at HEAD + git push origin TAG"
	@echo "  make release-tag-push TAG=v0.1.2 MSG='...'    - same, but commit all + push branch first"
	@echo ""

install: deps

deps:
	pnpm install

dashboard-build:
	cd "$(CURDIR)/apps/dashboard" && pnpm install && pnpm run build

dashboard-embed: dashboard-build
	@test -f "$(SERVER)/go.mod" || (echo "error: missing $(SERVER)/go.mod"; exit 1)
	@dest="$(SERVER)/internal/webui/dashboard-dist"; \
	rm -rf "$$dest" && mkdir -p "$$dest" && cp -a "$(CURDIR)/apps/dashboard/dist/." "$$dest/" && \
	echo "dashboard → $$dest"

go-build-server: dashboard-embed
	@test -f "$(SERVER)/go.mod" || (echo "error: missing $(SERVER)/go.mod"; exit 1)
	@mkdir -p "$(SERVER)/bin" && (cd "$(SERVER)" && go build -o bin/polybet ./cmd/server) && \
	echo "built $(SERVER)/bin/polybet"

run-server: dashboard-embed
	@test -f "$(SERVER)/go.mod" || (echo "error: missing $(SERVER)/go.mod"; exit 1)
	@env SPORTS_ROUTER_ENV_FILE="$(CURDIR)/.env" sh -c "cd \"$(SERVER)\" && exec go run ./cmd/server"

test-go:
	@test -f "$(SERVER)/go.mod" || (echo "error: missing $(SERVER)/go.mod"; exit 1)
	@(cd "$(SERVER)" && go test ./...)

lint-dashboard:
	cd "$(CURDIR)/apps/dashboard" && pnpm install && pnpm run lint

desktop-resources: dashboard-embed
	@test -f "$(SERVER)/go.mod" || (echo "error: missing $(SERVER)/go.mod"; exit 1)
	@export SERVER_DIR="$(SERVER)"; \
	cd "$(CURDIR)/apps/desktop" && pnpm exec node ./scripts/bundle-cli.mjs

desktop-build: desktop-resources
	cd "$(CURDIR)/apps/desktop" && pnpm run build

desktop-package: desktop-build
	cd "$(CURDIR)/apps/desktop" && pnpm run package

desktop-package-win: desktop-build
	cd "$(CURDIR)/apps/desktop" && pnpm run package:win

all-desktop: desktop-package

clean-dashboard-embed:
	@test -f "$(SERVER)/go.mod" || exit 0
	@rm -rf "$(SERVER)/internal/webui/dashboard-dist" && mkdir -p "$(SERVER)/internal/webui/dashboard-dist" && \
	printf '%s\n' '<!doctype html><html><body><p>Run <code>make dashboard-embed</code></p></body></html>' > "$(SERVER)/internal/webui/dashboard-dist/index.html"

# Push a semver Git tag so GitHub Actions runs release.yml (GoReleaser, Docker, desktop publish).
# TAG must match .github/workflows/release.yml (verify job + on.push.tags filter).
release-tag-push:
	@test -n "$(TAG)" || (echo "error: set TAG=vX.Y.Z  e.g.  make release-tag-push TAG=v0.1.2"; exit 1)
	@echo "$(TAG)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || \
		(echo "error: TAG must look like v1.2.3 or v1.2.3-rc.1 (see .github/workflows/release.yml)"; exit 1)
	@echo "$(TAG)" | grep -qv 'dirty' || (echo "error: tag name must not contain 'dirty' (workflow excludes it)"; exit 1)
	@if git show-ref --verify --quiet "refs/tags/$(TAG)"; then \
		echo "error: tag $(TAG) already exists locally — delete it or pick a new version"; exit 1; \
	fi
ifdef MSG
	@if git diff --quiet && git diff --cached --quiet; then \
		echo "note: working tree clean, skipping commit (MSG was set)"; \
	else \
		git add -A && git commit -m "$(MSG)"; \
	fi
	@current_branch=$$(git rev-parse --abbrev-ref HEAD); \
		if [ "$$current_branch" = "HEAD" ]; then \
			echo "error: detached HEAD — checkout a branch before using MSG="; exit 1; \
		fi; \
		git push origin "$$current_branch"
endif
	git tag -a "$(TAG)" -m "Release $(TAG)"
	git push origin "$(TAG)"
	@echo "Pushed tag $(TAG). Open Actions → Release on GitHub to watch the run."
