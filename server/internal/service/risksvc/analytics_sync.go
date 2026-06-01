package risksvc

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/data"
	"github.com/easyspace-ai/polysdk/pkg/types"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
)

// AnalyticsFullSyncResult summarizes a Polymarket closed-positions backfill.
type AnalyticsFullSyncResult struct {
	Fetched     int `json:"fetched"`
	Created     int `json:"created"`
	Updated     int `json:"updated"`
	Skipped     int `json:"skipped"`
	Resolutions int `json:"resolutions"`
}

var analyticsFullSyncRunning atomic.Bool

// SyncClosedPositionsFullFromDataAPI paginates Polymarket /closed-positions and
// upserts local closed risk rows plus market resolution metadata for analytics.
func (s *Service) SyncClosedPositionsFullFromDataAPI(ctx context.Context) (AnalyticsFullSyncResult, error) {
	var out AnalyticsFullSyncResult
	if s == nil || s.st == nil || s.dataClient == nil {
		return out, fmt.Errorf("analytics sync unavailable")
	}
	if !analyticsFullSyncRunning.CompareAndSwap(false, true) {
		return out, fmt.Errorf("analytics sync already running")
	}
	defer analyticsFullSyncRunning.Store(false)

	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil || strings.TrimSpace(acct.FunderAddress) == "" {
		if err != nil {
			return out, fmt.Errorf("no active account: %w", err)
		}
		return out, fmt.Errorf("no active account")
	}
	addr := common.HexToAddress(strings.TrimSpace(acct.FunderAddress))

	const pageSize = 50
	offset := 0
	sortBy := data.ClosedPositionSortTimestamp
	sortDir := data.SortDesc

	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		limit := pageSize
		off := offset
		batch, err := s.dataClient.ClosedPositions(ctx, &data.ClosedPositionsRequest{
			User:          addr,
			Limit:         &limit,
			Offset:        &off,
			SortBy:        &sortBy,
			SortDirection: &sortDir,
		})
		if err != nil {
			return out, fmt.Errorf("closed positions api: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, cp := range batch {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			out.Fetched++
			action, resErr := s.applyOfficialClosedPosition(ctx, acct.ID, cp)
			if resErr != nil {
				if s.log != nil {
					s.log.WithFields(logx.Pairs("token", tokenIDFromDataAsset(cp.Asset), "err", resErr.Error())).Debug("统计同步：跳过一条")
				}
				out.Skipped++
				continue
			}
			switch action {
			case "created":
				out.Created++
			case "updated":
				out.Updated++
			default:
				out.Skipped++
			}
			if cid := strings.TrimSpace(cp.ConditionID.Hex()); cid != "" && cp.Timestamp > 0 {
				tok := tokenIDFromDataAsset(cp.Asset)
				doc := &badgerdb.MarketResolutionDoc{
					ConditionID: cid,
					ResolvedAt:  time.Unix(cp.Timestamp, 0).UTC().Format(time.RFC3339Nano),
					TokenIDs:    []string{tok},
					Source:      badgerdb.ResolutionSourceGammaSync,
				}
				if err := s.st.UpsertMarketResolution(ctx, doc); err == nil {
					out.Resolutions++
				}
			}
		}
		offset += len(batch)
		if len(batch) < pageSize {
			break
		}
	}

	if s.log != nil {
		s.log.WithFields(logx.Pairs(
			"fetched", out.Fetched,
			"created", out.Created,
			"updated", out.Updated,
			"skipped", out.Skipped,
			"resolutions", out.Resolutions,
		)).Info("统计：官方已平仓历史全量同步完成")
	}
	return out, nil
}

func (s *Service) applyOfficialClosedPosition(ctx context.Context, accountID string, cp data.ClosedPosition) (string, error) {
	tokenID := tokenIDFromDataAsset(cp.Asset)
	if tokenID == "" {
		return "", fmt.Errorf("empty token")
	}
	invested, _ := cp.TotalBought.Float64()
	avgPrice, _ := cp.AvgPrice.Float64()
	realized, _ := cp.RealizedPnl.Float64()
	var closedAt time.Time
	if cp.Timestamp > 0 {
		closedAt = time.Unix(cp.Timestamp, 0).UTC()
	}
	entryCents := CentsFromPrice01(avgPrice)

	existing, err := s.st.GetRiskPositionByTokenAccount(ctx, tokenID, accountID)
	if err != nil {
		return "", err
	}
	if existing != nil {
		if existing.Status == "open" || existing.Status == "closing" {
			if err := s.st.CloseRiskPositionOfficial(ctx, existing.ID, invested, realized, closedAt); err != nil {
				return "", err
			}
			_ = s.st.UpdateRiskPositionTitle(ctx, existing.ID, cp.Title, cp.Outcome)
			_ = s.st.UpdateRiskPositionPolySlugs(ctx, existing.ID, cp.EventSlug, cp.Slug)
			if entryCents > 0 {
				_ = s.st.UpdateRiskPositionAvgEntry(ctx, existing.ID, entryCents)
			}
			return "updated", nil
		}
		if existing.Status == "closed" {
			if officialClosedMatches(existing, invested, realized, closedAt) {
				return "skipped", nil
			}
			if err := s.st.UpdateClosedRiskPositionOfficial(ctx, existing.ID, invested, realized, closedAt, cp.Title, cp.Outcome, cp.EventSlug, cp.Slug); err != nil {
				return "", err
			}
			if entryCents > 0 {
				_ = s.st.UpdateRiskPositionAvgEntry(ctx, existing.ID, entryCents)
			}
			return "updated", nil
		}
	}

	pnl := realized
	closedCopy := closedAt
	if closedCopy.IsZero() {
		closedCopy = time.Now().UTC()
	}
	err = s.st.ImportClosedRiskPosition(ctx, &store.RiskPosition{
		ID:             uuid.NewString(),
		Platform:       "polymarket",
		AccountID:      accountID,
		TokenID:        tokenID,
		Title:          strings.TrimSpace(cp.Title),
		SideLabel:      strings.TrimSpace(cp.Outcome),
		PolyEventSlug:  strings.TrimSpace(cp.EventSlug),
		PolyMarketSlug: strings.TrimSpace(cp.Slug),
		AvgEntryCents:  entryCents,
		InvestedUSD:    invested,
		Source:         "polymarket_closed_api",
		Status:         "closed",
		RealizedPnLUSD: &pnl,
		ClosedAt:       &closedCopy,
	})
	if err != nil {
		return "", err
	}
	return "created", nil
}

func tokenIDFromDataAsset(asset types.U256) string {
	hexStr := strings.ToLower(asset.Text(16))
	if len(hexStr) < 64 {
		hexStr = strings.Repeat("0", 64-len(hexStr)) + hexStr
	}
	return store.NormalizeRiskCLOBTokenID("0x" + hexStr)
}

func officialClosedMatches(p *store.RiskPosition, invested, realized float64, closedAt time.Time) bool {
	if p == nil || p.RealizedPnLUSD == nil {
		return false
	}
	if math.Abs(*p.RealizedPnLUSD-realized) > 0.01 {
		return false
	}
	if invested > 0 && p.InvestedUSD > 0 && math.Abs(p.InvestedUSD-invested) > 0.01 {
		return false
	}
	if !closedAt.IsZero() && p.ClosedAt != nil {
		local := p.ClosedAt.UTC()
		if math.Abs(local.Sub(closedAt).Seconds()) > 60 {
			return false
		}
	}
	return true
}
