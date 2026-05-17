// Package riskruntime provides an in-process ring buffer of structured risk events
// and broadcasts them to the risk WebSocket hub.
package riskruntime

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

// DefaultRingCap is the maximum number of runtime log entries retained in memory.
const DefaultRingCap = 400

// Envelope matches docs/runtime-observability.md §2 (WebSocket wraps this in { type, data }).
type Envelope struct {
	Seq           uint64         `json:"seq"`
	Ts            string         `json:"ts"`
	Type          string         `json:"type"`
	Category      string         `json:"category"`
	Severity      string         `json:"severity"`
	AccountID     *string        `json:"accountId"`
	MarketID      *string        `json:"marketId"`
	TokenID       *string        `json:"tokenId"`
	CorrelationID string         `json:"correlationId"`
	Detail        map[string]any `json:"detail"`
}

// Bus is a single-writer-safe ring of recent events plus throttled market_data lines.
type Bus struct {
	mu       sync.Mutex
	hub      *wsrelay.Hub
	max      int
	entries  []Envelope
	nextSeq  uint64
	bookMu   sync.Mutex
	bookLast map[string]time.Time
	bookPrev map[string]struct{ bid, ask float64 }
	diskMu   sync.Mutex
	disk     *os.File
}

// NewBus returns a bus that broadcasts JSON { "type": "risk_runtime_log", "data": Envelope } to hub.
func NewBus(hub *wsrelay.Hub, maxEntries int) *Bus {
	if maxEntries <= 0 {
		maxEntries = DefaultRingCap
	}
	return &Bus{
		hub:      hub,
		max:      maxEntries,
		bookLast: make(map[string]time.Time),
		bookPrev: make(map[string]struct{ bid, ask float64 }),
	}
}

// EnableJSONLLog appends every published envelope as one JSON line to path (NDJSON).
func (b *Bus) EnableJSONLLog(path string) error {
	if b == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	b.diskMu.Lock()
	defer b.diskMu.Unlock()
	if b.disk != nil {
		_ = b.disk.Close()
	}
	b.disk = f
	return nil
}

// CloseJSONLLog closes the NDJSON sink if open.
func (b *Bus) CloseJSONLLog() {
	if b == nil {
		return
	}
	b.diskMu.Lock()
	defer b.diskMu.Unlock()
	if b.disk != nil {
		_ = b.disk.Close()
		b.disk = nil
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Publish appends an event and broadcasts it. detail may be nil.
func (b *Bus) Publish(category, severity, eventType string, accountID, marketID, tokenID, correlationID string, detail map[string]any) {
	if b == nil || b.hub == nil {
		return
	}
	if detail == nil {
		detail = map[string]any{}
	}
	corr := correlationID
	if corr == "" {
		corr = uuid.NewString()
	}

	b.mu.Lock()
	b.nextSeq++
	env := Envelope{
		Seq:           b.nextSeq,
		Ts:            time.Now().UTC().Format(time.RFC3339Nano),
		Type:          eventType,
		Category:      category,
		Severity:      severity,
		AccountID:     strPtr(accountID),
		MarketID:      strPtr(marketID),
		TokenID:       strPtr(tokenID),
		CorrelationID: corr,
		Detail:        detail,
	}
	b.entries = append(b.entries, env)
	if len(b.entries) > b.max {
		b.entries = b.entries[len(b.entries)-b.max:]
	}
	b.mu.Unlock()

	if line, err := json.Marshal(env); err == nil {
		b.diskMu.Lock()
		if b.disk != nil {
			_, _ = b.disk.Write(append(line, '\n'))
		}
		b.diskMu.Unlock()
	}

	// Async fan-out: order book / risk hot paths must not block on WS writes + logging.
	b.hub.BroadcastJSONAsync(map[string]any{
		"type": "risk_runtime_log",
		"data": env,
	})
}

// summaryTickMinGap returns throttle duration from POLYBET_RUNTIME_BOOK_SUMMARY_MIN_GAP_MS (default 3000).
func summaryTickMinGap() time.Duration {
	const defaultMS = 3000
	s := strings.TrimSpace(os.Getenv("POLYBET_RUNTIME_BOOK_SUMMARY_MIN_GAP_MS"))
	if s == "" {
		return defaultMS * time.Millisecond
	}
	ms, err := strconv.Atoi(s)
	if err != nil || ms < 100 {
		return defaultMS * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

// MaybePublishMarketBookSummary emits market.book.summary_tick throttled per token (time + epsilon on bid/ask cents).
// Set POLYBET_RUNTIME_BOOK_SUMMARY_DISABLE=true to skip entirely (reduces risk_runtime_log volume).
func (b *Bus) MaybePublishMarketBookSummary(tokenID string, accountID string, bestBidCents, bestAskCents float64) {
	if b == nil || b.hub == nil || tokenID == "" {
		return
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("POLYBET_RUNTIME_BOOK_SUMMARY_DISABLE")), "true") {
		return
	}
	minGap := summaryTickMinGap()
	const eps = 0.5 // half cent

	now := time.Now()
	spread := bestAskCents - bestBidCents

	b.bookMu.Lock()
	last, okT := b.bookLast[tokenID]
	prev, okP := b.bookPrev[tokenID]
	changed := !okP || math.Abs(prev.bid-bestBidCents) >= eps || math.Abs(prev.ask-bestAskCents) >= eps
	elapsed := !okT || now.Sub(last) >= minGap
	if !elapsed && !changed {
		b.bookMu.Unlock()
		return
	}
	b.bookLast[tokenID] = now
	b.bookPrev[tokenID] = struct{ bid, ask float64 }{bid: bestBidCents, ask: bestAskCents}
	b.bookMu.Unlock()

	detail := map[string]any{
		"bestBid":       bestBidCents,
		"bestAsk":       bestAskCents,
		"spread":        spread,
		"schemaVersion": 1,
	}
	b.Publish("market_data", "info", "market.book.summary_tick", accountID, "", tokenID, "", detail)
}

// ListChronological returns up to limit entries from oldest to newest in the buffer.
func (b *Bus) ListChronological(limit int) []Envelope {
	if b == nil || limit <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) == 0 {
		return nil
	}
	n := len(b.entries)
	if limit < n {
		return append([]Envelope(nil), b.entries[n-limit:]...)
	}
	out := make([]Envelope, n)
	copy(out, b.entries)
	return out
}
