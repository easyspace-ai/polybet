package badgerdb

import (
	"context"
	"errors"
	"strings"

	badger "github.com/dgraph-io/badger/v4"
)

// RouterSiblingRow is one Polymarket sibling outcome for router allocation.
// Empty string fields correspond to SQL NULL in the legacy API shape.
type RouterSiblingRow struct {
	OutcomeID        string
	Label            string
	ExternalID       string
	CurrentOdds      float64
	LiquidityDepth   float64
	LiquidityLevels  string
	CanonicalBetID   string
	MarketID         string
	MarketExternalID string
	MarketPlatform   string
	MarketStatus     string
}

// ListRouterPolySiblings returns active Polymarket outcomes sharing the same
// canonical bet as the primary outcome, or only the primary when canonical is empty.
func (d *DB) ListRouterPolySiblings(ctx context.Context, primaryOutcomeID string) ([]RouterSiblingRow, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	pid := strings.TrimSpace(primaryOutcomeID)
	if pid == "" {
		return nil, nil
	}
	canon, err := d.OutcomeCanonID(ctx, pid)
	if err != nil {
		return nil, err
	}
	var oidList []string
	if strings.TrimSpace(canon) == "" {
		oidList = []string{pid}
	} else {
		oidList, err = d.ListActiveOutcomeIDsForCanonical(ctx, canon)
		if err != nil {
			return nil, err
		}
	}
	var out []RouterSiblingRow
	err = d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		for _, oid := range oidList {
			var od outcomeDoc
			if ok, e := d.getJSON(txn, KeyMarketOutcome(oid), &od); e != nil {
				return e
			} else if !ok {
				continue
			}
			var md marketDoc
			if ok, e := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); e != nil {
				return e
			} else if !ok {
				continue
			}
			if md.Platform != "polymarket" || md.Status != "active" {
				continue
			}
			out = append(out, RouterSiblingRow{
				OutcomeID:        od.ID,
				Label:            od.Label,
				ExternalID:       strings.TrimSpace(od.ExternalID),
				CurrentOdds:      od.CurrentOdds,
				LiquidityDepth:   od.LiquidityDepth,
				LiquidityLevels:  od.LiquidityLevels,
				CanonicalBetID:   strings.TrimSpace(od.CanonicalBetID),
				MarketID:         od.MarketID,
				MarketExternalID: md.ExternalID,
				MarketPlatform:   md.Platform,
				MarketStatus:     md.Status,
			})
		}
		return nil
	})
	return out, err
}
