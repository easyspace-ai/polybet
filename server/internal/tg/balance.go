package tg

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/store"
)

const collateralEpsilonUSD = 0.015

var (
	collatMu        sync.Mutex
	collatLastUSD   float64
	collatLastValid bool
)

// MaybeNotifyCollateralChanged fetches CLOB collateral (active / env funder) and notifies
// when the value moves by more than collateralEpsilonUSD. The first successful read
// only seeds the baseline (no message) to avoid noise on startup.
func MaybeNotifyCollateralChanged(cfg *config.Config, log *slog.Logger, st *store.Store) {
	if cfg == nil || st == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 28*time.Second)
	defer cancel()
	sum, err := balancesvc.Fetch(ctx, cfg, st)
	if err != nil || sum == nil || sum.Polymarket == nil {
		return
	}
	v := *sum.Polymarket

	collatMu.Lock()
	defer collatMu.Unlock()
	if collatLastValid {
		if math.Abs(v-collatLastUSD) < collateralEpsilonUSD {
			return
		}
		prev := collatLastUSD
		collatLastUSD = v
		Notify(ctx, cfg, st, log, fmt.Sprintf(
			"Polybet 余额变动\nCLOB 约 $%.2f（此前 $%.2f）",
			v, prev,
		))
		return
	}
	collatLastUSD = v
	collatLastValid = true
}
