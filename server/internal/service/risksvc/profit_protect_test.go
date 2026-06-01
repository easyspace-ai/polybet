package risksvc

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/storage"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
	"github.com/easyspace-ai/polybet/internal/store"
)

func TestProfitPctFromMark(t *testing.T) {
	pct, ok := profitPctFromMark(10, 20, 60) // mark 60¢, 20 shares = $12, profit $2 = 20%
	if !ok || pct < 19.9 || pct > 20.1 {
		t.Fatalf("got pct=%v ok=%v want ~20", pct, ok)
	}
	_, ok = profitPctFromMark(0, 10, 50)
	if ok {
		t.Fatal("expected false for zero cost")
	}
}

func TestEvaluateProfitProtect_disabledNoTrigger(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := badgerdb.Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	backend := storage.NewBackend(kv)
	_ = backend.SeedDefaultConfig(ctx)
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectEnabled, "false")

	svc := &Service{st: backend, cfg: nil}
	p := store.RiskPosition{
		ID: "p1", AccountID: "a1", TokenID: "0x1", Status: "open",
		SizeShares: 10, CostUSD: 10, AvgEntryCents: 50,
	}
	if err := svc.EvaluateProfitProtect(ctx, p, 80, 80); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateProfitProtect_drawdownTriggers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := badgerdb.Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	backend := storage.NewBackend(kv)
	_ = backend.SeedDefaultConfig(ctx)
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectEnabled, "true")
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectMode, "pct")
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectArmPct, "20")
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectDrawdownPct, "10")

	p := &store.RiskPosition{
		ID: "p1", Platform: "polymarket", AccountID: "a1", TokenID: "0x0000000000000000000000000000000000000000000000000000000000000001",
		Title: "Test", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
		HighWaterCents: 50, StopLossPct: 20, Status: "open", Source: "test",
	}
	if err := backend.CreateRiskPosition(ctx, p); err != nil {
		t.Fatal(err)
	}

	svc := &Service{st: backend, log: logrus.StandardLogger()}
	arm := svc.profitProtectArmPct(ctx)
	pct, ok := profitPctFromMark(p.CostUSD, p.SizeShares, 70)
	if !ok || pct < arm {
		t.Fatalf("precondition: pct=%v arm=%v ok=%v", pct, arm, ok)
	}
	if err := svc.EvaluateProfitProtect(ctx, *p, 70, 70); err != nil {
		t.Fatal(err)
	}
	got, _ := backend.GetRiskPosition(ctx, p.ID)
	if got == nil || !got.ProfitProtectArmed || got.PeakProfitPct < 35 {
		t.Fatalf("expected armed with peak ~40, got %+v", got)
	}
	if err := svc.EvaluateProfitProtect(ctx, *got, 65, 65); err != nil {
		t.Fatal(err)
	}
	has, err := backend.FindPendingCloseTask(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected pending close task for profit_protect")
	}
}

func TestEvaluateProfitProtect_centsModeTriggers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := badgerdb.Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	backend := storage.NewBackend(kv)
	_ = backend.SeedDefaultConfig(ctx)
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectEnabled, "true")
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectMode, "cents")
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectArmCents, "95")
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectStopCents, "85")

	p := &store.RiskPosition{
		ID: "p2", Platform: "polymarket", AccountID: "a1",
		TokenID: "0x0000000000000000000000000000000000000000000000000000000000000002",
		Title: "Test", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
		HighWaterCents: 50, StopLossPct: 20, Status: "open", Source: "test",
	}
	if err := backend.CreateRiskPosition(ctx, p); err != nil {
		t.Fatal(err)
	}

	svc := &Service{st: backend, log: logrus.StandardLogger()}
	if err := svc.EvaluateProfitProtect(ctx, *p, 96, 96); err != nil {
		t.Fatal(err)
	}
	got, _ := backend.GetRiskPosition(ctx, p.ID)
	if got == nil || !got.ProfitProtectArmed || got.PeakMarkCents < 95.9 {
		t.Fatalf("expected armed at 96¢, got %+v", got)
	}
	if err := svc.EvaluateProfitProtect(ctx, *got, 84, 84); err != nil {
		t.Fatal(err)
	}
	has, err := backend.FindPendingCloseTask(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected pending close task for cents profit_protect")
	}
}

func TestEvaluateProfitProtect_customCentsOverride(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := badgerdb.Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	backend := storage.NewBackend(kv)
	_ = backend.SeedDefaultConfig(ctx)
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectEnabled, "true")
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectMode, "cents")

	p := &store.RiskPosition{
		ID: "p3", Platform: "polymarket", AccountID: "a1",
		TokenID: "0x0000000000000000000000000000000000000000000000000000000000000003",
		Title: "Test", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
		HighWaterCents: 50, StopLossPct: 20, Status: "open", Source: "test",
		ProfitProtectCustom: true, ProfitProtectArmCentsOverride: 90, ProfitProtectStopCentsOverride: 80,
	}
	if err := backend.CreateRiskPosition(ctx, p); err != nil {
		t.Fatal(err)
	}

	svc := &Service{st: backend, log: logrus.StandardLogger()}
	if err := svc.EvaluateProfitProtect(ctx, *p, 91, 91); err != nil {
		t.Fatal(err)
	}
	got, _ := backend.GetRiskPosition(ctx, p.ID)
	if got == nil || !got.ProfitProtectArmed {
		t.Fatalf("expected armed with custom 90¢, got %+v", got)
	}
}

func TestProfitProtectEnabledForPosition_disableOverrideWhileGlobalOn(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	kv, err := badgerdb.Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	backend := storage.NewBackend(kv)
	_ = backend.SeedDefaultConfig(ctx)
	_ = backend.UpsertBotConfig(ctx, botKeyProfitProtectEnabled, "true")

	svc := &Service{st: backend, cfg: nil}
	globalOn := svc.profitProtectEnabled(ctx)
	if !globalOn {
		t.Fatal("expected global profit protect enabled")
	}

	p := store.RiskPosition{
		ProfitProtectCustom:            true,
		ProfitProtectUseEnableOverride: true,
		ProfitProtectEnableOverride:    false,
		ProfitProtectArmCentsOverride:  97,
		ProfitProtectStopCentsOverride: 90,
	}
	if svc.profitProtectEnabledForPosition(ctx, p) {
		t.Fatal("expected per-position disable override to win over global + custom thresholds")
	}
}
