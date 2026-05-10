package routercanon

import (
	"fmt"
	"regexp"
	"strings"
)

type CanonicalBetParts struct {
	Key     string
	BetType string
	Side    string
	Line    *float64
}

type CanonicalizeResult struct {
	Parts  *CanonicalBetParts
	Reason string
}

var spreadLabelRe = regexp.MustCompile(`^(.+?)\s+([+-]?\d+(?:\.\d+)?)$`)
var totalLabelRe = regexp.MustCompile(`^(Over|Under)\s+(\d+(?:\.\d+)?)$`)

func Canonicalize(label, betType, homeTeam, awayTeam string) CanonicalizeResult {
	lbl := strings.TrimSpace(label)
	bt := betType

	if bt == "1x2" {
		if lbl == homeTeam {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "1x2:home", BetType: bt, Side: "home"}}
		}
		if lbl == "Draw" {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "1x2:draw", BetType: bt, Side: "draw"}}
		}
		if lbl == awayTeam {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "1x2:away", BetType: bt, Side: "away"}}
		}
		if lbl == fmt.Sprintf("Not %s", homeTeam) {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "1x2:not_home", BetType: bt, Side: "not_home"}}
		}
		if lbl == "Not Draw" {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "1x2:not_draw", BetType: bt, Side: "not_draw"}}
		}
		if lbl == fmt.Sprintf("Not %s", awayTeam) {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "1x2:not_away", BetType: bt, Side: "not_away"}}
		}
		return CanonicalizeResult{Reason: "1x2 label did not match home/draw/away/negations"}
	}
	if bt == "12" {
		ll := strings.ToLower(strings.TrimSpace(lbl))
		hh := strings.ToLower(strings.TrimSpace(homeTeam))
		aa := strings.ToLower(strings.TrimSpace(awayTeam))
		if ll == hh {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "12:home", BetType: bt, Side: "home"}}
		}
		if ll == aa {
			return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "12:away", BetType: bt, Side: "away"}}
		}
		// Case-insensitive "… wins" (Gamma often uses "Celtics Wins" etc.)
		if strings.HasSuffix(ll, " wins") {
			core := strings.TrimSpace(strings.TrimSuffix(ll, " wins"))
			if core == hh {
				return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "12:home", BetType: bt, Side: "home"}}
			}
			if core == aa {
				return CanonicalizeResult{Parts: &CanonicalBetParts{Key: "12:away", BetType: bt, Side: "away"}}
			}
		}
		return CanonicalizeResult{Reason: "12 label did not match home/away"}
	}
	if bt == "spread" {
		m := spreadLabelRe.FindStringSubmatch(lbl)
		if m == nil {
			return CanonicalizeResult{Reason: "spread label did not parse"}
		}
		teamPart := strings.TrimSpace(m[1])
		var handicap float64
		fmt.Sscanf(m[2], "%f", &handicap)
		var side string
		switch teamPart {
		case homeTeam:
			side = "home"
		case awayTeam:
			side = "away"
		default:
			return CanonicalizeResult{Reason: fmt.Sprintf(`spread team prefix "%s" not home/away`, teamPart)}
		}
		v := handicap
		norm := spreadNorm(v)
		return CanonicalizeResult{Parts: &CanonicalBetParts{
			Key:     fmt.Sprintf("spread:%s:%s", side, norm),
			BetType: bt,
			Side:    side,
			Line:    &v,
		}}
	}
	if bt == "total" {
		m := totalLabelRe.FindStringSubmatch(lbl)
		if m == nil {
			return CanonicalizeResult{Reason: "total label did not parse"}
		}
		dir := strings.ToLower(m[1])
		var magnitude float64
		_, _ = fmt.Sscanf(m[2], "%f", &magnitude)
		key := fmt.Sprintf("total:%s:%g", dir, magnitude)
		mag := magnitude
		return CanonicalizeResult{Parts: &CanonicalBetParts{Key: key, BetType: bt, Side: dir, Line: &mag}}
	}
	return CanonicalizeResult{Reason: fmt.Sprintf("unknown betType %s", bt)}
}

func spreadNorm(v float64) string {
	if v == 0 {
		return "0"
	}
	if v > 0 {
		return "+" + strings.TrimPrefix(fmt.Sprintf("%v", v), "+")
	}
	return fmt.Sprintf("%v", v)
}
