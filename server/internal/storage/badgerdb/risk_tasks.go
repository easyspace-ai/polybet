package badgerdb

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// RiskTask is the shape used by services (mirrors store.RiskTask).
type RiskTask struct {
	ID                string
	Type              string
	PositionID        sql.NullString
	Status            string
	Attempts          int
	LastError         sql.NullString
	Reason            sql.NullString
	LastAttemptDetail sql.NullString
	NextRunAt         time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func taskDocToRisk(t *RiskTaskDoc) RiskTask {
	out := RiskTask{
		ID: t.ID, Type: t.Type, Status: t.Status, Attempts: t.Attempts,
		NextRunAt: ParseTimeFlexible(t.NextRunAt), CreatedAt: ParseTimeFlexible(t.CreatedAt), UpdatedAt: ParseTimeFlexible(t.UpdatedAt),
	}
	if strings.TrimSpace(t.PositionID) != "" {
		out.PositionID = sql.NullString{String: t.PositionID, Valid: true}
	}
	if strings.TrimSpace(t.LastError) != "" {
		out.LastError = sql.NullString{String: t.LastError, Valid: true}
	}
	if strings.TrimSpace(t.Reason) != "" {
		out.Reason = sql.NullString{String: t.Reason, Valid: true}
	}
	if strings.TrimSpace(t.LastAttemptDetail) != "" {
		out.LastAttemptDetail = sql.NullString{String: t.LastAttemptDetail, Valid: true}
	}
	return out
}

func riskToTaskDoc(t *RiskTask) *RiskTaskDoc {
	d := &RiskTaskDoc{
		ID: t.ID, Type: t.Type, Status: t.Status, Attempts: t.Attempts,
		NextRunAt: t.NextRunAt.UTC().Format(time.RFC3339Nano),
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: t.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if t.PositionID.Valid {
		d.PositionID = t.PositionID.String
	}
	if t.LastError.Valid {
		d.LastError = t.LastError.String
	}
	if t.Reason.Valid {
		d.Reason = t.Reason.String
	}
	if t.LastAttemptDetail.Valid {
		d.LastAttemptDetail = t.LastAttemptDetail.String
	}
	return d
}

func (d *DB) readTask(txn *badger.Txn, id string) (*RiskTaskDoc, error) {
	var t RiskTaskDoc
	ok, err := d.getJSON(txn, KeyRiskTask(id), &t)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func (d *DB) deleteTaskDue(txn *badger.Txn, t *RiskTaskDoc) error {
	if t == nil {
		return nil
	}
	nano := ParseTimeFlexible(t.NextRunAt).UnixNano()
	k := KeyRiskTaskDue(nano, t.ID)
	return txn.Delete(k)
}

func (d *DB) writeTask(txn *badger.Txn, t *RiskTaskDoc) error {
	if t == nil {
		return errors.New("nil task")
	}
	b, err := EncodeJSON(t)
	if err != nil {
		return err
	}
	return txn.Set(KeyRiskTask(t.ID), b)
}

func (d *DB) upsertTaskDue(txn *badger.Txn, t *RiskTaskDoc) error {
	if t.Status != "pending" && t.Status != "failed" {
		return d.deleteTaskDue(txn, t)
	}
	nano := ParseTimeFlexible(t.NextRunAt).UnixNano()
	return txn.Set(KeyRiskTaskDue(nano, t.ID), []byte{1})
}

func (d *DB) FindPendingCloseTask(ctx context.Context, positionID string) (bool, error) {
	var n int
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/task/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			k := string(it.Item().Key())
			if strings.Contains(k, "/due/") {
				continue
			}
			var t RiskTaskDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &t) }); err != nil {
				continue
			}
			if t.Type == "close_position" && strings.TrimSpace(t.PositionID) == positionID &&
				(t.Status == "pending" || t.Status == "running") {
				n++
			}
		}
		return nil
	})
	return n > 0, err
}

func (d *DB) InsertRiskTask(ctx context.Context, t *RiskTask) error {
	if d == nil || t == nil {
		return errors.New("badgerdb: nil task")
	}
	doc := riskToTaskDoc(t)
	if strings.TrimSpace(doc.CreatedAt) == "" {
		doc.CreatedAt = nowRFC()
	}
	if strings.TrimSpace(doc.UpdatedAt) == "" {
		doc.UpdatedAt = doc.CreatedAt
	}
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := d.writeTask(txn, doc); err != nil {
			return err
		}
		return d.upsertTaskDue(txn, doc)
	})
}

func (d *DB) ListDueRiskTasks(ctx context.Context, limit int) ([]RiskTask, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	nowNano := time.Now().UTC().UnixNano()
	var out []RiskTask
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/task/due/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			k := string(it.Item().Key())
			parts := strings.Split(k, "/")
			if len(parts) < 5 {
				continue
			}
			nanoStr := parts[3]
			nano, err := strconv.ParseInt(nanoStr, 10, 64)
			if err != nil || nano > nowNano {
				continue
			}
			taskID := parts[4]
			td, err := d.readTask(txn, taskID)
			if err != nil || td == nil {
				continue
			}
			if td.Status != "pending" && td.Status != "failed" {
				continue
			}
			out = append(out, taskDocToRisk(td))
			if len(out) >= limit {
				break
			}
		}
		return nil
	})
	return out, err
}

func (d *DB) SetRiskTaskRunning(ctx context.Context, id string) error {
	return d.Update(func(txn *badger.Txn) error {
		t, err := d.readTask(txn, id)
		if err != nil || t == nil {
			return err
		}
		_ = d.deleteTaskDue(txn, t)
		t.Status = "running"
		t.UpdatedAt = nowRFC()
		return d.writeTask(txn, t)
	})
}

func (d *DB) UpdateRiskTaskLastAttemptDetail(ctx context.Context, id, detailJSON string) error {
	return d.Update(func(txn *badger.Txn) error {
		t, err := d.readTask(txn, id)
		if err != nil || t == nil {
			return err
		}
		t.LastAttemptDetail = detailJSON
		t.UpdatedAt = nowRFC()
		return d.writeTask(txn, t)
	})
}

func (d *DB) SetRiskTaskFailed(ctx context.Context, id string, attempts int, lastErr string, nextRun time.Time) error {
	return d.Update(func(txn *badger.Txn) error {
		t, err := d.readTask(txn, id)
		if err != nil || t == nil {
			return err
		}
		_ = d.deleteTaskDue(txn, t)
		t.Status = "failed"
		t.Attempts = attempts
		t.LastError = lastErr
		t.NextRunAt = nextRun.UTC().Format(time.RFC3339Nano)
		t.UpdatedAt = nowRFC()
		if err := d.writeTask(txn, t); err != nil {
			return err
		}
		return d.upsertTaskDue(txn, t)
	})
}

func (d *DB) SetRiskTaskSucceeded(ctx context.Context, id string) error {
	return d.Update(func(txn *badger.Txn) error {
		t, err := d.readTask(txn, id)
		if err != nil || t == nil {
			return err
		}
		_ = d.deleteTaskDue(txn, t)
		t.Status = "succeeded"
		t.LastError = ""
		t.UpdatedAt = nowRFC()
		return d.writeTask(txn, t)
	})
}

func (d *DB) SetRiskTaskCancelled(ctx context.Context, id, reason string) error {
	return d.Update(func(txn *badger.Txn) error {
		t, err := d.readTask(txn, id)
		if err != nil || t == nil {
			return err
		}
		_ = d.deleteTaskDue(txn, t)
		t.Status = "cancelled"
		t.LastError = reason
		t.UpdatedAt = nowRFC()
		return d.writeTask(txn, t)
	})
}

func (d *DB) CancelOtherCloseTasks(ctx context.Context, positionID, exceptTaskID string) error {
	return d.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/task/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			k := string(it.Item().Key())
			if strings.Contains(k, "/due/") {
				continue
			}
			var t RiskTaskDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &t) }); err != nil {
				continue
			}
			if t.Type != "close_position" || strings.TrimSpace(t.PositionID) != positionID {
				continue
			}
			if t.ID == exceptTaskID {
				continue
			}
			if t.Status != "pending" && t.Status != "failed" {
				continue
			}
			_ = d.deleteTaskDue(txn, &t)
			t.Status = "cancelled"
			t.LastError = "superseded"
			t.UpdatedAt = nowRFC()
			_ = d.writeTask(txn, &t)
		}
		return nil
	})
}

func (d *DB) ListRiskTasksRecent(ctx context.Context, limit int) ([]RiskTask, error) {
	if limit <= 0 {
		limit = 40
	}
	var all []RiskTask
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/task/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			k := string(it.Item().Key())
			if strings.Contains(k, "/due/") {
				continue
			}
			var t RiskTaskDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &t) }); err != nil {
				continue
			}
			all = append(all, taskDocToRisk(&t))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// sort by UpdatedAt desc
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].UpdatedAt.Before(all[j].UpdatedAt) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (d *DB) DeleteRiskTasksTerminal(ctx context.Context) (int64, error) {
	var n int64
	err := d.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/task/")
		var dels [][]byte
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			k := it.Item().KeyCopy(nil)
			sk := string(k)
			if strings.Contains(sk, "/due/") {
				continue
			}
			var t RiskTaskDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &t) }); err != nil {
				continue
			}
			if t.Status == "succeeded" || t.Status == "failed" || t.Status == "cancelled" {
				dels = append(dels, k)
			}
		}
		for _, k := range dels {
			var t RiskTaskDoc
			if ok, _ := d.getJSON(txn, k, &t); ok {
				_ = d.deleteTaskDue(txn, &t)
			}
			if err := txn.Delete(k); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

func (d *DB) ListRiskTasksByReason(ctx context.Context, taskType, reason string, limit int) ([]RiskTask, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []RiskTask
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/task/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			if strings.Contains(string(it.Item().Key()), "/due/") {
				continue
			}
			var t RiskTaskDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &t) }); err != nil {
				continue
			}
			if t.Type != taskType || t.Reason != reason || t.Status != "succeeded" {
				continue
			}
			out = append(out, taskDocToRisk(&t))
			if len(out) >= limit {
				break
			}
		}
		return nil
	})
	// sort created desc
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].CreatedAt.Before(out[j].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, err
}
