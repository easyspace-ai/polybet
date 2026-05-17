package risksvc

import (
	"context"
	"time"

	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywarm"
	"github.com/easyspace-ai/polybet/internal/store"
)

const closeBookMaxCacheAge = 20 * time.Second

// bookTopForClose returns best bid/ask in cents for logging and pre-close checks.
// Refreshes from CLOB REST when cache is stale, empty, or looks like a partial junk tick.
func (s *Service) bookTopForClose(ctx context.Context, tokenID string) (bidCents, askCents float64, meta map[string]any) {
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	meta = map[string]any{
		"token_id":       tid,
		"clob_token_dec": polyexec.CLOBAssetIDForAPI(tid),
	}
	if tid == "" {
		meta["book_source"] = "invalid_token"
		return 0, 0, meta
	}

	needsREST := true
	if age, ok := s.cache.BookAge(tid); ok {
		meta["book_age_ms"] = age.Milliseconds()
		if age <= closeBookMaxCacheAge {
			bb, ba, topOk := s.cache.TopOfBook(tid)
			bids, asks := s.cache.GetBidsAsks(tid, 1)
			if topOk && (bb > 0 || ba > 0) && (len(bids) > 0 || len(asks) > 0) && !looksLikeJunkTop(bb, ba) {
				needsREST = false
				meta["book_source"] = "cache"
				return bb * 100, ba * 100, meta
			}
		}
	}

	if needsREST {
		if err := polywarm.RefreshFromREST(ctx, s.cfg.PolymarketAPIURL, s.cfg.HTTPPlatformProxy, tid, s.cache); err != nil {
			meta["book_refresh_err"] = err.Error()
			meta["book_source"] = "cache_stale"
		} else {
			meta["book_source"] = "rest"
			if age, ok := s.cache.BookAge(tid); ok {
				meta["book_age_ms"] = age.Milliseconds()
			}
		}
	}

	bb, ba, ok := s.cache.TopOfBook(tid)
	if !ok {
		return 0, 0, meta
	}
	return bb * 100, ba * 100, meta
}

func looksLikeJunkTop(bestBid, bestAsk float64) bool {
	if bestBid <= 0 && bestAsk > 0 && bestAsk <= 0.02 {
		return true
	}
	return false
}
