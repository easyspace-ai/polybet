package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/store"
)

// Run long-polls Telegram when credentials are set (env or bot_config). If unset,
// it sleeps and retries so dashboard saves apply without restarting the process.
func Run(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	hc := &http.Client{Timeout: 65 * time.Second}
	var offset int64
	startedLog := false
	for {
		if ctx.Err() != nil {
			return
		}
		token, chat := ResolveTelegramCreds(ctx, cfg, st)
		if token == "" || chat == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}
		if !startedLog {
			log.Info("telegram_bot_starting")
			startedLog = true
		}
		u := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=60&offset=%d", url.PathEscape(token), offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		resp, err := hc.Do(req)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		var body struct {
			OK     bool `json:"ok"`
			Result []struct {
				UpdateID int64 `json:"update_id"`
				Message  *struct {
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
					Text string `json:"text"`
				} `json:"message"`
			} `json:"result"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		for _, up := range body.Result {
			if up.UpdateID >= offset {
				offset = up.UpdateID + 1
			}
			if up.Message == nil {
				continue
			}
			if strconv.FormatInt(up.Message.Chat.ID, 10) != chat {
				continue
			}
			txt := strings.TrimSpace(up.Message.Text)
			if txt == "" {
				continue
			}
			if strings.HasPrefix(txt, "/status") || strings.HasPrefix(txt, "/start") {
				sendStatus(ctx, hc, token, chat, st)
			}
		}
	}
}

func sendStatus(ctx context.Context, hc *http.Client, token, chatID string, st *store.Store) {
	t, err := st.GetLastTradeSummary(context.Background())
	msg := "*Sports Prediction Market Router Status*\n"
	if err != nil || t == nil {
		msg += "Last trade: No trades yet"
	} else {
		plat := t.Platform
		if plat == "polymarket" {
			plat = "Polymarket"
		}
		odds := fmt.Sprintf("%.1f%% (req)", t.RequestedOdds*100)
		if t.FillOdds.Valid {
			odds = fmt.Sprintf("%.1f%%", t.FillOdds.Float64*100)
		}
		msg += fmt.Sprintf("Last trade: %s — $%.2f on %s @ %s", strings.ToUpper(t.Status), t.RequestedSize, plat, odds)
	}
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", msg)
	form.Set("parse_mode", "Markdown")
	u := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", url.PathEscape(token))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = hc.Do(req)
}
