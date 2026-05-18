package httpserver

import (
	"context"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/storage"
)

type bookSummaryPublisher interface {
	PublishBookSummaryTick(tokenID string)
}

func publishBookSummaryFromCache(
	rt *riskruntime.Bus,
	cache *bookcache.Cache,
	st *storage.Backend,
	stopLoss monitoredPositionsLookup,
	tokenID string,
) {
	if rt == nil || cache == nil || tokenID == "" {
		return
	}
	bestBid, bestAsk, ok := cache.TopOfBook(tokenID)
	if !ok && bestBid <= 0 && bestAsk <= 0 {
		return
	}
	bidCents := polyexec.CentsFromPrice01(bestBid)
	askCents := polyexec.CentsFromPrice01(bestAsk)
	acctID := ""
	if st != nil {
		if acct, err := st.GetActivePolymarketAccount(context.Background()); err == nil && acct != nil {
			acctID = acct.ID
		}
	}
	var positions []riskruntime.BookSummaryPosition
	if stopLoss != nil {
		positions = stopLoss.MonitoredPositionsForToken(context.Background(), acctID, tokenID)
	}
	rt.MaybePublishMarketBookSummary(tokenID, acctID, bidCents, askCents, positions)
}

type monitoredPositionsLookup interface {
	MonitoredPositionsForToken(ctx context.Context, accountID, tokenID string) []riskruntime.BookSummaryPosition
}
