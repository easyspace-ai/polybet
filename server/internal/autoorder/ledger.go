package autoorder

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/easyspace-ai/polybet/internal/storage"
)

var nyLoc *time.Location

func init() {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		nyLoc = time.FixedZone("ET", -5*3600)
	} else {
		nyLoc = loc
	}
}

// NYDateString returns YYYY-MM-DD for the America/New_York calendar day.
func NYDateString(t time.Time) string {
	return t.In(nyLoc).Format("2006-01-02")
}

func idempotencyKey(groupID, eventID, nyDate string) string {
	return groupID + ":" + eventID + ":" + nyDate
}

func loadLedger(ctx context.Context, st *storage.Backend) (Ledger, error) {
	raw, ok, err := st.GetBotConfig(ctx, LedgerKey)
	if err != nil {
		return Ledger{}, err
	}
	if !ok || trimJSON(raw) == "" {
		return Ledger{GroupSpent: map[string]float64{}}, nil
	}
	var l Ledger
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return Ledger{}, err
	}
	if l.GroupSpent == nil {
		l.GroupSpent = map[string]float64{}
	}
	return l, nil
}

func saveLedger(ctx context.Context, st *storage.Backend, l Ledger) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return st.UpsertBotConfig(ctx, LedgerKey, string(b))
}

// LedgerForToday returns ledger scoped to the current NY day (resets when date rolls).
func LedgerForToday(ctx context.Context, st *storage.Backend, now time.Time) (Ledger, error) {
	today := NYDateString(now)
	l, err := loadLedger(ctx, st)
	if err != nil {
		return Ledger{}, err
	}
	if l.Date != today {
		return Ledger{Date: today, GroupSpent: map[string]float64{}}, nil
	}
	return l, nil
}

func (l *Ledger) AlreadyExecuted(groupID, eventID, nyDate string) bool {
	key := idempotencyKey(groupID, eventID, nyDate)
	for _, k := range l.Executed {
		if k == key {
			return true
		}
	}
	return false
}

func (l *Ledger) GroupSpentUSD(groupID string) float64 {
	if l.GroupSpent == nil {
		return 0
	}
	return l.GroupSpent[groupID]
}

func (l *Ledger) RecordSpend(groupID, eventID, nyDate string, usd float64) {
	if l.GroupSpent == nil {
		l.GroupSpent = map[string]float64{}
	}
	l.GroupSpent[groupID] += usd
	key := idempotencyKey(groupID, eventID, nyDate)
	l.Executed = append(l.Executed, key)
}

func loadRuns(ctx context.Context, st *storage.Backend) ([]RunRecord, error) {
	raw, ok, err := st.GetBotConfig(ctx, RunsKey)
	if err != nil {
		return nil, err
	}
	if !ok || trimJSON(raw) == "" {
		return nil, nil
	}
	var runs []RunRecord
	if err := json.Unmarshal([]byte(raw), &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func appendRun(ctx context.Context, st *storage.Backend, rec RunRecord) error {
	runs, err := loadRuns(ctx, st)
	if err != nil {
		return err
	}
	runs = append([]RunRecord{rec}, runs...)
	if len(runs) > MaxRecentRuns {
		runs = runs[:MaxRecentRuns]
	}
	b, err := json.Marshal(runs)
	if err != nil {
		return err
	}
	return st.UpsertBotConfig(ctx, RunsKey, string(b))
}

// ListRecentRuns returns persisted auto-order attempts newest-first.
func ListRecentRuns(ctx context.Context, st *storage.Backend, limit int) ([]RunRecord, error) {
	runs, err := loadRuns(ctx, st)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > len(runs) {
		return runs, nil
	}
	return runs[:limit], nil
}

// PersistLedger saves ledger after a successful or dry-run reservation.
func PersistLedger(ctx context.Context, st *storage.Backend, l Ledger) error {
	if l.Date == "" {
		return fmt.Errorf("ledger date required")
	}
	return saveLedger(ctx, st, l)
}

// RecordAttempt appends a run log entry.
func RecordAttempt(ctx context.Context, st *storage.Backend, rec RunRecord) error {
	if rec.At == "" {
		rec.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return appendRun(ctx, st, rec)
}
