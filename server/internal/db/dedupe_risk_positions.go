package db

import (
	"context"
	"database/sql"
	"sort"

	"github.com/easyspace-ai/polybet/internal/store"
)

type riskPosDedupeRow struct {
	ID         string
	AccKey     string
	TokenID    string
	SideLabel  string
	UpdatedAt  string
	SizeShares float64
	HWOrAvg    float64
}

func normalizeAndDedupeRiskPositions(ctx context.Context, conn *sql.DB) error {
	ids, tids, err := scanAllRiskTokenIDs(ctx, conn)
	if err != nil {
		return err
	}
	for i, id := range ids {
		norm := store.NormalizeRiskCLOBTokenID(tids[i])
		if norm == "" || norm == tids[i] {
			continue
		}
		if _, err := conn.ExecContext(ctx, `UPDATE risk_positions SET token_id = ? WHERE id = ?`, norm, id); err != nil {
			return err
		}
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT rp.id, COALESCE(rp.account_id,'') AS acc, rp.token_id, rp.side_label, rp.updated_at, rp.size_shares,
			COALESCE(rpc.high_water_cents, rp.avg_entry_cents) AS hw
		FROM risk_positions rp
		LEFT JOIN risk_position_configs rpc ON rp.id = rpc.position_id
		WHERE rp.status IN ('open','closing')`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type gkey struct {
		acc, tok, side string
	}
	groups := make(map[gkey][]riskPosDedupeRow)
	for rows.Next() {
		var r riskPosDedupeRow
		if err := rows.Scan(&r.ID, &r.AccKey, &r.TokenID, &r.SideLabel, &r.UpdatedAt, &r.SizeShares, &r.HWOrAvg); err != nil {
			return err
		}
		r.TokenID = store.NormalizeRiskCLOBTokenID(r.TokenID)
		k := gkey{r.AccKey, r.TokenID, r.SideLabel}
		groups[k] = append(groups[k], r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, grp := range groups {
		if len(grp) < 2 {
			continue
		}
		sort.SliceStable(grp, func(i, j int) bool {
			a, b := grp[i], grp[j]
			if a.UpdatedAt != b.UpdatedAt {
				return a.UpdatedAt > b.UpdatedAt
			}
			if a.SizeShares != b.SizeShares {
				return a.SizeShares > b.SizeShares
			}
			return a.ID < b.ID
		})
		keeper := grp[0]
		maxHW := keeper.HWOrAvg
		for _, r := range grp {
			if r.HWOrAvg > maxHW {
				maxHW = r.HWOrAvg
			}
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
			VALUES(?, ?, COALESCE((SELECT stop_loss_pct FROM risk_position_configs WHERE position_id = ?), 10), datetime('now'), datetime('now'))
			ON CONFLICT(position_id) DO UPDATE SET high_water_cents = excluded.high_water_cents, updated_at = datetime('now')`,
			keeper.ID, maxHW, keeper.ID); err != nil {
			return err
		}
		for _, r := range grp[1:] {
			if _, err := conn.ExecContext(ctx, `DELETE FROM risk_positions WHERE id = ?`, r.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func scanAllRiskTokenIDs(ctx context.Context, conn *sql.DB) ([]string, []string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT id, token_id FROM risk_positions`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids, tids []string
	for rows.Next() {
		var id, tid string
		if err := rows.Scan(&id, &tid); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		tids = append(tids, tid)
	}
	return ids, tids, rows.Err()
}
