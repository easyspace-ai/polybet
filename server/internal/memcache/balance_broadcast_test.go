package memcache

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
)

func TestBalanceCache_MarkBalanceBroadcastIfChanged(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	b := NewBalanceCache(nil, &config.Config{}, log)

	v := 42.5
	s := &balancesvc.Summary{
		Polymarket: &v,
		PolymarketAccounts: []balancesvc.AccountRow{
			{ID: "a1", Name: "one", IsActive: true, Polymarket: &v},
		},
	}

	if !b.MarkBalanceBroadcastIfChanged("a1", s) {
		t.Fatal("first mark should broadcast")
	}
	if b.MarkBalanceBroadcastIfChanged("a1", s) {
		t.Fatal("identical payload should not broadcast")
	}

	if !b.MarkBalanceBroadcastIfChanged("a2", s) {
		t.Fatal("active account id change should broadcast even if numbers match")
	}

	b.Invalidate(context.Background())
	if !b.MarkBalanceBroadcastIfChanged("a2", s) {
		t.Fatal("after Invalidate digest is cleared; should broadcast again")
	}
}
