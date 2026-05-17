package logx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCategoryLoggers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("POLYBET_LOG_DIR", dir)
	t.Setenv("POLYBET_DISABLE_DISK_LOG", "")

	persistentLogDir = dir
	defer func() {
		persistentLogDir = ""
		ClosePersistentLog()
	}()

	if err := OpenCategoryLoggers(); err != nil {
		t.Fatalf("OpenCategoryLoggers: %v", err)
	}
	if err := OpenHTTPAccessLog(); err != nil {
		t.Fatalf("OpenHTTPAccessLog: %v", err)
	}

	Trade().WithField("event", "test").Info("trade event")
	Position().WithField("event", "test").Info("position event")
	Open().WithField("event", "test").Info("open event")
	StopLoss().WithField("event", "test").Info("stoploss event")
	LogHTTPAccess("GET", "/api/health", 200, 0, "127.0.0.1", "req-1")

	for _, name := range []string{FileTrade, FilePosition, FileOpen, FileStopLoss, FileHTTPAccess} {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		if st.Size() == 0 {
			t.Fatalf("expected non-empty %s", name)
		}
	}
}

func TestCategoryLoggersDisabled(t *testing.T) {
	t.Setenv("POLYBET_DISABLE_DISK_LOG", "1")
	if err := OpenCategoryLoggers(); err != nil {
		t.Fatalf("OpenCategoryLoggers: %v", err)
	}
	Trade().Info("should discard")
}
