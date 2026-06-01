package sync

import (
	"context"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

// backfillClosedMarketResolutions ingests recently closed Gamma events so analytics
// can bucket PnL by official settlement time (approximate when Gamma has no timestamp).
func (e *Engine) backfillClosedMarketResolutions(ctx context.Context, leagues []League) {
	if e == nil || e.st == nil || e.st.Badger == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	written := 0
	for _, lg := range leagues {
		if err := ctx.Err(); err != nil {
			return
		}
		events, err := fetchGammaClosedEvents(ctx, e.cfg.HTTPPlatformProxy, lg.SeriesID, 2)
		if err != nil {
			e.logger.WithFields(logx.Pairs("league", lg.League, "err", err.Error())).Warn("市场同步：closed 决议回补失败")
			continue
		}
		for _, ev := range events {
			for _, m := range ev.Markets {
				if !m.Closed {
					continue
				}
				cid := strings.TrimSpace(m.ConditionID)
				if cid == "" {
					continue
				}
				tokens := parseJSONStringArray(m.ClobTokenIDs)
				doc := &badgerdb.MarketResolutionDoc{
					ConditionID: cid,
					ResolvedAt:  now,
					TokenIDs:    tokens,
					Source:      badgerdb.ResolutionSourceGammaSync,
				}
				if err := e.st.Badger.UpsertMarketResolution(ctx, doc); err != nil {
					e.logger.WithFields(logx.Pairs("condition_id", cid, "err", err.Error())).Debug("市场同步：决议写入跳过")
					continue
				}
				written++
			}
		}
	}
	if written > 0 {
		e.logger.WithFields(logx.Pairs("resolutions_upserted", written)).Info("市场同步：closed 决议已回补")
	}
}
