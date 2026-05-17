package db

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigration010BackfillsConfigForLegacyPositions verifies that the
// idempotent backfill inserts a config row for any legacy risk_position
// without one, using avg_entry_cents as high_water and 20% as stop loss.
// Re-running the migration must NOT change values for rows that already
// have a config (INSERT OR IGNORE invariant).
func TestMigration010BackfillsConfigForLegacyPositions(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}

	// Insert a legacy-looking risk_position with NO config row, then a
	// modern one with a tighter custom stop already configured. Use
	// account-isolated columns added by 003 to mimic post-migration shape.
	_, err = db.Exec(`INSERT INTO risk_positions
		(id, platform, account_id, token_id, title, side_label, avg_entry_cents, size_shares, cost_usd, source, status)
		VALUES('legacy', 'polymarket', 'a1', '0xabc', 't', 'YES', 55.0, 10.0, 5.5, 'polymarket_api', 'open')`)
	if err != nil {
		t.Fatalf("insert legacy: %v", err)
	}
	_, err = db.Exec(`INSERT INTO risk_positions
		(id, platform, account_id, token_id, title, side_label, avg_entry_cents, size_shares, cost_usd, source, status)
		VALUES('modern', 'polymarket', 'a1', '0xdef', 't2', 'YES', 70.0, 5.0, 3.5, 'polymarket_api', 'open')`)
	if err != nil {
		t.Fatalf("insert modern: %v", err)
	}
	_, err = db.Exec(`INSERT INTO risk_position_configs
		(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
		VALUES('modern', 80.0, 5.0, datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert modern config: %v", err)
	}

	// Re-apply the 010 backfill SQL directly. We cannot rewind schema_migrations
	// and call Migrate() once later migrations (011+) have landed — those ALTER
	// TABLE steps are not idempotent. The production path only ever runs 010
	// once; here we verify the embedded SQL itself is idempotent.
	b, err := migrations.ReadFile("migrations/010_risk_position_configs_backfill.sql")
	if err != nil {
		t.Fatalf("read 010: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), string(b)); err != nil {
		t.Fatalf("apply 010: %v", err)
	}

	// Legacy row should now have a config with avg_entry_cents as HW and 20% stop.
	var hw, pct float64
	err = db.QueryRow(`SELECT high_water_cents, stop_loss_pct FROM risk_position_configs WHERE position_id = 'legacy'`).Scan(&hw, &pct)
	if err != nil {
		t.Fatalf("legacy config not present: %v", err)
	}
	if hw != 55.0 {
		t.Fatalf("legacy HW = %v want 55.0 (avg_entry_cents)", hw)
	}
	if pct != 20.0 {
		t.Fatalf("legacy stop_loss_pct = %v want 20.0", pct)
	}

	// Modern row's existing config must be UNCHANGED (idempotent backfill).
	err = db.QueryRow(`SELECT high_water_cents, stop_loss_pct FROM risk_position_configs WHERE position_id = 'modern'`).Scan(&hw, &pct)
	if err != nil {
		t.Fatalf("modern config: %v", err)
	}
	if hw != 80.0 || pct != 5.0 {
		t.Fatalf("modern row was overwritten: hw=%v pct=%v want 80/5", hw, pct)
	}

	// Re-run the backfill once more to confirm idempotency.
	if _, err := db.ExecContext(context.Background(), string(b)); err != nil {
		t.Fatalf("reapply 010: %v", err)
	}
	err = db.QueryRow(`SELECT high_water_cents, stop_loss_pct FROM risk_position_configs WHERE position_id = 'modern'`).Scan(&hw, &pct)
	if err != nil {
		t.Fatalf("modern post-rerun: %v", err)
	}
	if hw != 80.0 || pct != 5.0 {
		t.Fatalf("modern row drifted on rerun: hw=%v pct=%v", hw, pct)
	}
}
