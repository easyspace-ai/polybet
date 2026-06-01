// Package sync implements Polymarket Gamma → SQLite market ingestion (moneyline "12" only).
//
// Data path (high level):
//  1. Read bot_config.eventClassificationTags (JSON array of slugs, e.g. ["nba","nhl"]).
//  2. Map each known slug to a Polymarket Gamma series_id (see leagues.go).
//  3. GET https://gamma-api.polymarket.com/events?closed=false&series_id=… (paginated).
//  4. Skip event titles that look like embedded "more markets" / spread / total rows.
//  5. quoteFromMoneyline12: pick the combined moneyline market (NBA/NHL/MLB), map title teams to
//     Gamma `outcomes` labels, attach CLOB token ids + prices (matches bot polymarket.ts).
//  6. store.UpsertPolyMarketQuote: event + market + canonical_bets + outcomes (canonicalize labels).
//  7. GET /api/markets reads active markets + outcomes from DB (marketsvc.BuildMarketsPayload).
//
// Common reasons for empty /api/markets: Gamma returned 0 events, quote step failed (title/ML/outcome map),
// upsert failed (DB), or outcomes skipped because labels did not canonicalize (see routercanon).
package sync

import (
	"context"
	"encoding/json"
	"strings"
	syncstd "sync"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/storage"
)

// Engine runs periodic Polymarket Gamma sync.
type Engine struct {
	cfg         *config.Config
	st          *storage.Backend
	cache       *bookcache.Cache
	sportsCache *SportsCache
	logger      *logrus.Logger
	mu          syncstd.Mutex // avoid overlapping Once (matches Node marketSync running guard)
}

func NewEngine(cfg *config.Config, st *storage.Backend, cache *bookcache.Cache, sportsCache *SportsCache, logger *logrus.Logger) *Engine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Engine{cfg: cfg, st: st, cache: cache, sportsCache: sportsCache, logger: logger}
}

// defaultFeeRate returns the fee fraction (e.g. 0.02 = 2%) to apply when a
// Gamma row does not carry a per-market fee field. Reads bot_config
// syncDefaultTakerFeeRate (float string); falls back to sportsFeeRate.
//
// Acceptable range is [0, 1). Out-of-range values fall back to the
// default. Negative configured values are treated as 0 (no fee), which
// is sometimes accurate for promotional / maker-rebated markets.
func (e *Engine) defaultFeeRate(ctx context.Context) float64 {
	v := e.st.GetBotConfigFloat(ctx, "syncDefaultTakerFeeRate", sportsFeeRate)
	if v < 0 {
		return 0
	}
	if v >= 1 {
		return sportsFeeRate
	}
	return v
}

func parseTagListJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	return tags
}

// Once fetches configured leagues and upserts Polymarket moneyline (12) games.
// When force is true, waits for any in-flight sync instead of skipping (dashboard hard refresh).
func (e *Engine) Once(ctx context.Context, force bool) error {
	if force {
		e.mu.Lock()
	} else if !e.mu.TryLock() {
		e.logger.WithFields(logx.Pairs("msg", "previous sync still running")).Warn("市场同步：上一次仍在运行，跳过")
		return nil
	}
	defer e.mu.Unlock()

	raw, _, err := e.st.GetBotConfig(ctx, "eventClassificationTags")
	if err != nil {
		e.logger.WithFields(logx.Pairs("key", "eventClassificationTags", "err", err)).Error("市场同步：读取配置失败")
		return err
	}
	tags := parseTagListJSON(raw)
	sports, err := e.sportsCache.Get(ctx)
	if err != nil {
		e.logger.WithFields(logx.Pairs("err", err)).Error("市场同步：体育缓存不可用")
		return err
	}
	leagues := leaguesFromTags(tags, sports)
	e.logger.WithFields(logx.Pairs(
		"tags_raw_len", len(strings.TrimSpace(raw)), "tags_parsed", tags, "leagues", len(leagues),
		"gamma_api", gammaAPI, "http_proxy_set", strings.TrimSpace(e.cfg.HTTPPlatformProxy) != "",
	)).Info("市场同步：开始拉取 Gamma")
	if len(leagues) > 0 {
		leagueNames := make([]string, len(leagues))
		for i, lg := range leagues {
			leagueNames[i] = lg.League
		}
		e.logger.WithFields(logx.Pairs("leagues", leagueNames)).Info("市场同步：联赛列表已解析")
	}

	totalEvents := 0
	upserted := 0
	skippedGameLines := 0
	skippedQuote := 0
	skippedUpsertErr := 0
	seenPolyEvents := make(map[string]struct{})

	for _, lg := range leagues {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.logger.WithFields(logx.Pairs("league", lg.League, "series_id", lg.SeriesID, "sport", lg.Sport)).Info("市场同步：拉取联赛页")
		events, err := fetchGammaEvents(ctx, e.cfg.HTTPPlatformProxy, lg.SeriesID)
		if err != nil {
			e.logger.WithFields(logx.Pairs("league", lg.League, "series_id", lg.SeriesID, "err", err)).Error("市场同步：Gamma HTTP 失败")
			continue
		}
		totalEvents += len(events)
		e.logger.WithFields(logx.Pairs("league", lg.League, "events", len(events))).Info("市场同步：Gamma 返回事件数")

		for _, ev := range events {
			if err := ctx.Err(); err != nil {
				return err
			}
			if stringsContainsGameLinesKeyword(ev.Title) {
				skippedGameLines++
				e.logger.WithFields(logx.Pairs("event_id", ev.ID, "title", ev.Title)).Debug("市场同步：跳过子市场标题")
				continue
			}
			q, fee, err := quoteFromMoneyline12WithFee(ev, lg, e.defaultFeeRate(ctx))
			if err != nil {
				skippedQuote++
				e.logger.WithFields(logx.Pairs("event_id", ev.ID, "title", ev.Title, "reason", err.Error())).Debug("市场同步：跳过报价解析")
				continue
			}
			for _, oc := range q.Outcomes {
				e.cache.SetFeeRate(oc.ExternalID, fee)
			}
			if err := e.st.UpsertPolyMarketQuote(ctx, q); err != nil {
				skippedUpsertErr++
				e.logger.WithFields(logx.Pairs("poly_event_id", ev.ID, "external_id", q.ExternalID, "err", err)).Warn("市场同步：写入数据库失败")
				continue
			}
			seenPolyEvents[strings.TrimSpace(ev.ID)] = struct{}{}
			upserted++
			e.logger.WithFields(logx.Pairs("poly_event_id", ev.ID, "league", lg.League, "home", q.HomeTeam, "away", q.AwayTeam)).Debug("市场同步：报价已写入")
		}
		e.logger.WithFields(logx.Pairs("league", lg.League, "events_in_page", len(events))).Info("市场同步：联赛页处理完成")
	}

	e.backfillClosedMarketResolutions(ctx, leagues)

	if deactivated, err := e.st.DeactivatePolyEventsNotIn(ctx, seenPolyEvents); err != nil {
		e.logger.WithFields(logx.Pairs("err", err)).Warn("市场同步：清理过期事件失败")
	} else if deactivated > 0 {
		e.logger.WithFields(logx.Pairs("deactivated_events", deactivated)).Info("市场同步：已关闭未出现在 Gamma 的事件")
	}

	// DB row counts for operators (best-effort)
	nMkt, _ := e.st.CountActiveMarkets(ctx)
	nOut, _ := e.st.CountActiveOutcomes(ctx)

	e.logger.WithFields(logx.Pairs(
		"leagues", len(leagues), "gamma_events_total", totalEvents,
		"quotes_upserted", upserted, "skipped_quote", skippedQuote, "skipped_game_lines_title", skippedGameLines,
		"skipped_upsert_err", skippedUpsertErr,
		"db_active_markets", nMkt, "db_active_outcomes", nOut,
	)).Info("市场同步：本轮结束")
	return nil
}

func stringsContainsGameLinesKeyword(title string) bool {
	i := strings.Index(title, " - ")
	if i < 0 {
		return false
	}
	suf := strings.ToLower(title[i+3:])
	keywords := []string{"more markets", "goal", "total", "over", "under", "handicap", "spread"}
	for _, k := range keywords {
		if strings.Contains(suf, k) {
			return true
		}
	}
	return false
}
