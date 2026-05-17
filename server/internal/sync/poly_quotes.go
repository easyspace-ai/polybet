package sync

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/domain"
	"github.com/easyspace-ai/polybet/internal/logx"
)

const sportsFeeRate = 0.03

var shortTZOffsetRe = regexp.MustCompile(`([+-]\d{2})$`)
var hasTimezoneRe = regexp.MustCompile(`[Zz]|[+-]\d{2}(:\d{2})?$`)

func applyFee(p, fee float64) float64 {
	if fee == 0 {
		return p
	}
	return p + fee*p*(1-p)
}

func parseGammaTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	iso := strings.Replace(raw, " ", "T", 1)
	if shortTZOffsetRe.MatchString(iso) {
		iso = shortTZOffsetRe.ReplaceAllString(iso, "${1}:00")
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.999999Z07:00",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, iso); err == nil {
			return t, true
		}
	}
	// Gamma sometimes sends gameStartTime without timezone (e.g. "2026-05-12 19:00:00").
	// Treat bare datetime as UTC.
	if !hasTimezoneRe.MatchString(iso) {
		iso += "Z"
		for _, layout := range layouts {
			if t, err := time.Parse(layout, iso); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// startTimeFromEvent returns the parsed market start time, or the zero
// value when no field on the event can be decoded.
//
// The legacy implementation fell back to time.Now() which made "unknown
// start time" indistinguishable from "starts now", silently bypassing
// any downstream check that relies on start_time (e.g. the post-kickoff
// open-block gate). Returning zero forces consumers to handle the
// "unknown" case explicitly via IsKnownStartTime.
func startTimeFromEvent(ev gammaEvent) time.Time {
	for _, m := range ev.Markets {
		if m.GameStartTime != nil && strings.TrimSpace(*m.GameStartTime) != "" {
			if t, ok := parseGammaTime(*m.GameStartTime); ok {
				return t
			}
			logrus.WithFields(logx.Pairs("event_id", ev.ID, "raw", *m.GameStartTime)).Debug("市场同步：解析 gameStartTime 失败")
		}
	}
	if ev.EndDate != "" {
		if t, ok := parseGammaTime(ev.EndDate); ok {
			return t
		}
		logrus.WithFields(logx.Pairs("event_id", ev.ID, "raw", ev.EndDate)).Debug("市场同步：解析 EndDate 失败")
	}
	if ev.StartDate != "" {
		if t, ok := parseGammaTime(ev.StartDate); ok {
			return t
		}
		logrus.WithFields(logx.Pairs("event_id", ev.ID, "raw", ev.StartDate)).Debug("市场同步：解析 StartDate 失败")
	}
	logrus.WithFields(logx.Pairs("event_id", ev.ID)).Warn("市场同步：开赛时间未知（保留 zero time，下游需识别）")
	return time.Time{}
}

var titleStripRe = regexp.MustCompile(`(?i)^[^:]+:\s*`)
var titleSuffixRe = regexp.MustCompile(`\s+-\s+.+$`)

func extractTeams(title, ordering string) (home, away string, ok bool) {
	stripped := titleSuffixRe.ReplaceAllString(titleStripRe.ReplaceAllString(title, ""), "")
	re := regexp.MustCompile(`(?i)^(.+?)\s+vs\.?\s+(.+)$`)
	m := re.FindStringSubmatch(stripped)
	if m == nil {
		return "", "", false
	}
	first := strings.TrimSpace(m[1])
	second := strings.TrimSpace(m[2])
	if ordering == "home" {
		return first, second, true
	}
	return second, first, true
}

func isMoneyline(m *gammaMarket) bool {
	if m.SportsMarketType == nil {
		return true
	}
	return *m.SportsMarketType == "moneyline"
}

// findCombinedMoneylineMarket picks the single two-outcome moneyline Polymarket uses for NBA/NHL/MLB
// (one market, team names in `outcomes` JSON — not two separate "home wins?" binaries like soccer).
func findCombinedMoneylineMarket(markets []gammaMarket) *gammaMarket {
	var genericYesNo *gammaMarket
	for i := range markets {
		m := &markets[i]
		if !m.Active || m.Closed || !isMoneyline(m) {
			continue
		}
		labels := parseJSONStringArray(m.Outcomes)
		if len(labels) < 2 {
			continue
		}
		l0 := strings.ToLower(strings.TrimSpace(labels[0]))
		l1 := strings.ToLower(strings.TrimSpace(labels[1]))
		if (l0 == "yes" || l0 == "no") && (l1 == "yes" || l1 == "no") {
			if genericYesNo == nil {
				genericYesNo = m
			}
			continue
		}
		return m
	}
	return genericYesNo
}

// outcomeIndexForTitleTeam maps a title-derived team name to an index in Gamma's `outcomes` array.
func outcomeIndexForTitleTeam(titleTeam string, labels []string) int {
	t := strings.ToLower(strings.TrimSpace(titleTeam))
	if t == "" {
		return -1
	}
	for i, lb := range labels {
		l := strings.ToLower(strings.TrimSpace(lb))
		if l == "" || l == "yes" || l == "no" {
			continue
		}
		if l == t {
			return i
		}
	}
	best := -1
	for i, lb := range labels {
		l := strings.ToLower(strings.TrimSpace(lb))
		if l == "" || l == "yes" || l == "no" {
			continue
		}
		if strings.Contains(t, l) || strings.Contains(l, t) {
			if best < 0 {
				best = i
			}
		}
	}
	if best >= 0 {
		return best
	}
	parts := strings.Fields(t)
	if len(parts) > 0 {
		last := strings.ToLower(parts[len(parts)-1])
		for i, lb := range labels {
			l := strings.ToLower(strings.TrimSpace(lb))
			if l == "" {
				continue
			}
			if strings.Contains(l, last) || strings.Contains(last, l) {
				return i
			}
		}
	}
	return -1
}

func quoteFromMoneyline12(ev gammaEvent, lg League) (*domain.MarketQuote, error) {
	homeTitle, awayTitle, ok := extractTeams(ev.Title, lg.TitleOrdering)
	if !ok {
		return nil, fmt.Errorf("bad title")
	}
	ml := findCombinedMoneylineMarket(ev.Markets)
	if ml == nil {
		return nil, fmt.Errorf("missing moneyline")
	}
	outLabels := parseJSONStringArray(ml.Outcomes)
	tokens := parseJSONStringArray(ml.ClobTokenIDs)
	prices := parseJSONNumberArray(ml.OutcomePrices)
	if len(outLabels) < 2 || len(tokens) < 2 {
		return nil, fmt.Errorf("tokens")
	}
	hi := outcomeIndexForTitleTeam(homeTitle, outLabels)
	ai := outcomeIndexForTitleTeam(awayTitle, outLabels)
	if hi < 0 || ai < 0 || hi == ai {
		return nil, fmt.Errorf("outcome map")
	}
	homeLabel := strings.TrimSpace(outLabels[hi])
	awayLabel := strings.TrimSpace(outLabels[ai])
	tokenHome := tokens[hi]
	tokenAway := tokens[ai]
	if tokenHome == "" || tokenAway == "" {
		return nil, fmt.Errorf("tokens")
	}
	priceH := pickPrice(prices, hi, 0.5)
	priceA := pickPrice(prices, ai, 0.5)
	liq := parseLiquidity(ml.Liquidity)

	st := startTimeFromEvent(ev)
	// HomeTeam/AwayTeam must equal outcome labels so routercanon "12" canonicalization succeeds.
	name := homeLabel + " vs " + awayLabel
	return &domain.MarketQuote{
		Platform:    "polymarket",
		ExternalID:  ev.ID,
		Sport:       lg.Sport,
		League:      lg.League,
		HomeTeam:    homeLabel,
		AwayTeam:    awayLabel,
		Name:        name,
		StartTime:   st,
		BetType:     "12",
		MainLine:    true,
		PolyEventID: ev.ID,
		PolySlug:    strings.TrimSpace(ev.Slug),
		Outcomes: []domain.OutcomeOdds{
			{
				Label:       homeLabel,
				ImpliedOdds: applyFee(priceH, sportsFeeRate),
				ExternalID:  tokenHome,
				LiquidityDepth: domain.LiquidityDepth{
					AvailableSize: liq / 2,
				},
			},
			{
				Label:       awayLabel,
				ImpliedOdds: applyFee(priceA, sportsFeeRate),
				ExternalID:  tokenAway,
				LiquidityDepth: domain.LiquidityDepth{
					AvailableSize: liq / 2,
				},
			},
		},
	}, nil
}

func pickPrice(arr []float64, i int, def float64) float64 {
	if i < len(arr) {
		return arr[i]
	}
	return def
}
