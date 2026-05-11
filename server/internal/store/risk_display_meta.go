package store

import (
	"context"
	"database/sql"
	"strings"
)

// RiskDisplayMeta joins CLOB token → synced Gamma/Polybet market row for UI.
type RiskDisplayMeta struct {
	TokenID     string
	HomeTeam    string
	AwayTeam    string
	Sport       string
	PolyEventID string
	PolySlug    string
}

func mergeRiskMeta(dst, src RiskDisplayMeta) RiskDisplayMeta {
	if src.HomeTeam != "" {
		dst.HomeTeam = src.HomeTeam
	}
	if src.AwayTeam != "" {
		dst.AwayTeam = src.AwayTeam
	}
	if src.Sport != "" {
		dst.Sport = src.Sport
	}
	if src.PolyEventID != "" {
		dst.PolyEventID = src.PolyEventID
	}
	if src.PolySlug != "" {
		dst.PolySlug = src.PolySlug
	}
	if dst.TokenID == "" {
		dst.TokenID = src.TokenID
	}
	return dst
}

// RiskDisplayMetaForPositions resolves display rows for risk positions using:
//  1) outcomes.external_id = token_id (market sync path)
//  2) risk_positions.outcome_id → outcomes.id (CLOB FK path when (1) missed)
func (s *Store) RiskDisplayMetaForPositions(ctx context.Context, positions []RiskPosition) (map[string]RiskDisplayMeta, error) {
	out := make(map[string]RiskDisplayMeta)
	uniq := make([]string, 0, len(positions))
	seen := make(map[string]struct{}, len(positions))
	for _, p := range positions {
		tid := strings.TrimSpace(p.TokenID)
		if tid == "" {
			continue
		}
		if _, ok := seen[tid]; ok {
			continue
		}
		seen[tid] = struct{}{}
		uniq = append(uniq, tid)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	const maxBatch = 300
	if len(uniq) > maxBatch {
		uniq = uniq[:maxBatch]
	}
	ph := placeholders(len(uniq))
	args := make([]any, len(uniq))
	for i := range uniq {
		args[i] = uniq[i]
	}

	q1 := `SELECT o.external_id, e.home_team, e.away_team, e.sport, e.poly_event_id, e.poly_slug
FROM outcomes o
JOIN markets m ON o.market_id = m.id
JOIN events e ON e.id = m.event_id
WHERE o.external_id IN (` + ph + `) AND m.platform = 'polymarket'`
	rows1, err := s.db.QueryContext(ctx, q1, args...)
	if err != nil {
		return nil, err
	}
	if err := scanRiskMetaRows(rows1, out); err != nil {
		return nil, err
	}

	q2 := `SELECT DISTINCT rp.token_id, e.home_team, e.away_team, e.sport, e.poly_event_id, e.poly_slug
FROM risk_positions rp
INNER JOIN outcomes o ON o.id = rp.outcome_id
INNER JOIN markets m ON o.market_id = m.id
INNER JOIN events e ON e.id = m.event_id
WHERE rp.token_id IN (` + ph + `) AND m.platform = 'polymarket'
  AND rp.outcome_id IS NOT NULL AND TRIM(rp.outcome_id) != ''`
	rows2, err := s.db.QueryContext(ctx, q2, args...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var token, home, away, sport, polyID string
		var slug sql.NullString
		if err := rows2.Scan(&token, &home, &away, &sport, &polyID, &slug); err != nil {
			return nil, err
		}
		ps := ""
		if slug.Valid {
			ps = strings.TrimSpace(slug.String)
		}
		nm := RiskDisplayMeta{
			TokenID:     strings.TrimSpace(token),
			HomeTeam:    strings.TrimSpace(home),
			AwayTeam:    strings.TrimSpace(away),
			Sport:       strings.TrimSpace(strings.ToLower(sport)),
			PolyEventID: strings.TrimSpace(polyID),
			PolySlug:    ps,
		}
		key := strings.TrimSpace(token)
		if prev, ok := out[key]; ok {
			out[key] = mergeRiskMeta(prev, nm)
		} else {
			out[key] = nm
		}
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanRiskMetaRows(rows *sql.Rows, out map[string]RiskDisplayMeta) error {
	defer rows.Close()
	for rows.Next() {
		var token, home, away, sport, polyID string
		var slug sql.NullString
		if err := rows.Scan(&token, &home, &away, &sport, &polyID, &slug); err != nil {
			return err
		}
		ps := ""
		if slug.Valid {
			ps = strings.TrimSpace(slug.String)
		}
		key := strings.TrimSpace(token)
		nm := RiskDisplayMeta{
			TokenID:     key,
			HomeTeam:    strings.TrimSpace(home),
			AwayTeam:    strings.TrimSpace(away),
			Sport:       strings.TrimSpace(strings.ToLower(sport)),
			PolyEventID: strings.TrimSpace(polyID),
			PolySlug:    ps,
		}
		if prev, ok := out[key]; ok {
			out[key] = mergeRiskMeta(prev, nm)
		} else {
			out[key] = nm
		}
	}
	return rows.Err()
}
