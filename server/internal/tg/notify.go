package tg

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/store"
)

const maxTelegramMessageRunes = 3500

// ResolveTelegramCreds returns bot token and chat id. Non-empty env (cfg) wins per
// field; otherwise values come from bot_config (dashboard / TELEGRAM_* parity with Node).
func ResolveTelegramCreds(ctx context.Context, cfg *config.Config, st *store.Store) (token, chat string) {
	if cfg != nil {
		token = strings.TrimSpace(cfg.TelegramBotToken)
		chat = strings.TrimSpace(cfg.TelegramChatID)
	}
	if st == nil {
		return token, chat
	}
	if token == "" {
		if v, ok, err := st.GetBotConfig(ctx, "telegramBotToken"); err == nil && ok {
			token = strings.TrimSpace(v)
		}
	}
	if chat == "" {
		if v, ok, err := st.GetBotConfig(ctx, "telegramAuthorizedChatId"); err == nil && ok {
			chat = strings.TrimSpace(v)
		}
	}
	return token, chat
}

// Notify sends a plain-text Telegram message if bot token and chat id are configured
// (env or store). It is best-effort, non-blocking, and ignores empty text.
func Notify(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger, text string) {
	if log == nil {
		log = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	token, chat := ResolveTelegramCreds(ctx, cfg, st)
	t := strings.TrimSpace(text)
	if token == "" || chat == "" || t == "" {
		return
	}
	if len([]rune(t)) > maxTelegramMessageRunes {
		rs := []rune(t)
		t = string(rs[:maxTelegramMessageRunes]) + "…"
	}
	go func(msg string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		form := url.Values{}
		form.Set("chat_id", chat)
		form.Set("text", msg)
		u := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", url.PathEscape(token))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
		if err != nil {
			log.Debug("telegram_send_skip", "err", err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		hc := &http.Client{Timeout: 14 * time.Second}
		resp, err := hc.Do(req)
		if err != nil {
			log.Warn("telegram_send_failed", "err", err.Error())
			return
		}
		resp.Body.Close()
	}(t)
}
