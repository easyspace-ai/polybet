package homesettings

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/easyspace-ai/polybet/internal/db"
	"github.com/easyspace-ai/polybet/internal/store"
)

func TestApplyAndSnapshotRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx := context.Background()
	sqlDB, err := db.Open("file:" + filepath.Join(t.TempDir(), "s.db") + "?mode=rwc&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	st := store.New(sqlDB)
	if err := st.SeedDefaultConfig(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertBotConfig(ctx, "maxTradeSize", "123"); err != nil {
		t.Fatal(err)
	}
	if err := SnapshotToFile(ctx, st); err != nil {
		t.Fatal(err)
	}
	p, err := FilePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected file at %s: %v", p, err)
	}
	if err := st.UpsertBotConfig(ctx, "maxTradeSize", "1"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyFromFile(ctx, st); err != nil {
		t.Fatal(err)
	}
	v, ok, err := st.GetBotConfig(ctx, "maxTradeSize")
	if err != nil || !ok || v != "123" {
		t.Fatalf("file should override db: got %q ok=%v err=%v", v, ok, err)
	}
}
