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

var gammaSportsLocation *time.Location

func init() {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		gammaSportsLocation = time.FixedZone("ET", -5*3600)
	} else {
		gammaSportsLocation = loc
	}
}

// sportsFeeRate is the legacy hardcoded taker-fee fallback used when the
// upstream Gamma row does not carry a parseable feeRate / feeRateBps /
// takerBaseFee. Operators on a league with a different blanket fee can
// override via bot_config.syncDefaultTakerFeeRate.
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
	// Gamma sometimes sends datetimes without timezone on non-sports fields; treat as UTC.
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

// parseGammaTimeSports parses Gamma sports timestamps. Bare datetimes without a
// timezone are interpreted as US Eastern wall time (Polymarket sports UI), not UTC.
func parseGammaTimeSports(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, ok := parseGammaTime(raw); ok && hasTimezoneRe.MatchString(strings.Replace(raw, " ", "T", 1)) {
		return t, true
	}
	iso := strings.Replace(raw, " ", "T", 1)
	if hasTimezoneRe.MatchString(iso) {
		return parseGammaTime(raw)
	}
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999",
		"2006-01-02 15:04:05.999999",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, iso, gammaSportsLocation); err == nil {
			return t.UTC(), true
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
	// Prefer moneyline gameStartTime — EndDate is market close / resolution, not tip-off
	// (using EndDate caused evening times e.g. 下午8:30 when official shows 8:30 AM).
	var fallback time.Time
	for i := range ev.Markets {
		m := &ev.Markets[i]
		if m.GameStartTime == nil {
			continue
		}
		raw := strings.TrimSpace(*m.GameStartTime)
		if raw == "" {
			continue
		}
		t, ok := parseGammaTimeSports(raw)
		if !ok {
			logrus.WithFields(logx.Pairs("event_id", ev.ID, "raw", raw, "market", m.Question)).Debug("市场同步：解析 gameStartTime 失败")
			continue
		}
		if isMoneyline(m) {
			return t
		}
		if fallback.IsZero() {
			fallback = t
		}
	}
	if !fallback.IsZero() {
		return fallback
	}
	if ev.StartDate != "" {
		if t, ok := parseGammaTimeSports(ev.StartDate); ok {
			return t
		}
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

// quoteFromMoneyline12 builds a MarketQuote from a Gamma event using the
// resolved per-market taker fee for price adjustment. defaultFeeRate is
// applied when the market itself doesn't carry a fee field; this is also
// returned via the resolvedFeeRate output so the sync engine can push the
// same value into bookcache.SetFeeRate without re-deriving it.
func quoteFromMoneyline12WithFee(ev gammaEvent, lg League, defaultFeeRate float64) (*domain.MarketQuote, float64, error) {
	q, err := quoteFromMoneyline12Internal(ev, lg, defaultFeeRate)
	if err != nil {
		return nil, defaultFeeRate, err
	}
	resolved := defaultFeeRate
	if ml := findCombinedMoneylineMarket(ev.Markets); ml != nil {
		if v, ok := marketFeeRate(ml); ok {
			resolved = v
		}
	}
	return q, resolved, nil
}

func quoteFromMoneyline12(ev gammaEvent, lg League) (*domain.MarketQuote, error) {
	q, _, err := quoteFromMoneyline12WithFee(ev, lg, sportsFeeRate)
	return q, err
}

func quoteFromMoneyline12Internal(ev gammaEvent, lg League, fee float64) (*domain.MarketQuote, error) {
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

	// Use the per-market fee when available, fall back to the supplied
	// default (typically sportsFeeRate). Prevents a 3% blanket overstating
	// the taker price on a market that's actually 0–2% in reality.
	effectiveFee := fee
	if v, ok := marketFeeRate(ml); ok {
		effectiveFee = v
	}

	st := startTimeFromEvent(ev)
	eventVol := optionalFeeFloat(ev.Volume)
	if eventVol <= 0 {
		eventVol = optionalFeeFloat(ev.VolumeNum)
	}
	if eventVol <= 0 {
		eventVol = optionalFeeFloat(ev.Volume24hr)
	}
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
		EventVolume: eventVol,
		BetType:     "12",
		MainLine:    true,
		PolyEventID: ev.ID,
		PolySlug:    strings.TrimSpace(ev.Slug),
		Outcomes: []domain.OutcomeOdds{
			{
				Label:       homeLabel,
				ImpliedOdds: applyFee(priceH, effectiveFee),
				ExternalID:  tokenHome,
				LiquidityDepth: domain.LiquidityDepth{
					AvailableSize: liq / 2,
				},
			},
			{
				Label:       awayLabel,
				ImpliedOdds: applyFee(priceA, effectiveFee),
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
