package badgerdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/domain"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/routercanon"
)

// --- JSON documents ---

type eventDoc struct {
	ID          string  `json:"id"`
	Sport       string  `json:"sport"`
	League      string  `json:"league"`
	HomeTeam    string  `json:"homeTeam"`
	AwayTeam    string  `json:"awayTeam"`
	StartTime   string  `json:"startTime"`
	EventVolume float64 `json:"eventVolume,omitempty"`
	Status      string  `json:"status"`
	PolyEventID string  `json:"polyEventId"`
	PolySlug    string  `json:"polySlug"`
}

type marketDoc struct {
	ID         string   `json:"id"`
	EventID    string   `json:"eventId"`
	Platform   string   `json:"platform"`
	ExternalID string   `json:"externalId"`
	StartTime  string   `json:"startTime"`
	BetType    string   `json:"betType"`
	Line       *float64 `json:"line,omitempty"`
	MainLine   bool     `json:"mainLine"`
	Status     string   `json:"status"`
}

type canonicalDoc struct {
	ID      string   `json:"id"`
	EventID string   `json:"eventId"`
	Key     string   `json:"key"`
	BetType string   `json:"betType"`
	Side    string   `json:"side"`
	Line    *float64 `json:"line,omitempty"`
}

type outcomeDoc struct {
	ID              string  `json:"id"`
	MarketID        string  `json:"marketId"`
	Label           string  `json:"label"`
	ExternalID      string  `json:"externalId"`
	CurrentOdds     float64 `json:"currentOdds"`
	LiquidityDepth  float64 `json:"liquidityDepth"`
	LiquidityLevels string  `json:"liquidityLevels"`
	LastUpdated     string  `json:"lastUpdated"`
	CanonicalBetID  string  `json:"canonicalBetId"`
	CanonicalKey    string  `json:"canonicalKey"`
}

func (d *DB) getJSON(txn *badger.Txn, key []byte, dest any) (bool, error) {
	it, err := txn.Get(key)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = it.Value(func(val []byte) error {
		return DecodeJSON(val, dest)
	})
	return err == nil, err
}

const marketKickoffGrace = 4 * time.Hour

func parseMarketStartInstant(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (d *DB) eventPolyIndexKey(polyEventID string) []byte {
	return []byte("market/eventPoly/" + polyEventID)
}

func isMarketKickoffOpen(startTime string, now time.Time) bool {
	t, ok := parseMarketStartInstant(startTime)
	if !ok {
		return true
	}
	return now.Before(t.Add(marketKickoffGrace))
}

// UpsertPolyMarketQuote writes event, market, canonical bets, and outcomes (Polymarket only).
func (d *DB) UpsertPolyMarketQuote(ctx context.Context, q *domain.MarketQuote) error {
	if d == nil {
		return errors.New("badgerdb: nil db")
	}
	if q.Platform != "polymarket" {
		return fmt.Errorf("only polymarket quotes supported")
	}
	st := q.StartTime.UTC().Format(time.RFC3339Nano)

	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var eventID string
		epKey := d.eventPolyIndexKey(q.PolyEventID)
		if item, err := txn.Get(epKey); err == nil {
			_ = item.Value(func(v []byte) error {
				eventID = string(v)
				return nil
			})
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		if eventID == "" {
			eventID = "e_poly_" + q.PolyEventID
			ev := eventDoc{
				ID: eventID, Sport: q.Sport, League: q.League, HomeTeam: q.HomeTeam, AwayTeam: q.AwayTeam,
				StartTime: st, EventVolume: q.EventVolume, Status: "active", PolyEventID: q.PolyEventID, PolySlug: strings.TrimSpace(q.PolySlug),
			}
			b, err := EncodeJSON(ev)
			if err != nil {
				return err
			}
			if err := txn.Set(KeyMarketEvent(eventID), b); err != nil {
				return err
			}
			if err := txn.Set(epKey, []byte(eventID)); err != nil {
				return err
			}
		} else {
			var ev eventDoc
			if _, err := d.getJSON(txn, KeyMarketEvent(eventID), &ev); err != nil {
				return err
			}
			ev.Sport, ev.League = q.Sport, q.League
			ev.HomeTeam, ev.AwayTeam = q.HomeTeam, q.AwayTeam
			ev.StartTime = st
			if q.EventVolume > 0 {
				ev.EventVolume = q.EventVolume
			}
			ev.Status = "active"
			if strings.TrimSpace(q.PolySlug) != "" {
				ev.PolySlug = strings.TrimSpace(q.PolySlug)
			}
			b, err := EncodeJSON(ev)
			if err != nil {
				return err
			}
			if err := txn.Set(KeyMarketEvent(eventID), b); err != nil {
				return err
			}
		}

		marketID := "m_poly_" + q.ExternalID
		md := marketDoc{
			ID: marketID, EventID: eventID, Platform: "polymarket", ExternalID: q.ExternalID,
			StartTime: st, BetType: q.BetType, Line: q.Line, MainLine: q.MainLine, Status: "active",
		}
		mb, err := EncodeJSON(md)
		if err != nil {
			return err
		}
		if err := txn.Set(KeyMarketMarket(marketID), mb); err != nil {
			return err
		}

		for _, oc := range q.Outcomes {
			cr := routercanon.Canonicalize(oc.Label, q.BetType, q.HomeTeam, q.AwayTeam)
			if cr.Parts == nil {
				logrus.WithFields(logx.Pairs(
					"poly_event_id", q.PolyEventID, "market_external_id", q.ExternalID,
					"label", oc.Label, "bet_type", q.BetType, "home", q.HomeTeam, "away", q.AwayTeam,
					"reason", cr.Reason,
				)).Warn("市场入库：outcome 规范化跳过")
				continue
			}
			canonID := "cb_" + eventID + "_" + cr.Parts.Key
			cd := canonicalDoc{
				ID: canonID, EventID: eventID, Key: cr.Parts.Key, BetType: cr.Parts.BetType,
				Side: cr.Parts.Side, Line: linePtr(cr.Parts.Line),
			}
			cb, err := EncodeJSON(cd)
			if err != nil {
				return err
			}
			if _, err := txn.Get(KeyMarketCanonical(canonID)); errors.Is(err, badger.ErrKeyNotFound) {
				if err := txn.Set(KeyMarketCanonical(canonID), cb); err != nil {
					return err
				}
			}

			levelsJSON := ""
			if len(oc.LiquidityDepth.TopLevels) > 0 {
				b, _ := json.Marshal(oc.LiquidityDepth.TopLevels)
				levelsJSON = string(b)
			}
			outcomeID := "o_poly_" + oc.ExternalID
			od := outcomeDoc{
				ID: outcomeID, MarketID: marketID, Label: oc.Label, ExternalID: oc.ExternalID,
				CurrentOdds: oc.ImpliedOdds, LiquidityDepth: oc.LiquidityDepth.AvailableSize,
				LiquidityLevels: levelsJSON, LastUpdated: time.Now().UTC().Format(time.RFC3339Nano),
				CanonicalBetID: canonID, CanonicalKey: cr.Parts.Key,
			}
			ob, err := EncodeJSON(od)
			if err != nil {
				return err
			}
			if err := txn.Set(KeyMarketOutcome(outcomeID), ob); err != nil {
				return err
			}
			tok := strings.TrimSpace(oc.ExternalID)
			for _, lookupKey := range CLOBTokenLookupVariants(tok) {
				if err := txn.Set(KeyMarketTokenLookup(lookupKey), []byte(outcomeID)); err != nil {
					return err
				}
			}
			if err := txn.Set(KeyMarketCanonOutcome(canonID, outcomeID), []byte{1}); err != nil {
				return err
			}
		}
		return nil
	})
}

func linePtr(l *float64) *float64 {
	if l == nil {
		return nil
	}
	v := *l
	return &v
}

func sqlNS(s string) sql.NullString {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func sqlNF(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ListActiveMarketsFlat returns active Polymarket markets with joined event fields and outcomes.
func (d *DB) ListActiveMarketsFlat(ctx context.Context) ([]MarketRow, []OutcomeRow, error) {
	if d == nil {
		return nil, nil, errors.New("badgerdb: nil db")
	}
	var markets []MarketRow
	marketIDs := make(map[string]struct{})
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("market/market/")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var md marketDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &md) }); err != nil {
				return err
			}
			if md.Status != "active" || md.Platform != "polymarket" {
				continue
			}
			var ed eventDoc
			if ok, err := d.getJSON(txn, KeyMarketEvent(md.EventID), &ed); err != nil || !ok {
				continue
			}
			if ed.Status != "active" {
				continue
			}
			if !isMarketKickoffOpen(md.StartTime, time.Now().UTC()) {
				continue
			}
			mr := MarketRow{
				ID: md.ID, EventID: md.EventID, Platform: md.Platform, ExternalID: md.ExternalID,
				Sport: ed.Sport, League: ed.League, HomeTeam: ed.HomeTeam, AwayTeam: ed.AwayTeam,
				StartTime: md.StartTime, Status: md.Status, BetType: md.BetType,
				Line: sqlNF(md.Line), MainLine: boolToInt(md.MainLine), PolySlug: ed.PolySlug,
				EventVolume: ed.EventVolume,
			}
			markets = append(markets, mr)
			marketIDs[md.ID] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i < len(markets); i++ {
		for j := i + 1; j < len(markets); j++ {
			if markets[i].StartTime > markets[j].StartTime {
				markets[i], markets[j] = markets[j], markets[i]
			}
		}
	}
	if len(marketIDs) == 0 {
		return markets, []OutcomeRow{}, nil
	}

	var outcomes []OutcomeRow
	err = d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("market/outcome/")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			var od outcomeDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &od) }); err != nil {
				return err
			}
			if _, ok := marketIDs[od.MarketID]; !ok {
				continue
			}
			outcomes = append(outcomes, OutcomeRow{
				MarketID: od.MarketID, ID: od.ID, Label: od.Label,
				ExternalID: sqlNS(od.ExternalID), CurrentOdds: od.CurrentOdds,
				LiquidityDepth: od.LiquidityDepth, LiquidityLevels: sqlNS(od.LiquidityLevels),
				LastUpdated: od.LastUpdated, CanonicalKey: sqlNS(od.CanonicalKey),
			})
		}
		return nil
	})
	return markets, outcomes, err
}

// CountActiveMarkets counts polymarket active markets with active parent event.
func (d *DB) CountActiveMarkets(ctx context.Context) (int, error) {
	m, _, err := d.ListActiveMarketsFlat(ctx)
	return len(m), err
}

// CountActiveOutcomes counts outcomes for those active markets.
func (d *DB) CountActiveOutcomes(ctx context.Context) (int, error) {
	_, o, err := d.ListActiveMarketsFlat(ctx)
	return len(o), err
}

// FindOutcomeIDByToken resolves outcome id from CLOB token external_id.
func (d *DB) FindOutcomeIDByToken(ctx context.Context, tokenID string) (string, bool, error) {
	if d == nil {
		return "", false, errors.New("badgerdb: nil db")
	}
	var out string
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		id, ok, err := d.findOutcomeIDByTokenTxn(txn, tokenID)
		if err != nil {
			return err
		}
		if ok {
			out = id
		}
		return nil
	})
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false, err
	}
	return out, true, err
}

// GetOutcomeWithMarket loads outcome + market + event labels.
func (d *DB) GetOutcomeWithMarket(ctx context.Context, outcomeID string) (outcomeIDRet, marketID, label, extID, home, away string, err error) {
	if d == nil {
		return "", "", "", "", "", "", errors.New("badgerdb: nil db")
	}
	err = d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var od outcomeDoc
		if ok, e := d.getJSON(txn, KeyMarketOutcome(outcomeID), &od); e != nil || !ok {
			if e != nil {
				return e
			}
			return errNotFound
		}
		var md marketDoc
		if ok, e := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); e != nil || !ok {
			if e != nil {
				return e
			}
			return errNotFound
		}
		var ed eventDoc
		if ok, e := d.getJSON(txn, KeyMarketEvent(md.EventID), &ed); e != nil || !ok {
			if e != nil {
				return e
			}
			return errNotFound
		}
		outcomeIDRet = od.ID
		marketID = od.MarketID
		label = od.Label
		extID = strings.TrimSpace(od.ExternalID)
		home, away = ed.HomeTeam, ed.AwayTeam
		return nil
	})
	return
}

var errNotFound = errors.New("not found")

// PolyEventSlugForToken returns normalized event slug for a CLOB token id.
func (d *DB) PolyEventSlugForToken(ctx context.Context, tokenID string) string {
	tid := NormalizeCLOBTokenID(tokenID)
	if tid == "" {
		return ""
	}
	var slug string
	_ = d.View(func(txn *badger.Txn) error {
		for _, candidate := range []string{tid, strings.TrimSpace(tokenID)} {
			if candidate == "" {
				continue
			}
			oid, ok, err := d.findOutcomeIDByTokenTxn(txn, candidate)
			if err != nil || !ok {
				continue
			}
			var od outcomeDoc
			if ok, err := d.getJSON(txn, KeyMarketOutcome(oid), &od); err != nil || !ok {
				continue
			}
			var md marketDoc
			if ok, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !ok {
				continue
			}
			var ed eventDoc
			if ok, err := d.getJSON(txn, KeyMarketEvent(md.EventID), &ed); err != nil || !ok {
				continue
			}
			s := strings.Trim(strings.TrimPrefix(strings.TrimSpace(ed.PolySlug), "event/"), "/")
			if s != "" {
				slug = s
				return nil
			}
		}
		return nil
	})
	return slug
}

func (d *DB) findOutcomeIDByTokenTxn(txn *badger.Txn, tokenID string) (string, bool, error) {
	for _, candidate := range CLOBTokenLookupVariants(tokenID) {
		item, err := txn.Get(KeyMarketTokenLookup(candidate))
		if errors.Is(err, badger.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		var id string
		err = item.Value(func(v []byte) error {
			id = string(v)
			return nil
		})
		if err != nil {
			return "", false, err
		}
		if strings.TrimSpace(id) != "" {
			return id, true, nil
		}
	}
	return "", false, nil
}

// MarketStartTimeForToken returns market start time when known and valid.
func (d *DB) MarketStartTimeForToken(ctx context.Context, tokenID string) (time.Time, bool) {
	tid := NormalizeCLOBTokenID(tokenID)
	if tid == "" {
		return time.Time{}, false
	}
	var out time.Time
	var ok bool
	_ = d.View(func(txn *badger.Txn) error {
		for _, candidate := range []string{tid, strings.TrimSpace(tokenID)} {
			if candidate == "" {
				continue
			}
			oid, has, err := d.findOutcomeIDByTokenTxn(txn, candidate)
			if err != nil || !has {
				continue
			}
			var od outcomeDoc
			if has, err := d.getJSON(txn, KeyMarketOutcome(oid), &od); err != nil || !has {
				continue
			}
			var md marketDoc
			if has, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !has {
				continue
			}
			st := strings.TrimSpace(md.StartTime)
			if st == "" {
				continue
			}
			t := ParseTimeFlexible(st)
			if !IsKnownStartTime(t) {
				continue
			}
			out, ok = t, true
			return nil
		}
		return nil
	})
	return out, ok
}

// ListPolymarketOutcomeTokenIDs returns distinct CLOB token ids for active Polymarket markets.
func (d *DB) ListPolymarketOutcomeTokenIDs(ctx context.Context) ([]string, error) {
	_, outcomes, err := d.ListActiveMarketsFlat(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, o := range outcomes {
		if !o.ExternalID.Valid || o.ExternalID.String == "" {
			continue
		}
		if _, ok := seen[o.ExternalID.String]; ok {
			continue
		}
		seen[o.ExternalID.String] = struct{}{}
		ids = append(ids, o.ExternalID.String)
	}
	return ids, nil
}

// OutcomeCanonID returns canonical bet id for an outcome (empty if missing).
func (d *DB) OutcomeCanonID(ctx context.Context, outcomeID string) (string, error) {
	if d == nil {
		return "", errors.New("badgerdb: nil db")
	}
	var canon string
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var od outcomeDoc
		if ok, err := d.getJSON(txn, KeyMarketOutcome(outcomeID), &od); err != nil {
			return err
		} else if !ok {
			return nil
		}
		canon = strings.TrimSpace(od.CanonicalBetID)
		return nil
	})
	return canon, err
}

// ListActiveOutcomeIDsForCanonical returns outcome ids on active Polymarket markets for a canonical bet.
func (d *DB) ListActiveOutcomeIDsForCanonical(ctx context.Context, canonID string) ([]string, error) {
	if d == nil || strings.TrimSpace(canonID) == "" {
		return nil, nil
	}
	var ids []string
	prefix := []byte("market/canonOut/" + canonID + "/")
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			k := string(it.Item().Key())
			parts := strings.Split(k, "/")
			if len(parts) < 4 {
				continue
			}
			oid := parts[len(parts)-1]
			var od outcomeDoc
			if ok, err := d.getJSON(txn, KeyMarketOutcome(oid), &od); err != nil || !ok {
				continue
			}
			var md marketDoc
			if ok, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !ok {
				continue
			}
			if md.Platform != "polymarket" || md.Status != "active" {
				continue
			}
			ids = append(ids, oid)
		}
		return nil
	})
	return ids, err
}

var marketDataPrefixes = [][]byte{
	[]byte("market/event/"), []byte("market/market/"), []byte("market/outcome/"),
	[]byte("market/canonical/"), []byte("market/canonOut/"), []byte("market/tokenLookup/"), []byte("market/eventPoly/"),
}

func (d *DB) ClearAllMarketData(ctx context.Context) error {
	if d == nil {
		return errors.New("badgerdb: nil db")
	}
	return d.Update(func(txn *badger.Txn) error {
		for _, prefix := range marketDataPrefixes {
			if err := ctx.Err(); err != nil {
				return err
			}
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			var keys [][]byte
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				keys = append(keys, append([]byte(nil), it.Item().Key()...))
			}
			it.Close()
			for _, k := range keys {
				if err := txn.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (d *DB) DeactivatePolyEventsNotIn(ctx context.Context, keep map[string]struct{}) (int, error) {
	if d == nil {
		return 0, errors.New("badgerdb: nil db")
	}
	if keep == nil {
		keep = map[string]struct{}{}
	}
	now := time.Now().UTC()
	n := 0
	err := d.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("market/event/")); it.ValidForPrefix([]byte("market/event/")); it.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			var ev eventDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &ev) }); err != nil {
				return err
			}
			if ev.Status == "closed" || strings.TrimSpace(ev.PolyEventID) == "" {
				continue
			}
			if _, ok := keep[ev.PolyEventID]; ok {
				continue
			}
			ev.Status = "closed"
			b, err := EncodeJSON(ev)
			if err != nil {
				return err
			}
			n++
			if err := txn.Set(KeyMarketEvent(ev.ID), b); err != nil {
				return err
			}
		}
		mp := txn.NewIterator(badger.DefaultIteratorOptions)
		defer mp.Close()
		for mp.Seek([]byte("market/market/")); mp.ValidForPrefix([]byte("market/market/")); mp.Next() {
			var md marketDoc
			if err := mp.Item().Value(func(v []byte) error { return DecodeJSON(v, &md) }); err != nil {
				return err
			}
			if md.Status != "active" {
				continue
			}
			var ev eventDoc
			if ok, err := d.getJSON(txn, KeyMarketEvent(md.EventID), &ev); err != nil || !ok {
				continue
			}
			if ev.Status != "closed" && isMarketKickoffOpen(md.StartTime, now) {
				continue
			}
			md.Status = "closed"
			b, err := EncodeJSON(md)
			if err != nil {
				return err
			}
			if err := txn.Set(KeyMarketMarket(md.ID), b); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}
