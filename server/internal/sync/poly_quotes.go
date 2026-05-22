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
var slugGameDateRe = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2})$`)

// maxEndDateDaysAfterSlug is how far event.endDate may sit past the slug game day
// and still be treated as tip-off (NBA). MLB market windows are ~7d out.
const maxEndDateDaysAfterSlug = 2

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
func parseKickoffCandidate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	iso := strings.Replace(raw, " ", "T", 1)
	// Explicit timezone → absolute instant (RFC3339 / +00 / Z).
	if hasTimezoneRe.MatchString(iso) {
		if t, ok := parseGammaTime(raw); ok {
			return t, true
		}
	}
	// Bare sports wall times (e.g. "2026-05-20 08:30:00") are Eastern, not UTC.
	if t, ok := parseGammaTimeSports(raw); ok {
		return t, true
	}
	if t, ok := parseGammaTime(raw); ok {
		return t, true
	}
	return time.Time{}, false
}

// leagueKickoffPrefersGameStart reports leagues where Gamma event.endDate is a
// trading/resolution window, not the scheduled first pitch. MLB cards use
// moneyline gameStartTime for the clock; NBA/NHL align endDate with tip-off.
func leagueKickoffPrefersGameStart(league string) bool {
	switch strings.ToLower(strings.TrimSpace(league)) {
	case "mlb":
		return true
	default:
		return false
	}
}

// eventTotalVolumeUSD matches Polymarket sports cards: lifetime event volume, including
// all active market lines (moneyline + spreads + totals). Gamma's volume vs volumeNum
// can differ by a small amount; the UI tends to track event.volume and the sum of markets.
func eventTotalVolumeUSD(ev gammaEvent) float64 {
	best := optionalFeeFloat(ev.Volume)
	if v := optionalFeeFloat(ev.VolumeNum); v > best {
		best = v
	}
	var sumMarkets float64
	for i := range ev.Markets {
		m := &ev.Markets[i]
		if !m.Active || m.Closed {
			continue
		}
		v := optionalFeeFloat(m.Volume)
		if vn := optionalFeeFloat(m.VolumeNum); vn > v {
			v = vn
		}
		sumMarkets += v
	}
	if sumMarkets > best {
		best = sumMarkets
	}
	return best
}

func kickoffFromEndDate(ev gammaEvent) (time.Time, bool) {
	if ev.EndDate == "" {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(ev.EndDate)
	if t, ok := parseKickoffCandidate(raw); ok {
		return t, true
	}
	logrus.WithFields(logx.Pairs("event_id", ev.ID, "raw", raw)).Debug("市场同步：解析 event.EndDate 失败")
	return time.Time{}, false
}

func parseSlugGameDate(slug string) (time.Time, bool) {
	m := slugGameDateRe.FindStringSubmatch(strings.TrimSpace(slug))
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", m[1], gammaSportsLocation)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func etCalendarDay(t time.Time) time.Time {
	y, m, d := t.In(gammaSportsLocation).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, gammaSportsLocation)
}

func daysBetweenET(earlier, later time.Time) int {
	return int(etCalendarDay(later).Sub(etCalendarDay(earlier)).Hours() / 24)
}

func earliestETTime(times []time.Time) time.Time {
	best := times[0]
	bestET := best.In(gammaSportsLocation)
	for _, t := range times[1:] {
		tET := t.In(gammaSportsLocation)
		if tET.Before(bestET) {
			best, bestET = t, tET
		}
	}
	return best
}

// collectGameStartTimes gathers parsed instants from the combined moneyline row first,
// then every market. Gamma often omits gameStartTime on the ML row but keeps it on spreads.
func collectGameStartTimes(ev gammaEvent, primary *gammaMarket) []time.Time {
	var out []time.Time
	seen := map[int64]struct{}{}
	add := func(m *gammaMarket) {
		if m == nil || m.GameStartTime == nil {
			return
		}
		raw := strings.TrimSpace(*m.GameStartTime)
		t, ok := parseKickoffCandidate(raw)
		if !ok {
			return
		}
		ns := t.UTC().UnixNano()
		if _, dup := seen[ns]; dup {
			return
		}
		seen[ns] = struct{}{}
		out = append(out, t)
	}
	add(primary)
	for i := range ev.Markets {
		add(&ev.Markets[i])
	}
	return out
}

func mergeSlugDayWithClock(slugDay, clock time.Time) time.Time {
	h, min, sec := clock.In(gammaSportsLocation).Clock()
	y, m, d := slugDay.In(gammaSportsLocation).Date()
	return time.Date(y, m, d, h, min, sec, 0, gammaSportsLocation).UTC()
}

// resolveSlugAnchoredKickoff picks first pitch for leagues where endDate is a market window.
// The slug suffix (e.g. mlb-chc-hou-2026-05-22) is Polymarket's canonical game day.
func resolveSlugAnchoredKickoff(ev gammaEvent, league string, ml *gammaMarket) (time.Time, bool) {
	slugDay, hasSlug := parseSlugGameDate(ev.Slug)
	candidates := collectGameStartTimes(ev, ml)

	if hasSlug && len(candidates) > 0 {
		var onSlug []time.Time
		for _, t := range candidates {
			if etCalendarDay(t).Equal(etCalendarDay(slugDay)) {
				onSlug = append(onSlug, t)
			}
		}
		if len(onSlug) > 0 {
			return earliestETTime(onSlug), true
		}
		return mergeSlugDayWithClock(slugDay, earliestETTime(candidates)), true
	}
	if len(candidates) > 0 {
		return earliestETTime(candidates), true
	}

	if t, ok := kickoffFromEndDate(ev); ok {
		if !hasSlug || daysBetweenET(slugDay, t) <= maxEndDateDaysAfterSlug {
			return t, true
		}
		logrus.WithFields(logx.Pairs(
			"event_id", ev.ID, "league", league, "slug", ev.Slug,
			"endDate_et", t.In(gammaSportsLocation).Format(time.RFC3339),
		)).Debug("市场同步：跳过 slug 远期的 endDate（市场窗口）")
	}

	if hasSlug {
		if ev.StartDate != "" {
			if t, ok := parseKickoffCandidate(ev.StartDate); ok {
				if daysBetweenET(slugDay, t) <= maxEndDateDaysAfterSlug {
					return mergeSlugDayWithClock(slugDay, t), true
				}
			}
		}
		return slugDay.UTC(), true
	}
	return time.Time{}, false
}

// startTimeFromEvent resolves tip-off for sports events.
//
// NBA/NHL: event.endDate is the scheduled tip-off (SwishAI / Poly sports UI).
//
// MLB: endDate is often a ~7d market window. Tip-off comes from gameStartTime on any
// sub-market, anchored to the slug game day so refresh stays stable when Gamma omits
// gameStartTime on the moneyline row.
func startTimeFromEvent(ev gammaEvent, league string, ml *gammaMarket) time.Time {
	if leagueKickoffPrefersGameStart(league) {
		if t, ok := resolveSlugAnchoredKickoff(ev, league, ml); ok {
			return t
		}
	} else {
		if t, ok := kickoffFromEndDate(ev); ok {
			return t
		}
		if times := collectGameStartTimes(ev, ml); len(times) > 0 {
			return earliestETTime(times)
		}
	}

	if ev.StartDate != "" {
		if t, ok := parseKickoffCandidate(ev.StartDate); ok {
			return t
		}
	}

	logrus.WithFields(logx.Pairs("event_id", ev.ID, "league", league)).Warn("市场同步：开赛时间未知（保留 zero time，下游需识别）")
	return time.Time{}
}

var titleStripRe = regexp.MustCompile(`(?i)^[^:]+:\s*`)
var titleSuffixRe = regexp.MustCompile(`\s+-\s+.+$`)

func extractTeams(title, ordering string) (home, away string, ok bool) {
	stripped := titleSuffixRe.ReplaceAllString(titleStripRe.ReplaceAllString(title, ""), "")
	re := regexp.MustCompile(`(?i)^(.+?)\s+(?:vs\.?|@)\s+(.+)$`)
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

	st := startTimeFromEvent(ev, lg.League, ml)
	eventVol := eventTotalVolumeUSD(ev)
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
