-- Polymarket Gamma event slug for deep links (e.g. https://polymarket.com/event/<slug>)
ALTER TABLE events ADD COLUMN poly_slug TEXT;
