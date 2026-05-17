package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	accessMu     sync.Mutex
	accessWriter *os.File
)

// OpenHTTPAccessLog opens http-access.log in the same directory as other process logs.
func OpenHTTPAccessLog() error {
	if strings.TrimSpace(os.Getenv("POLYBET_DISABLE_DISK_LOG")) == "1" {
		return nil
	}
	dir := PolybetLogsDir()
	if dir == "" {
		var err error
		dir, err = defaultLogsDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, FileHTTPAccess)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	accessMu.Lock()
	defer accessMu.Unlock()
	if accessWriter != nil {
		_ = accessWriter.Close()
	}
	accessWriter = f
	return nil
}

// CloseHTTPAccessLog closes the HTTP access log file if open.
func CloseHTTPAccessLog() {
	accessMu.Lock()
	defer accessMu.Unlock()
	if accessWriter != nil {
		_ = accessWriter.Close()
		accessWriter = nil
	}
}

// LogHTTPAccess appends one HTTP access line (method, path, status, latency, client IP).
// Request bodies and query strings are intentionally omitted.
func LogHTTPAccess(method, path string, status int, latency time.Duration, clientIP, requestID string) {
	accessMu.Lock()
	w := accessWriter
	accessMu.Unlock()
	if w == nil {
		return
	}
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
	line := fmt.Sprintf("%s method=%s path=%s status=%d latency_ms=%.2f client_ip=%s request_id=%s\n",
		ts, method, path, status, float64(latency.Microseconds())/1000.0, clientIP, requestID)
	accessMu.Lock()
	defer accessMu.Unlock()
	if accessWriter != nil {
		_, _ = accessWriter.WriteString(line)
	}
}
