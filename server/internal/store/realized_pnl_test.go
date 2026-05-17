package store

import (
	"context"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

func TestAccountRealizedPnLSince(t *testing.T) {
	kv := openTestBadger(t)
	s := New(kv)
	ctx := context.Background()
	pnl := func(v float64) *float64 { return &v }

	now := time.Now().UTC()
	old := now.Add(-26 * time.Hour).UTC().Format(time.RFC3339Nano)

	// Rows exercised via direct docs for precise closed_at control.
	docs := []struct {
		id     string
		acct   string
		status string
		pnl    *float64
		closed *string
	}{
		{"p1", "acct1", "closed", pnl(-5.0), ptrRFC(now.Add(-1 * time.Hour))},
		{"p2", "acct1", "closed", pnl(-2.5), ptrRFC(now.Add(-30 * time.Minute))},
		{"p3", "acct1", "closed", pnl(7.0), ptrRFC(now.Add(-2 * time.Hour))},
		{"p4", "acct1", "closed", pnl(-100.0), &old},
		{"p5", "acct1", "closed", nil, ptrRFC(now.Add(-10 * time.Minute))},
		{"p6", "acct1", "open", pnl(-50.0), nil},
		{"p7", "acct2", "closed", pnl(-999.0), ptrRFC(now.Add(-1 * time.Hour))},
	}
	for _, d := range docs {
		doc := badgerdb.RiskPosDoc{
			ID: d.id, Platform: "polymarket", AccountID: d.acct, TokenID: "0xtok",
			SideLabel: "Y", Title: "t", Status: d.status, Source: "test",
			AvgEntryCents: 50, SizeShares: 1, CostUSD: 1, HighWaterCents: 50, StopLossPct: 20,
			RealizedPnLUSD: d.pnl, ClosedAt: d.closed,
			CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
		}
		b, err := badgerdb.EncodeJSON(&doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := kv.Update(func(txn *badger.Txn) error {
			return txn.Set(badgerdb.KeyRiskPosition(d.id), b)
		}); err != nil {
			t.Fatal(err)
		}
	}

	since := now.Add(-24 * time.Hour)
	got, err := s.AccountRealizedPnLSince(ctx, "acct1", since)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := -5.0 + -2.5 + 7.0
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}

	if got, err := s.AccountRealizedPnLSince(ctx, "", since); err != nil || got != 0 {
		t.Fatalf("empty account: got=%v err=%v", got, err)
	}
}

func ptrRFC(t time.Time) *string {
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}
