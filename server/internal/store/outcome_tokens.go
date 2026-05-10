package store

import "context"

// ListPolymarketOutcomeTokenIDs returns distinct CLOB token ids for active Polymarket markets.
func (s *Store) ListPolymarketOutcomeTokenIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT o.external_id
		FROM outcomes o
		JOIN markets m ON o.market_id = m.id
		WHERE m.platform = 'polymarket' AND m.status = 'active' AND o.external_id IS NOT NULL AND o.external_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
