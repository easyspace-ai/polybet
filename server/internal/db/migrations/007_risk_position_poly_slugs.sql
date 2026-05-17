-- Polymarket deep-link slugs from Data API (authoritative for /event/<slug> URLs).
ALTER TABLE risk_positions ADD COLUMN poly_event_slug TEXT;
ALTER TABLE risk_positions ADD COLUMN poly_market_slug TEXT;
