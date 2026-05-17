package accountsfile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/easyspace-ai/polybet/internal/polymarketacct"
)

func TestAccountsRoundTrip(t *testing.T) {
	t.Cleanup(ResetDefaultForTest)
	dir := t.TempDir()
	t.Setenv("POLYBET_ACCOUNTS_FILE", filepath.Join(dir, "accounts.json"))
	ResetDefaultForTest()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	a := polymarketacct.Account{
		ID: "u1", Name: "A", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := Default().InsertPolymarketAccount(ctx, &a); err != nil {
		t.Fatal(err)
	}
	list, err := Default().ReadAccounts(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("read %v err=%v", list, err)
	}
	if list[0].ID != "u1" || !list[0].IsActive {
		t.Fatalf("got %+v", list[0])
	}
	aid, err := Default().ReadActiveAccountID(ctx)
	if err != nil || aid != "u1" {
		t.Fatalf("active %q err=%v", aid, err)
	}
	u2 := polymarketacct.Account{
		ID: "u2", Name: "B", IsActive: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := Default().InsertPolymarketAccount(ctx, &u2); err != nil {
		t.Fatal(err)
	}
	if err := Default().ActivateAccount(ctx, "u2"); err != nil {
		t.Fatal(err)
	}
	one, err := Default().ReadAccount(ctx, "u2")
	if err != nil || one == nil || !one.IsActive {
		t.Fatalf("u2 %+v err=%v", one, err)
	}
	n, err := Default().DeletePolymarketAccount(ctx, "u1")
	if err != nil || n != 1 {
		t.Fatalf("del n=%d err=%v", n, err)
	}
	if c, _ := Default().CountPolymarketAccounts(ctx); c != 1 {
		t.Fatalf("count=%d", c)
	}
}
