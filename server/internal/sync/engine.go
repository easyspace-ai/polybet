// Package sync implements Polymarket Gamma → SQLite market ingestion (moneyline "12" only).
//
// Data path (high level):
//  1. Read bot_config.eventClassificationTags (JSON array of slugs, e.g. ["nba","nhl"]).
//  2. Map each known slug to a Polymarket Gamma series_id (see leagues.go).
//  3. GET https://gamma-api.polymarket.com/events?active=true&closed=false&series_id=… (paginated).
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
	"log/slog"
	"strings"
	syncstd "sync"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/store"
)

// Engine runs periodic Polymarket Gamma sync.
type Engine struct {
	cfg    *config.Config
	st     *store.Store
	cache  *bookcache.Cache
	logger *slog.Logger
	mu     syncstd.Mutex // avoid overlapping Once (matches Node marketSync running guard)
}

func NewEngine(cfg *config.Config, st *store.Store, cache *bookcache.Cache, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{cfg: cfg, st: st, cache: cache, logger: logger}
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
func (e *Engine) Once(ctx context.Context) error {
	if !e.mu.TryLock() {
		e.logger.Warn("market_sync_overlap_skip", "msg", "previous sync still running")
		return nil
	}
	defer e.mu.Unlock()

	raw, _, err := e.st.GetBotConfig(ctx, "eventClassificationTags")
	if err != nil {
		e.logger.Error("market_sync_config", "key", "eventClassificationTags", "err", err)
		return err
	}
	tags := parseTagListJSON(raw)
	leagues := leaguesFromTags(tags)
	e.logger.Info("market_sync_start",
		"tags_raw_len", len(strings.TrimSpace(raw)), "tags_parsed", tags, "leagues", len(leagues),
		"gamma_api", gammaAPI, "http_proxy_set", strings.TrimSpace(e.cfg.HTTPPlatformProxy) != "")

	totalEvents := 0
	upserted := 0
	skippedGameLines := 0
	skippedQuote := 0
	skippedUpsertErr := 0

	for _, lg := range leagues {
		e.logger.Info("market_sync_league_fetch", "league", lg.League, "series_id", lg.SeriesID, "sport", lg.Sport)
		events, err := fetchGammaEvents(ctx, e.cfg.HTTPPlatformProxy, lg.SeriesID)
		if err != nil {
			e.logger.Error("market_sync_gamma_http", "league", lg.League, "series_id", lg.SeriesID, "err", err)
			continue
		}
		totalEvents += len(events)
		e.logger.Info("market_sync_gamma_page", "league", lg.League, "events", len(events))

		for _, ev := range events {
			if stringsContainsGameLinesKeyword(ev.Title) {
				skippedGameLines++
				e.logger.Debug("market_sync_skip_submarket_title", "event_id", ev.ID, "title", ev.Title)
				continue
			}
			q, err := quoteFromMoneyline12(ev, lg)
			if err != nil {
				skippedQuote++
				e.logger.Debug("market_sync_skip_quote", "event_id", ev.ID, "title", ev.Title, "reason", err.Error())
				continue
			}
			for _, oc := range q.Outcomes {
				e.cache.SetFeeRate(oc.ExternalID, sportsFeeRate)
			}
			if err := e.st.UpsertPolyMarketQuote(ctx, q); err != nil {
				skippedUpsertErr++
				e.logger.Warn("market_sync_upsert_failed", "poly_event_id", ev.ID, "external_id", q.ExternalID, "err", err)
				continue
			}
			upserted++
			e.logger.Debug("market_sync_upsert_ok", "poly_event_id", ev.ID, "league", lg.League, "home", q.HomeTeam, "away", q.AwayTeam)
		}
		e.logger.Info("market_sync_league_done", "league", lg.League, "events_in_page", len(events))
	}

	// DB row counts for operators (best-effort)
	var nMkt, nOut int
	_ = e.st.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM markets WHERE status='active'`).Scan(&nMkt)
	_ = e.st.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM outcomes o JOIN markets m ON o.market_id=m.id WHERE m.status='active'`).Scan(&nOut)

	e.logger.Info("market_sync_done",
		"leagues", len(leagues), "gamma_events_total", totalEvents,
		"quotes_upserted", upserted, "skipped_quote", skippedQuote, "skipped_game_lines_title", skippedGameLines,
		"skipped_upsert_err", skippedUpsertErr,
		"db_active_markets", nMkt, "db_active_outcomes", nOut)
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
