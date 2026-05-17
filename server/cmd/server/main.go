package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/easyspace-ai/polybet/internal/app"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/storage"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
	"github.com/sirupsen/logrus"
)

// Injected by go build -ldflags (see apps/desktop/scripts/bundle-cli.mjs).
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func expandBadgerDir(dir string) (string, error) {
	d := strings.TrimSpace(dir)
	if d == "" {
		return "", nil
	}
	if strings.HasPrefix(d, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(home, strings.TrimPrefix(d, "~/"))
	}
	return filepath.Abs(d)
}

func main() {
	logx.Configure("warn")
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
	if err := logx.OpenCategoryLoggers(); err != nil {
		logrus.WithFields(logx.Pairs("err", err.Error())).Warn("分类日志未启用")
	}
	if err := logx.OpenHTTPAccessLog(); err != nil {
		logrus.WithFields(logx.Pairs("err", err.Error())).Warn("HTTP 访问日志未启用")
	}
	defer logx.ClosePersistentLog()

	badgerDir, err := expandBadgerDir(cfg.BadgerDir)
	if err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Error("解析 Badger 目录失败")
		os.Exit(1)
	}
	if badgerDir == "" {
		logrus.Error("Badger 数据目录为空（请设置 POLYBET_BADGER_DIR）")
		os.Exit(1)
	}
	if err := os.MkdirAll(badgerDir, 0o700); err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Error("创建 Badger 目录失败")
		os.Exit(1)
	}

	kv, err := badgerdb.Open(badgerDir, cfg.BadgerSyncWrites)
	if err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Error("打开 Badger 失败")
		os.Exit(1)
	}
	be := storage.NewBackend(kv)
	if err := storage.InitBadger(context.Background(), cfg, be, logrus.StandardLogger()); err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Error("Badger 初始化失败")
		os.Exit(1)
	}

	logrus.WithFields(logx.Pairs(
		"version", version,
		"commit", commit,
		"date", date,
	)).Info("Polybet 服务进程已启动")

	a := app.New(cfg, be, logrus.StandardLogger())
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
