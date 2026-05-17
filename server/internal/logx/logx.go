// Package logx configures process-wide logrus output for the server binary.
package logx

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
)

var (
	persistentMu     sync.Mutex
	persistentFile   *os.File
	persistentLogDir string
)

// PolybetLogsDir returns the directory used for on-disk logs (after EnablePersistentLog succeeded), or "".
func PolybetLogsDir() string {
	persistentMu.Lock()
	defer persistentMu.Unlock()
	return persistentLogDir
}

// ClosePersistentLog closes the disk log file if open. Safe to call multiple times.
func ClosePersistentLog() {
	CloseCategoryLoggers()
	CloseHTTPAccessLog()
	persistentMu.Lock()
	defer persistentMu.Unlock()
	if persistentFile != nil {
		_ = persistentFile.Close()
		persistentFile = nil
	}
}

// EnablePersistentLog appends all logrus output to $HOME/.polybet/logs/server.log (in addition to stdout).
// Set POLYBET_LOG_DIR to override the directory (still creates a file named server.log inside it).
// Set POLYBET_DISABLE_DISK_LOG=1 to skip disk logging.
func EnablePersistentLog() error {
	if strings.TrimSpace(os.Getenv("POLYBET_DISABLE_DISK_LOG")) == "1" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".polybet", "logs")
	if d := strings.TrimSpace(os.Getenv("POLYBET_LOG_DIR")); d != "" {
		dir = d
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "server.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	persistentMu.Lock()
	defer persistentMu.Unlock()
	if persistentFile != nil {
		_ = persistentFile.Close()
	}
	persistentFile = f
	persistentLogDir = dir

	forceColors := isatty.IsTerminal(os.Stdout.Fd())
	logrus.SetOutput(io.MultiWriter(os.Stdout, f))
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05.000000000Z07:00",
		ForceColors:     forceColors,
		DisableColors:   !forceColors,
	})
	return nil
}

// Configure sets global logrus formatter (text, full timestamp, TTY colors) and level from LOG_LEVEL-style strings.
func Configure(level string) {
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(ParseLevel(level))
	forceColors := isatty.IsTerminal(os.Stdout.Fd())
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05.000000000Z07:00",
		ForceColors:     forceColors,
		DisableColors:   !forceColors,
	})
}

// ParseLevel maps LOG_LEVEL env values to logrus levels.
func ParseLevel(s string) logrus.Level {
	switch s {
	case "debug":
		return logrus.DebugLevel
	case "warn":
		return logrus.WarnLevel
	case "error":
		return logrus.ErrorLevel
	default:
		return logrus.InfoLevel
	}
}

// Pairs builds logrus.Fields from alternating key/value arguments (slog-style).
// Non-string keys become "key"; a trailing key without value is ignored.
func Pairs(kv ...any) logrus.Fields {
	if len(kv) == 0 {
		return nil
	}
	n := (len(kv) + 1) / 2
	f := make(logrus.Fields, n)
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = "key"
		}
		f[k] = kv[i+1]
	}
	return f
}
