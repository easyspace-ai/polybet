package store

import "strings"

// RiskDisplayMeta joins CLOB token → synced Gamma/Polybet market row for UI.
type RiskDisplayMeta struct {
	TokenID     string
	HomeTeam    string
	AwayTeam    string
	Sport       string
	League      string
	EventVolume float64
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
	if dst.TokenID == "" {
		dst.TokenID = strings.TrimSpace(src.TokenID)
	}
	return dst
}
