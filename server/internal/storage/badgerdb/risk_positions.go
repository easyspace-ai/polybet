package badgerdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/google/uuid"
)

// RiskPosition is the public shape used by services (mirrors store.RiskPosition).
type RiskPosition struct {
	ID             string
	Platform       string
	AccountID      string
	OutcomeID      sql.NullString
	TokenID        string
	Title          string
	SideLabel      string
	PolyEventSlug  string
	PolyMarketSlug string
	AvgEntryCents  float64
	SizeShares     float64
	CostUSD        float64
	HighWaterCents float64
	StopLossPct    float64
	Source         string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func docToRisk(p *RiskPosDoc) RiskPosition {
	out := RiskPosition{
		ID: p.ID, Platform: p.Platform, AccountID: p.AccountID, TokenID: p.TokenID,
		Title: p.Title, SideLabel: p.SideLabel, PolyEventSlug: p.PolyEventSlug, PolyMarketSlug: p.PolyMarketSlug,
		AvgEntryCents: p.AvgEntryCents, SizeShares: p.SizeShares, CostUSD: p.CostUSD,
		HighWaterCents: p.HighWaterCents, StopLossPct: p.StopLossPct, Source: p.Source, Status: p.Status,
		CreatedAt: ParseTimeFlexible(p.CreatedAt), UpdatedAt: ParseTimeFlexible(p.UpdatedAt),
	}
	if strings.TrimSpace(p.OutcomeID) != "" {
		out.OutcomeID = sql.NullString{String: p.OutcomeID, Valid: true}
	}
	if p.StopLossPct == 0 {
		out.StopLossPct = DefaultStopLossPct
	}
	if p.HighWaterCents == 0 && p.AvgEntryCents != 0 {
		out.HighWaterCents = p.AvgEntryCents
	}
	return out
}

func riskToDoc(p *RiskPosition) *RiskPosDoc {
	oc := ""
	if p.OutcomeID.Valid {
		oc = p.OutcomeID.String
	}
	slp := p.StopLossPct
	if slp <= 0 {
		slp = DefaultStopLossPct
	}
	hw := p.HighWaterCents
	if hw == 0 {
		hw = p.AvgEntryCents
	}
	return &RiskPosDoc{
		ID: p.ID, Platform: p.Platform, AccountID: p.AccountID, OutcomeID: oc, TokenID: NormalizeCLOBTokenID(p.TokenID),
		Title: p.Title, SideLabel: p.SideLabel, PolyEventSlug: p.PolyEventSlug, PolyMarketSlug: p.PolyMarketSlug,
		AvgEntryCents: p.AvgEntryCents, SizeShares: p.SizeShares, CostUSD: p.CostUSD,
		HighWaterCents: hw, StopLossPct: slp, Source: p.Source, Status: p.Status,
		RealizedPnLUSD: nil,
		ClosedAt:       nil,
		CreatedAt:      p.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (d *DB) scanPositions(ctx context.Context, pred func(RiskPosDoc) bool) ([]RiskPosition, error) {
	var out []RiskPosition
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/position/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var p RiskPosDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &p) }); err != nil {
				continue
			}
			if pred(p) {
				out = append(out, docToRisk(&p))
			}
		}
		return nil
	})
	return out, err
}

func (d *DB) GetRiskPosition(ctx context.Context, id string) (*RiskPosition, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	var rp *RiskPosition
	err := d.View(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil {
			return err
		}
		if p == nil {
			rp = nil
			return nil
		}
		r := docToRisk(p)
		rp = &r
		return nil
	})
	return rp, err
}

func (d *DB) ListOpenRiskPositionsByToken(ctx context.Context, tokenID, accountID string) ([]RiskPosition, error) {
	tok := NormalizeCLOBTokenID(tokenID)
	return d.scanPositions(ctx, func(p RiskPosDoc) bool {
		return p.Status == "open" && NormalizeCLOBTokenID(p.TokenID) == tok && strings.TrimSpace(p.AccountID) == strings.TrimSpace(accountID)
	})
}

func (d *DB) ListOpenRiskPositionTokenIDs(ctx context.Context) ([]string, error) {
	rows, err := d.scanPositions(ctx, func(p RiskPosDoc) bool {
		return p.Status == "open" && strings.TrimSpace(p.TokenID) != ""
	})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, r := range rows {
		t := NormalizeCLOBTokenID(r.TokenID)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		ids = append(ids, t)
	}
	return ids, nil
}

func (d *DB) ListOpenRiskPositionTokenIDsForAccount(ctx context.Context, accountID string) ([]string, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, nil
	}
	rows, err := d.scanPositions(ctx, func(p RiskPosDoc) bool {
		return p.Status == "open" && p.AccountID == accountID && strings.TrimSpace(p.TokenID) != ""
	})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, r := range rows {
		t := NormalizeCLOBTokenID(r.TokenID)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		ids = append(ids, t)
	}
	return ids, nil
}

func (d *DB) ListOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) ([]RiskPosition, error) {
	return d.scanPositions(ctx, func(p RiskPosDoc) bool {
		return p.Status == "open" && p.AccountID == accountID && p.SizeShares >= minShares
	})
}

func (d *DB) CountOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) (int64, error) {
	rows, err := d.ListOpenRiskPositionsMinShares(ctx, minShares, accountID)
	return int64(len(rows)), err
}

func (d *DB) ListOpenOrClosingRiskPositions(ctx context.Context, accountID string) ([]RiskPosition, error) {
	return d.scanPositions(ctx, func(p RiskPosDoc) bool {
		return (p.Status == "open" || p.Status == "closing") && p.AccountID == accountID
	})
}

func (d *DB) ListRiskPositionsOpenClosing(ctx context.Context, accountID string) ([]RiskPosition, error) {
	return d.ListOpenOrClosingRiskPositions(ctx, accountID)
}

func (d *DB) GetOpenRiskPositionByToken(ctx context.Context, tokenID, accountID string) (*RiskPosition, error) {
	rows, err := d.scanPositions(ctx, func(p RiskPosDoc) bool {
		tok := NormalizeCLOBTokenID(tokenID)
		return NormalizeCLOBTokenID(p.TokenID) == tok && strings.TrimSpace(p.AccountID) == strings.TrimSpace(accountID) &&
			(p.Status == "open" || p.Status == "closing")
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (d *DB) CreateRiskPosition(ctx context.Context, p *RiskPosition) error {
	if d == nil || p == nil {
		return errors.New("badgerdb: nil position")
	}
	pc := *p
	if pc.CreatedAt.IsZero() {
		pc.CreatedAt = time.Now().UTC()
	}
	if pc.UpdatedAt.IsZero() {
		pc.UpdatedAt = pc.CreatedAt
	}
	pc.TokenID = NormalizeCLOBTokenID(pc.TokenID)
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		openK := riskOpenKey(pc.AccountID, pc.TokenID, pc.SideLabel)
		var existingID string
		item, getErr := txn.Get(openK)
		if getErr == nil {
			_ = item.Value(func(v []byte) error {
				existingID = string(v)
				return nil
			})
		} else if !errors.Is(getErr, badger.ErrKeyNotFound) {
			return getErr
		}

		now := nowRFC()
		if existingID != "" {
			cur, err := d.readRiskPos(txn, existingID)
			if err != nil {
				return err
			}
			if cur == nil {
				return fmt.Errorf("missing open index target position %s", existingID)
			}
			cur.Platform = pc.Platform
			if pc.OutcomeID.Valid {
				cur.OutcomeID = pc.OutcomeID.String
			}
			cur.Title = pc.Title
			cur.AvgEntryCents = pc.AvgEntryCents
			cur.SizeShares = pc.SizeShares
			cur.CostUSD = pc.CostUSD
			if strings.TrimSpace(pc.PolyEventSlug) != "" {
				cur.PolyEventSlug = strings.TrimSpace(pc.PolyEventSlug)
			}
			if strings.TrimSpace(pc.PolyMarketSlug) != "" {
				cur.PolyMarketSlug = strings.TrimSpace(pc.PolyMarketSlug)
			}
			cur.Source = pc.Source
			cur.Status = pc.Status
			cur.UpdatedAt = now
			newHW := pc.HighWaterCents
			if newHW < cur.HighWaterCents {
				newHW = cur.HighWaterCents
			}
			cur.HighWaterCents = newHW
			if pc.StopLossPct > 0 {
				cur.StopLossPct = pc.StopLossPct
			}
			return d.writeRiskPos(txn, cur)
		}

		if strings.TrimSpace(pc.ID) == "" {
			pc.ID = uuid.New().String()
		}
		doc := riskToDoc(&pc)
		doc.CreatedAt = now
		doc.UpdatedAt = now
		if doc.StopLossPct == 0 {
			doc.StopLossPct = DefaultStopLossPct
		}
		if doc.HighWaterCents == 0 {
			doc.HighWaterCents = doc.AvgEntryCents
		}
		if err := d.writeRiskPos(txn, doc); err != nil {
			return err
		}
		return txn.Set(openK, []byte(doc.ID))
	})
}

func (d *DB) deleteOpenIndexIfMatch(txn *badger.Txn, p *RiskPosDoc) error {
	k := riskOpenKey(p.AccountID, p.TokenID, p.SideLabel)
	if item, err := txn.Get(k); err == nil {
		var id string
		_ = item.Value(func(v []byte) error {
			id = string(v)
			return nil
		})
		if id == p.ID {
			return txn.Delete(k)
		}
	}
	return nil
}

func (d *DB) SetRiskPositionStatus(ctx context.Context, id, status string) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		p.Status = status
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) UpdateRiskPositionSharesCost(ctx context.Context, id string, shares, cost float64) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		p.SizeShares, p.CostUSD = shares, cost
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) CloseRiskPosition(ctx context.Context, id string) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		_ = d.deleteOpenIndexIfMatch(txn, p)
		p.Status = "closed"
		p.SizeShares, p.CostUSD = 0, 0
		if p.ClosedAt == nil {
			s := nowRFC()
			p.ClosedAt = &s
		}
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) CloseRiskPositionPnL(ctx context.Context, id string, realizedPnLUSD float64) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		_ = d.deleteOpenIndexIfMatch(txn, p)
		p.Status = "closed"
		p.SizeShares, p.CostUSD = 0, 0
		v := realizedPnLUSD
		p.RealizedPnLUSD = &v
		if p.ClosedAt == nil {
			s := nowRFC()
			p.ClosedAt = &s
		}
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) UpdateRiskPositionPolySlugs(ctx context.Context, id, eventSlug, marketSlug string) error {
	eventSlug = strings.Trim(strings.TrimPrefix(strings.TrimSpace(eventSlug), "event/"), "/")
	marketSlug = strings.Trim(strings.TrimPrefix(strings.TrimSpace(marketSlug), "event/"), "/")
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		if eventSlug != "" {
			p.PolyEventSlug = eventSlug
		}
		if marketSlug != "" {
			p.PolyMarketSlug = marketSlug
		}
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) UpdateRiskPositionHighWater(ctx context.Context, id string, hw float64) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		p.HighWaterCents = hw
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

var ErrRiskPatchNoFields = errors.New("risk_patch_no_fields")

func (d *DB) UpdateRiskPositionStop(ctx context.Context, id string, stopLossPct *float64, highWaterCents *float64) error {
	if stopLossPct == nil && highWaterCents == nil {
		return ErrRiskPatchNoFields
	}
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		if stopLossPct != nil {
			p.StopLossPct = *stopLossPct
		}
		if highWaterCents != nil {
			p.HighWaterCents = *highWaterCents
		}
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) UpsertRiskPositionConfig(ctx context.Context, positionID string, hw, stop float64) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, positionID)
		if err != nil || p == nil {
			return err
		}
		p.HighWaterCents = hw
		p.StopLossPct = stop
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) GetRiskPositionConfig(ctx context.Context, positionID string) (hw, stop float64, created, updated time.Time, ok bool, err error) {
	p, err := d.GetRiskPosition(ctx, positionID)
	if err != nil {
		return 0, 0, time.Time{}, time.Time{}, false, err
	}
	if p == nil {
		return 0, 0, time.Time{}, time.Time{}, false, nil
	}
	return p.HighWaterCents, p.StopLossPct, p.CreatedAt, p.UpdatedAt, true, nil
}

func (d *DB) UpdateRiskPositionStatusShares(ctx context.Context, id, status string, shares, cost float64) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		p.Status, p.SizeShares, p.CostUSD = status, shares, cost
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) UpdateRiskPositionAvgEntry(ctx context.Context, id string, avgEntryCents float64) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		p.AvgEntryCents = avgEntryCents
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) UpdateRiskPositionTitle(ctx context.Context, id, title, sideLabel string) error {
	return d.Update(func(txn *badger.Txn) error {
		p, err := d.readRiskPos(txn, id)
		if err != nil || p == nil {
			return err
		}
		p.Title, p.SideLabel = title, sideLabel
		p.UpdatedAt = nowRFC()
		return d.writeRiskPos(txn, p)
	})
}

func (d *DB) NormalizeDustRisk(ctx context.Context, dust float64) error {
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/position/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var p RiskPosDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &p) }); err != nil {
				continue
			}
			if (p.Status == "open" || p.Status == "closing") && p.SizeShares <= dust {
				_ = d.deleteOpenIndexIfMatch(txn, &p)
				p.Status = "closed"
				p.SizeShares, p.CostUSD = 0, 0
				if p.ClosedAt == nil {
					s := nowRFC()
					p.ClosedAt = &s
				}
				p.UpdatedAt = nowRFC()
				_ = d.writeRiskPos(txn, &p)
			}
		}
		return nil
	})
}

// --- Exposure ---

func (d *DB) AccountRealizedPnLSince(ctx context.Context, accountID string, since time.Time) (float64, error) {
	if strings.TrimSpace(accountID) == "" || since.IsZero() {
		return 0, nil
	}
	var sum float64
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/position/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var p RiskPosDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &p) }); err != nil {
				continue
			}
			if p.AccountID != accountID || p.Status != "closed" || p.RealizedPnLUSD == nil || p.ClosedAt == nil {
				continue
			}
			ct := ParseTimeFlexible(*p.ClosedAt)
			if !ct.Before(since) {
				sum += *p.RealizedPnLUSD
			}
		}
		return nil
	})
	return sum, err
}

func (d *DB) AccountOpenExposureUSD(ctx context.Context, accountID string) (float64, error) {
	if strings.TrimSpace(accountID) == "" {
		return 0, nil
	}
	var sum float64
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/position/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var p RiskPosDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &p) }); err != nil {
				continue
			}
			if p.Status == "open" && p.AccountID == accountID {
				sum += p.CostUSD
			}
		}
		return nil
	})
	return sum, err
}

func (d *DB) MarketOpenExposureUSD(ctx context.Context, accountID, polyEventSlug string) (float64, error) {
	if strings.TrimSpace(accountID) == "" {
		return 0, nil
	}
	slug := strings.Trim(strings.TrimPrefix(strings.TrimSpace(polyEventSlug), "event/"), "/")
	if slug == "" {
		return 0, nil
	}
	var sum float64
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/position/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var p RiskPosDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &p) }); err != nil {
				continue
			}
			if p.Status == "open" && p.AccountID == accountID && strings.TrimSpace(p.PolyEventSlug) == slug {
				sum += p.CostUSD
			}
		}
		return nil
	})
	return sum, err
}
