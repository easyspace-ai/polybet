package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/easyspace-ai/polybet/internal/app"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/db"
)

// Injected by go build -ldflags (see apps/desktop/scripts/bundle-cli.mjs).
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	config.LoadEnvFile()
	config.ApplyHomePolybetProjectJSON()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	sqlDB, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		slog.Error("db", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: app.SlogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(log)
	log.Info("sports_router_build", "version", version, "commit", commit, "date", date)
	a := app.New(cfg, sqlDB, log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := a.Run(ctx); err != nil {
		slog.Error("run", "err", err)
		os.Exit(1)
	}
}
