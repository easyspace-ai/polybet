package homesettings

import (
	"context"
	"os"
	"testing"

	"github.com/easyspace-ai/polybet/internal/storage"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

func TestApplyAndSnapshotRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx := context.Background()
	dir := t.TempDir()
	kv, err := badgerdb.Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	st := storage.NewBackend(kv)
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
		t.Fatalf("file should override persisted config: got %q ok=%v err=%v", v, ok, err)
	}
}
