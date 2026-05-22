package badgerdb

import (
	"context"
	"errors"
	"strings"

	badger "github.com/dgraph-io/badger/v4"
)

// DisplayMetaParts is event-level display data keyed by CLOB token id.
type DisplayMetaParts struct {
	HomeTeam    string
	AwayTeam    string
	Sport       string
	League      string
	EventVolume float64
	PolyEventID string
	PolySlug    string
}

func mergeDisplayParts(dst, src DisplayMetaParts) DisplayMetaParts {
	if src.HomeTeam != "" {
		dst.HomeTeam = src.HomeTeam
	}
	if src.AwayTeam != "" {
		dst.AwayTeam = src.AwayTeam
	}
	if src.Sport != "" {
		dst.Sport = src.Sport
	}
	if src.League != "" {
		dst.League = src.League
	}
	if src.EventVolume > 0 {
		dst.EventVolume = src.EventVolume
	}
	if src.PolyEventID != "" {
		dst.PolyEventID = src.PolyEventID
	}
	if src.PolySlug != "" {
		dst.PolySlug = src.PolySlug
	}
	return dst
}

func (d *DB) loadDisplayPartsTxn(txn *badger.Txn, outcomeID string) (DisplayMetaParts, bool) {
	oid := strings.TrimSpace(outcomeID)
	if oid == "" {
		return DisplayMetaParts{}, false
	}
	var od outcomeDoc
	if ok, err := d.getJSON(txn, KeyMarketOutcome(oid), &od); err != nil || !ok {
		return DisplayMetaParts{}, false
	}
	var md marketDoc
	if ok, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !ok {
		return DisplayMetaParts{}, false
	}
	if md.Platform != "polymarket" {
		return DisplayMetaParts{}, false
	}
	var ed eventDoc
	if ok, err := d.getJSON(txn, KeyMarketEvent(md.EventID), &ed); err != nil || !ok {
		return DisplayMetaParts{}, false
	}
	slug := strings.Trim(strings.TrimPrefix(strings.TrimSpace(ed.PolySlug), "event/"), "/")
	return DisplayMetaParts{
		HomeTeam:    strings.TrimSpace(ed.HomeTeam),
		AwayTeam:    strings.TrimSpace(ed.AwayTeam),
		Sport:       strings.TrimSpace(strings.ToLower(ed.Sport)),
		League:      strings.TrimSpace(strings.ToLower(ed.League)),
		EventVolume: ed.EventVolume,
		PolyEventID: strings.TrimSpace(ed.PolyEventID),
		PolySlug:    slug,
	}, true
}

// RiskDisplayMetaBatch resolves display metadata for CLOB tokens.
// uniqTokens should be de-duplicated; at most the first 300 are processed.
// outcomeByToken maps trimmed token id → outcome id for a secondary join path.
func (d *DB) RiskDisplayMetaBatch(ctx context.Context, uniqTokens []string, outcomeByToken map[string]string) (map[string]DisplayMetaParts, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	const maxBatch = 300
	if len(uniqTokens) > maxBatch {
		uniqTokens = uniqTokens[:maxBatch]
	}
	out := make(map[string]DisplayMetaParts)
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		for _, tok := range uniqTokens {
			key := strings.TrimSpace(tok)
			if key == "" {
				continue
			}
			oid, ok, err := d.findOutcomeIDByTokenTxn(txn, key)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			parts, ok := d.loadDisplayPartsTxn(txn, oid)
			if !ok {
				continue
			}
			if prev, has := out[key]; has {
				out[key] = mergeDisplayParts(prev, parts)
			} else {
				out[key] = parts
			}
		}
		for tokenKey, oid := range outcomeByToken {
			key := strings.TrimSpace(tokenKey)
			oid = strings.TrimSpace(oid)
			if key == "" || oid == "" {
				continue
			}
			parts, ok := d.loadDisplayPartsTxn(txn, oid)
			if !ok {
				continue
			}
			if prev, has := out[key]; has {
				out[key] = mergeDisplayParts(prev, parts)
			} else {
				out[key] = parts
			}
		}
		return nil
	})
	return out, err
}
