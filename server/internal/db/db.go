package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed all:migrations
var migrations embed.FS

func Open(databaseURL string) (*sql.DB, error) {
	// modernc.org/sqlite expects "file:path" or memory
	dsn := databaseURL
	if strings.HasPrefix(dsn, "file:") {
		if !strings.Contains(dsn, "_pragma=") && !strings.Contains(dsn, "?") {
			dsn = dsn + "?_pragma=foreign_keys(1)"
		} else if strings.Contains(dsn, "?") && !strings.Contains(dsn, "_pragma=") {
			dsn = dsn + "&_pragma=foreign_keys(1)"
		}
	} else if strings.Contains(dsn, "://") {
		// prisma-style file:./dev.db -> sqlite path
		dsn = strings.TrimPrefix(dsn, "file:")
		dsn = "file:" + dsn + "?_pragma=foreign_keys(1)"
	} else {
		dsn = "file:" + dsn + "?_pragma=foreign_keys(1)"
	}
	if err := ensureSQLiteParentDir(dsn); err != nil {
		return nil, fmt.Errorf("sqlite parent dir: %w", err)
	}
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if err := Migrate(context.Background(), conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// ensureSQLiteParentDir creates the parent directory for a file: DSN so SQLite can create the db file.
func ensureSQLiteParentDir(dsn string) error {
	if !strings.HasPrefix(dsn, "file:") {
		return nil
	}
	rest := strings.TrimPrefix(dsn, "file:")
	// file:///absolute/path
	if strings.HasPrefix(rest, "///") {
		rest = rest[3:]
	} else if strings.HasPrefix(rest, "//") {
		rest = rest[2:]
	}
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" || rest == ":memory:" || strings.HasPrefix(rest, ":memory:") ||
		strings.EqualFold(rest, "mem:") {
		return nil
	}
	dir := filepath.Dir(rest)
	if dir == "." || dir == "" || dir == "/" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func Migrate(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("schema_migrations: %w (check DATABASE_URL path exists or is writable; SQLite often reports SQLITE_CANTOPEN as 'out of memory')", err)
	}
	var v int
	_ = conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)
	if v >= 1 {
		return nil
	}
	b, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, string(b)); err != nil {
		return fmt.Errorf("migrate 001: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (1)`); err != nil {
		return err
	}
	return nil
}
