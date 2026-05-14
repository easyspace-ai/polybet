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
	dsn := databaseURL
	if strings.HasPrefix(dsn, "file:") {
		dsn = withSQLitePragmas(dsn)
	} else if strings.Contains(dsn, "://") {
		// prisma-style file:./dev.db -> sqlite path
		dsn = strings.TrimPrefix(dsn, "file:")
		dsn = withSQLitePragmas("file:" + dsn)
	} else {
		dsn = withSQLitePragmas("file:" + dsn)
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

func withSQLitePragmas(dsn string) string {
	pragmas := []string{
		"_pragma=foreign_keys(1)",
		"_pragma=busy_timeout(10000)",
		"_pragma=journal_mode(WAL)",
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	for _, pragma := range pragmas {
		if strings.Contains(dsn, pragma) {
			continue
		}
		dsn += sep + pragma
		sep = "&"
	}
	return dsn
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

// Migrate applies embedded SQL migrations in order (version 1 = 001_init.sql, etc.).
func Migrate(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("schema_migrations: %w (check DATABASE_URL path exists or is writable; SQLite often reports SQLITE_CANTOPEN as 'out of memory')", err)
	}
	var v int
	_ = conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)

	steps := []struct {
		version int
		file    string
	}{
		{1, "migrations/001_init.sql"},
		{2, "migrations/002_events_poly_slug.sql"},
		{3, "migrations/003_account_isolation.sql"},
		{4, "migrations/004_risk_configs.sql"},
	}
	for _, step := range steps {
		if v >= step.version {
			continue
		}
		if v != step.version-1 {
			return fmt.Errorf("db migration: expected schema version %d before applying %d, found %d", step.version-1, step.version, v)
		}
		b, err := migrations.ReadFile(step.file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", step.file, err)
		}
		if _, err := conn.ExecContext(ctx, string(b)); err != nil {
			return fmt.Errorf("migrate %d (%s): %w", step.version, step.file, err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, step.version); err != nil {
			return fmt.Errorf("schema_migrations insert %d: %w", step.version, err)
		}
		v = step.version
	}
	return nil
}
