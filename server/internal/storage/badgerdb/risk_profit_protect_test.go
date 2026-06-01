package badgerdb

import (
	"context"
	"testing"

	badger "github.com/dgraph-io/badger/v4"
)

func TestUpdateRiskPositionProfitProtectRoundTrip(t *testing.T) {
	ctx := context.Background()
	kv, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()

	doc := &RiskPosDoc{
		ID: "p1", Platform: "polymarket", AccountID: "a1",
		TokenID: "0x0000000000000000000000000000000000000000000000000000000000000001",
		Title: "T", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
		Status: "open", StopLossPct: 20, HighWaterCents: 50,
		CreatedAt: nowRFC(), UpdatedAt: nowRFC(),
	}
	if err := kv.Update(func(txn *badger.Txn) error { return kv.writeRiskPos(txn, doc) }); err != nil {
		t.Fatal(err)
	}
	if err := kv.UpdateRiskPositionProfitProtectState(ctx, "p1", true, 40, 88); err != nil {
		t.Fatal(err)
	}
	got, err := kv.GetRiskPosition(ctx, "p1")
	if err != nil || got == nil {
		t.Fatal(err, got)
	}
	if !got.ProfitProtectArmed || got.PeakProfitPct != 40 || got.PeakMarkCents != 88 {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdateRiskPositionProfitProtectSettings(t *testing.T) {
	ctx := context.Background()
	kv, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()

	doc := &RiskPosDoc{
		ID: "p1", Platform: "polymarket", AccountID: "a1",
		TokenID: "0x0000000000000000000000000000000000000000000000000000000000000001",
		Title: "T", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
		Status: "open", StopLossPct: 20, HighWaterCents: 50,
		CreatedAt: nowRFC(), UpdatedAt: nowRFC(),
	}
	if err := kv.Update(func(txn *badger.Txn) error { return kv.writeRiskPos(txn, doc) }); err != nil {
		t.Fatal(err)
	}
	custom := true
	if err := kv.UpdateRiskPositionProfitProtectSettings(ctx, "p1", ProfitProtectSettingsPatch{
		Custom: &custom, ArmCents: ptrF(92), StopCents: ptrF(82),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := kv.GetRiskPosition(ctx, "p1")
	if err != nil || got == nil {
		t.Fatal(err, got)
	}
	if !got.ProfitProtectCustom || got.ProfitProtectArmCentsOverride != 92 || got.ProfitProtectStopCentsOverride != 82 {
		t.Fatalf("got %+v", got)
	}
	if err := kv.UpdateRiskPositionProfitProtectSettings(ctx, "p1", ProfitProtectSettingsPatch{ClearCustom: true}); err != nil {
		t.Fatal(err)
	}
	got, err = kv.GetRiskPosition(ctx, "p1")
	if err != nil || got == nil {
		t.Fatal(err, got)
	}
	if got.ProfitProtectCustom {
		t.Fatalf("expected custom cleared, got %+v", got)
	}
}

func ptrF(v float64) *float64 { return &v }
