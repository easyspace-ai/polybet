package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/easyspace-ai/polybet/internal/app"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/db"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/sirupsen/logrus"
)

// Injected by go build -ldflags (see apps/desktop/scripts/bundle-cli.mjs).
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	logx.Configure("info")
	config.LoadEnvFile()
	config.ApplyHomePolybetProjectJSON()

	cfg, err := config.Load()
	if err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Error("加载配置失败")
		os.Exit(1)
	}
	logx.Configure(cfg.LogLevel)
	if err := logx.EnablePersistentLog(); err != nil {
		logrus.WithFields(logx.Pairs("err", err.Error())).Warn("日志落盘未启用（仍输出到 stdout）")
	} else if d := logx.PolybetLogsDir(); d != "" {
		logrus.WithFields(logx.Pairs("log_dir", d)).Info("进程日志已追加写入磁盘")
	}
	defer logx.ClosePersistentLog()

	sqlDB, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Error("打开数据库失败")
		os.Exit(1)
	}
	defer sqlDB.Close()

	logrus.WithFields(logx.Pairs(
		"version", version,
		"commit", commit,
		"date", date,
	)).Info("Polybet 服务进程已启动")

	a := app.New(cfg, sqlDB, logrus.StandardLogger())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Second Ctrl+C forces exit if graceful shutdown is still draining workers.
	go func() {
		<-ctx.Done()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sig)
		<-sig
		logrus.Error("再次收到退出信号，强制退出")
		os.Exit(1)
	}()

	if err := a.Run(ctx); err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Error("服务退出异常")
		os.Exit(1)
	}
}
