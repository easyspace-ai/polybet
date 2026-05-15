package tg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
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
func Notify(ctx context.Context, cfg *config.Config, st *store.Store, log *logrus.Logger, text string) {
	if log == nil {
		log = logrus.StandardLogger()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	token, chat := ResolveTelegramCreds(ctx, cfg, st)
	t := strings.TrimSpace(text)
	log.WithFields(logx.Pairs("token_set", token != "", "chat_set", chat != "", "proxy", func() string {
		if cfg != nil {
			return cfg.HTTPPlatformProxy
		}
		return ""
	}())).Info("Telegram：准备发送通知")
	if token == "" || chat == "" || t == "" {
		log.WithFields(logx.Pairs("token_empty", token == "", "chat_empty", chat == "", "text_empty", t == "")).Warn("Telegram：跳过发送（凭证或正文为空）")
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
			log.WithFields(logx.Pairs("err", err.Error())).Debug("Telegram：跳过发送（构造请求失败）")
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		var hc *http.Client
		proxyUsed := false
		if cfg != nil && cfg.HTTPPlatformProxy != "" {
			if proxyURL, err := url.Parse(cfg.HTTPPlatformProxy); err == nil {
				log.WithFields(logx.Pairs("proxy", cfg.HTTPPlatformProxy)).Info("Telegram：使用代理发送")
				hc = &http.Client{
					Timeout: 14 * time.Second,
					Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
				}
				proxyUsed = true
			}
		}
		if hc == nil {
			log.Info("Telegram：直连发送")
			hc = &http.Client{Timeout: 14 * time.Second}
		}
		resp, err := hc.Do(req)
		if err != nil {
			log.WithFields(logx.Pairs("err", err.Error(), "proxy_used", proxyUsed)).Warn("Telegram：发送失败")
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		log.WithFields(logx.Pairs("status", resp.StatusCode, "body", string(body), "proxy_used", proxyUsed)).Info("Telegram：HTTP 响应")
		if resp.StatusCode != 200 {
			log.WithFields(logx.Pairs("status", resp.StatusCode, "response", string(body))).Warn("Telegram：非 200 响应")
		}
	}(t)
}
