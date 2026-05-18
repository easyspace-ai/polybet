package badgerdb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

const (
	defaultOpenAttempts = 7
	badgerLockFile      = "LOCK" // advisory PID marker; directory flock is the real lock (badger v4 db.go)
)

// openRetryDelays spaces retries from 200ms up to 1s during restart overlap.
var openRetryDelays = []time.Duration{
	200 * time.Millisecond,
	300 * time.Millisecond,
	400 * time.Millisecond,
	600 * time.Millisecond,
	800 * time.Millisecond,
	time.Second,
	time.Second,
}

// openSleep is time.Sleep in production; tests may replace it to avoid delays.
var openSleep = time.Sleep

// DB wraps a BadgerDB v4 handle (ADR Phase 1).
type DB struct {
	inner *badger.DB
	dir   string
}

// Open opens or creates a database at dir with durability tuned for operator safety.
// Retries briefly when the directory flock is held during restart overlap (Electron respawn).
//
// Badger v4.9.x always acquires a directory flock; there is no WithDirLock or BypassDirLock
// in this release (BypassDirLock existed in older Badger but was removed — do not disable).
func Open(dir string, syncWrites bool) (*DB, error) {
	if dir == "" {
		return nil, errors.New("badgerdb: empty dir")
	}
	opts := badger.DefaultOptions(dir).
		WithSyncWrites(syncWrites).
		WithLoggingLevel(badger.ERROR)

	attempts := openAttemptsFromEnv()
	var lastErr error
	for try := 1; try <= attempts; try++ {
		inner, err := badger.Open(opts)
		if err == nil {
			return &DB{inner: inner, dir: dir}, nil
		}
		lastErr = err
		if !isDirLockError(err) || try == attempts {
			break
		}
		openSleep(openRetryDelay(try - 1))
	}
	if isDirLockError(lastErr) {
		return nil, fmt.Errorf(
			"badgerdb open %q: directory lock 未释放 / lock still held after %d attempts: %w\n%s",
			dir, attempts, lastErr, lockHint(dir),
		)
	}
	return nil, fmt.Errorf("badgerdb open %q: %w", dir, lastErr)
}

func openAttemptsFromEnv() int {
	if v := strings.TrimSpace(os.Getenv("POLYBET_BADGER_OPEN_RETRIES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return defaultOpenAttempts
}

func openRetryDelay(attemptIdx int) time.Duration {
	if attemptIdx < len(openRetryDelays) {
		return openRetryDelays[attemptIdx]
	}
	return openRetryDelays[len(openRetryDelays)-1]
}

func isDirLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Cannot acquire directory lock") ||
		strings.Contains(msg, "resource temporarily unavailable")
}

func lockHint(dir string) string {
	lockPath := filepath.Join(dir, badgerLockFile)
	return fmt.Sprintf(
		"排查：确认没有其他 polybet/server 进程占用该目录；第二个实例请设置 POLYBET_BADGER_DIR 指向独立路径。"+
			" 可用 lsof %q 查看占用进程（Badger 对目录本身加 flock，并写入 %q 作为 PID 标记）。 / "+
			"Troubleshoot: ensure no other polybet/server process holds this dir; for a second instance set POLYBET_BADGER_DIR. "+
			"Run: lsof %q (Badger locks the directory via flock and writes %q as a PID marker).",
		dir, lockPath, dir, lockPath,
	)
}

// Dir returns the on-disk directory.
func (d *DB) Dir() string {
	if d == nil {
		return ""
	}
	return d.dir
}

// Close releases the database handle.
func (d *DB) Close() error {
	if d == nil || d.inner == nil {
		return nil
	}
	return d.inner.Close()
}

// Inner exposes the raw handle for advanced callers (e.g. subscriptions).
func (d *DB) Inner() *badger.DB {
	if d == nil {
		return nil
	}
	return d.inner
}

// View runs a read-only transaction.
func (d *DB) View(fn func(txn *badger.Txn) error) error {
	if d == nil || d.inner == nil {
		return errors.New("badgerdb: nil db")
	}
	return d.inner.View(fn)
}

// Update runs a read-write transaction.
func (d *DB) Update(fn func(txn *badger.Txn) error) error {
	if d == nil || d.inner == nil {
		return errors.New("badgerdb: nil db")
	}
	return d.inner.Update(fn)
}
